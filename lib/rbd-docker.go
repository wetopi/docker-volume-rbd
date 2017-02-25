package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
	"strconv"
	"fmt"
	"os"
)


// Implement the Docker VolumeDriver API via volume interface
// https://github.com/docker/go-plugins-helpers/blob/master/volume/api.go
// Create will ensure the RBD image requested is available.  Plugin requires
// --create option to provision new RBD images.
//
// Docker Volume Create Options:
//   size   - in MB
//   pool
//   fstype
//
//
// POST /VolumeDriver.Create
//
// Request:
//    {
//      "name": "volume_name",
//      "size": {}
//    }
//
//    Instruct the plugin that the user wants to create a volume, given a user
//    specified volume name. The plugin does not need to actually manifest the
//    volume on the filesystem yet (until Mount is called).
//
// Response:
//    { "Err": null }
//    Respond with a string error if an error occurred.
//
func (d *rbdDriver) Create(r volume.Request) volume.Response {
	logrus.WithField("api", "Create").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v := &Volume{
		name: "",
		fstype: "ext4",
		pool: "",
		size: 512, // 512MB
		order: 22, // 4KB Objects
	}

	for key, val := range r.Options {
		switch key {
		case "name":
			v.name = val
		case "pool":
			v.pool = val
		case "size":
			var size, err = strconv.ParseUint(val, 10, 64)
			if err != nil {
				return responseError(fmt.Sprintf("unable to parse size int: %s", err))
			}
			v.size = size
		case "order":
			var order, err = strconv.Atoi(val)
			if err != nil {
				return responseError(fmt.Sprintf("unable to parse order int: %s", err))
			}
			v.order = order
		case "fstype":
			v.fstype = val
		default:
			return responseError(fmt.Sprintf("unknown option %q", val))
		}
	}

	if v.name == "" {
		return responseError("'name' option required")
	}

	// do we already know about this volume? return early
	if _, found := d.volumes[v.name]; found {
		return responseError("volume is already in known mounts: " + v.name)
	}

	if v.pool == "" {
		return responseError("'pool' option required")
	}


	// set mountpoint
	v.mountpoint = d.mountPointOnHost(v.pool, v.name)


	// connect to ceph
	err := d.connect(v.pool)
	if err != nil {
		return responseError(fmt.Sprintf("unable to connect to ceph and access pool: %s", err))

	}
	defer d.shutdown()

	exists, err := d.rbdImageExists(v.pool, v.name)
	if err != nil {
		return responseError(fmt.Sprintf("unable to check for rbd image: %s", err))
	}

	if !exists {

		// try to create it
		err = d.createRBDImage(v.pool, v.name, v.size, v.order, v.fstype)
		if err != nil {
			return responseError(fmt.Sprintf("unable to create Ceph RBD Image(%s): %s", v.name, err))
		}
	}

	d.volumes[v.name] = v

	d.saveState()

	return volume.Response{}
}

func (d *rbdDriver) Remove(r volume.Request) volume.Response {
	logrus.WithField("api", "Remove").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	if v.connections != 0 {
		return responseError(fmt.Sprintf("volume %s is currently used by a container", r.Name))
	}


	// connect to Ceph and check ceph rbd api for it
	err := d.connect(v.pool)
	if err != nil {
		return responseError(fmt.Sprintf("unable to connect to ceph and access pool: %s", err))
	}
	defer d.shutdown()

	exists, err := d.rbdImageExists(v.pool, v.name)
	if err != nil {
		return responseError(fmt.Sprintf("unable to check for rbd image: %s", err))
	}

	if !exists {
		return responseError(fmt.Sprintf("rbd image not found: %s", v.name))
	}

	err = d.removeRBDImage(v.name)
	if err != nil {
		return responseError(fmt.Sprintf("unable to remove rbd image(%s): %s", v.name, err))
	}

	delete(d.volumes, r.Name)
	d.saveState()
	return volume.Response{}
}

func (d *rbdDriver) Path(r volume.Request) volume.Response {
	logrus.WithField("method", "path").Debugf("%#v", r)

	d.RLock()
	defer d.RUnlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	return volume.Response{Mountpoint: v.mountpoint}
}



