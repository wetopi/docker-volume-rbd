package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
	"strconv"
	"fmt"
)

// Volume is the Docker concept which we map onto a Ceph RBD Image
type Volume struct {
	Name       string // RBD Image name
	Fstype     string
	Pool       string
	Size       uint64
	Order      int    // Specifies the object size expressed as a number of bits. The default is 22 (4KB).
	Mountpoint string
	Device     string
}


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
//      "Name": "volume_name",
//      "Options": {}
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
	logrus.Infof("volume-rbd Name=%s Request=Create", r.Name)

	d.Lock()
	defer d.Unlock()

	v := &Volume{
		Name: r.Name,
		Fstype: "ext4",
		Pool: "",
		Size: 512, // 512MB
		Order: 22, // 4KB Objects
		Mountpoint: "", // Unmounted when ""
		Device: "",
	}

	for key, val := range r.Options {
		switch key {
		case "pool":
			v.Pool = val
		case "size":
			var size, err = strconv.ParseUint(val, 10, 64)
			if err != nil {
				return responseError(fmt.Sprintf("unable to parse size int: %s", err))
			}
			v.Size = size
		case "order":
			var order, err = strconv.Atoi(val)
			if err != nil {
				return responseError(fmt.Sprintf("unable to parse order int: %s", err))
			}
			v.Order = order
		case "fstype":
			v.Fstype = val
		default:
			return responseError(fmt.Sprintf("unknown option %q", val))
		}
	}

	if v.Pool == "" {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Create Message=pool option required", r.Name))
	}


	err := d.connect(v.Pool)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Create Message=unable to connect to ceph pool: %s", r.Name, err))
	}

	defer d.shutdown()

	err, exists := d.rbdImageExists(v.Pool, v.Name)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Create Message=unable to check if rbd image exists: %s", r.Name, err))
	}

	if exists {
		logrus.Warnf("volume-rbd Name=%s Request=Create Message=skipping image create: ceph rbd image exists.", r.Name)

	} else {
		err = d.createRbdImage(v.Pool, v.Name, v.Size, v.Order, v.Fstype)
		if err != nil {
			return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Create Message=unable to create ceph rbd image: %s", v.Name, err))
		}
	}

	err = d.setVolume(v)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Create Message=unable to save volume state: %s", v.Name, err))
	}

	return volume.Response{}
}

func (d *rbdDriver) Remove(r volume.Request) volume.Response {
	logrus.Infof("volume-rbd Name=%s Request=Create", r.Name)

	d.Lock()
	defer d.Unlock()

	err, v := d.getVolume(r.Name)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Remove Message=unable to get volume state: %s", r.Name, err))
	}

	if v.Name == "" {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Remove Message=volume state not found", r.Name))
	}

	err = d.connect(v.Pool)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Remove Message=unable to connect to ceph pool: %s", r.Name, err))
	}

	defer d.shutdown()

	err, exists := d.rbdImageExists(v.Pool, v.Name)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Remove Message=unable to check if rbd image exists: %s", r.Name, err))
	}

	if !exists {
		logrus.Infof("volume-rbd Name=%s Request=Remove Message=skipping image remove: unexisting ceph rbd image.", r.Name)

	} else {

		d.freeUpRbdImage(v.Pool, v.Name, v.Mountpoint)
		if err != nil {
			return responseError(err.Error())
		}

		err = d.removeRbdImageWithRetries(v.Name)
		if err != nil {
			return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Remove Message=unable to remove rbd image: %s", r.Name, err))
		}
	}

	err = d.deleteVolume(v.Name)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Remove Message=unable to delete volume state: %s", v.Name, err))
	}

	return volume.Response{}
}

func (d *rbdDriver) Path(r volume.Request) volume.Response {
	logrus.Infof("volume-rbd Name=%s Request=Path", r.Name)

	d.RLock()
	defer d.RUnlock()

	err, v := d.getVolume(r.Name)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Path Message=unable to get volume state: %s", r.Name, err))
	}

	if v.Name == "" {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Path Message=volume state not found", r.Name))
	}

	return volume.Response{Mountpoint: v.Mountpoint}
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
	logrus.Infof("volume-rbd Name=%s Request=Mount", r.Name)

	var err error

	d.Lock()
	defer d.Unlock()

	err, v := d.getVolume(r.Name)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Mount Message=unable to get volume state: %s", r.Name, err))
	}

	if v.Name == "" {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Mount Message=volume state not found", r.Name))
	}

	if v.Mountpoint != "" {
		logrus.Warnf("volume-rbd Name=%s Request=Mount Message=this volume has a previous registered mountpoint(%s)", v.Name, v.Mountpoint)
	}

	err, v.Device, v.Mountpoint = d.mountRbdImage(v.Pool, v.Name, v.Fstype)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Mount Message=unable to mount rbd image: %s", v.Name, err))
	}

	d.setVolume(v)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Mount Message=unable to set volume state: %s", v.Name, err))
	}

	return volume.Response{Mountpoint: v.Mountpoint}
}


// POST /VolumeDriver.Unmount
//
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
	logrus.Infof("volume-rbd Name=%s Request=Unmount", r.Name)

	var err error

	d.Lock()
	defer d.Unlock()

	err, v := d.getVolume(r.Name)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Unmount Message=unable to get volume state: %s", r.Name, err))
	}

	if v.Name == "" {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Unmount Message=volume state not found", r.Name))
	}

	err = d.freeUpRbdImage(v.Pool, v.Name, v.Mountpoint)
	if err != nil {
		return responseError(err.Error())
	}

	v.Device = ""
	v.Mountpoint = ""
	d.setVolume(v)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Unmount Message=unable to set volume state: %s", v.Name, err))
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
	logrus.Infof("volume-rbd Name=%s Request=Get", r.Name)

	d.Lock()
	defer d.Unlock()

	err, v := d.getVolume(r.Name)
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Get Message=unable to get volume state: %s", r.Name, err))
	}

	if v.Name == "" {
		return responseError(fmt.Sprintf("volume-rbd Name=%s Request=Get Message=volume state not found", r.Name))
	}

	return volume.Response{Volume: &volume.Volume{Name: r.Name, Mountpoint: v.Mountpoint}}
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
	logrus.Infof("volume-rbd Request=List")

	d.Lock()
	defer d.Unlock()

	err, volumes := d.getVolumes()
	if err != nil {
		return responseError(fmt.Sprintf("volume-rbd Request=List Message=getting volumes state give us error: %s", err))
	}

	var vols []*volume.Volume
	for _, v := range *volumes {
		vols = append(vols, &volume.Volume{Name: v.Name, Mountpoint: v.Mountpoint})
	}
	return volume.Response{Volumes: vols}
}

func (d *rbdDriver) Capabilities(r volume.Request) volume.Response {
	logrus.Infof("volume-rbd Request=Capabilities")

	return volume.Response{
		Capabilities: volume.Capability{
			Scope: "global",
		},
	}
}

func responseError(err string) volume.Response {
	logrus.Error(err)
	return volume.Response{Err: err}
}