// Mount will Ceph Map the RBD image to the local kernel and create a mount
// point and mount the image.
//
// POST /VolumeDriver.Mount
//
// Request:
//    { "Name": "volume_name" }
//    Docker requires the plugin to provide a volume, given a user specified
//    volume name. This is called once per container start.
//
// Response:
//    { "Mountpoint": "/path/to/directory/on/host", "Err": null }
//    Respond with the path on the host filesystem where the volume has been
//    made available, and/or a string error if an error occurred.
//
func (d *rbdDriver) Mount(r volume.MountRequest) volume.Response {
	logrus.WithField("api", "Mount").Debugf("%#v", r)

	var err error

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	if v.connections == 0 {

		// map and mount the RBD image
		// map
		v.device, err = d.mapImage(v.pool, v.name)
		if err != nil {
			return responseError(fmt.Sprintf(" unable to map rbd image(%s) to kernel device: %s", v.name, err))
		}


		// check for mountdir - create if necessary
		err = os.MkdirAll(v.mountpoint, os.ModeDir | os.FileMode(int(0775)))
		if err != nil {
			defer d.unmapImageDevice(v.device)
			return responseError(fmt.Sprintf("unable to make mountpoint(%s): %s", v.mountpoint, err))
		}


		// mount
		err = d.mountDevice(v.fstype, v.device, v.mountpoint)
		if err != nil {
			defer d.unmapImageDevice(v.device)
			return responseError(fmt.Sprintf("unable to mount device(%s) to directory(%s): %s", v.device, v.mountpoint, err))
		}

	}

	v.connections++

	return volume.Response{Mountpoint: v.mountpoint}
}


// POST /VolumeDriver.Unmount
//
// - assuming writes are finished and no other containers using same disk on this host?

// Request:
//    { "Name": "volume_name" }
//    Indication that Docker no longer is using the named volume. This is
//    called once per container stop. Plugin may deduce that it is safe to
//    deprovision it at this point.
//
// Response:
//    { "Err": null }
//    Respond with a string error if an error occurred.
//
func (d *rbdDriver) Unmount(r volume.UnmountRequest) volume.Response {
	logrus.WithField("api", "Unmount").Debugf("%#v", r)

	var err error

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	v.connections--

	if v.connections <= 0 {

		// unmount
		err = d.unmountDevice(v.device)
		if err != nil {
			return responseError(err.Error())
		}


		// unmap
		err = d.unmapImageDevice(v.device)
		if err != nil {
			return responseError(err.Error())
		}

		v.connections = 0
	}

	return volume.Response{}
}

// Get the volume info.
//
// POST /VolumeDriver.Get
//
// Request:
//    { "Name": "volume_name" }
//    Docker needs reminding of the path to the volume on the host.
//
// Response:
//    { "Volume": { "Name": "volume_name", "Mountpoint": "/path/to/directory/on/host" }, "Err": null }
//    Respond with a tuple containing the name of the queried volume and the
//    path on the host filesystem where the volume has been made available,
//    and/or a string error if an error occurred.
//
func (d *rbdDriver) Get(r volume.Request) volume.Response {
	logrus.WithField("api", "Get").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	return volume.Response{Volume: &volume.Volume{Name: r.Name, Mountpoint: v.mountpoint}}
}


// Get the list of volumes registered with the plugin.
//
// POST /VolumeDriver.List
//
// Request:
//    {}
//    List the volumes mapped by this plugin.
//
// Response:
//    { "Volumes": [ { "Name": "volume_name", "Mountpoint": "/path/to/directory/on/host" } ], "Err": null }
//    Respond with an array containing pairs of known volume names and their
//    respective paths on the host filesystem (where the volumes have been
//    made available).
//
func (d *rbdDriver) List(r volume.Request) volume.Response {
	logrus.WithField("api", "List").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	var vols []*volume.Volume
	for name, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: v.mountpoint})
	}
	return volume.Response{Volumes: vols}
}



// Capabilities
// Scope: global - the cluster manager knows it only needs to create the volume once instead of on every engine
func (d *rbdDriver) Capabilities(r volume.Request) volume.Response {
	logrus.WithField("api", "Capabilities").Debugf("%#v", r)

	return volume.Response{
		Capabilities: volume.Capability{
			Scope: "global",
		},
	}
}
