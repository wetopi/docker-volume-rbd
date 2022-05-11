package dockerVolumeRbd

import (
	"github.com/sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
	"strconv"
	"fmt"
)


func (d *rbdDriver) Create(r *volume.CreateRequest) error {
	logrus.Infof("volume-rbd Name=%s Request=Create", r.Name)

	d.Lock()
	defer d.Unlock()

    var err error
	var fstype string = "ext4"
	var mkfsOptions string = "-O mmp"
	var size  uint64 =  512 // 512MB
	var order int = 22 // 4KB Objects

	for key, val := range r.Options {
		switch key {
		case "size":
			size, err = strconv.ParseUint(val, 10, 64)
			if err != nil {
				return fmt.Errorf("unable to parse size int: %s", err)
			}

		case "order":
			order, err = strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("unable to parse order int: %s", err)
			}

		case "fstype":
			fstype = val

		case "mkfsOptions":
			mkfsOptions = val

		case "pool":
		    // ignored ... backward compatibility

		default:
			return fmt.Errorf("unknown option %q", val)
		}
	}


	err = d.Connect()
	if err != nil {
		return fmt.Errorf("volume-rbd Name=%s Request=Create Message=unable to connect to rbd pool", err)
	}
	defer d.Shutdown()

	err, exists := d.RbdImageExists(r.Name)
	if err != nil {
		return fmt.Errorf("volume-rbd Name=%s Request=Create Message=unable to check if rbd image exists: %s", r.Name, err)
	}

	if exists {
		return fmt.Errorf("volume-rbd Name=%s Request=Create Message=skipping image create: ceph rbd image exists.", r.Name)
    }

    err = d.CreateRbdImage(r.Name, size, order, fstype, mkfsOptions)
    if err != nil {
        return fmt.Errorf("volume-rbd Name=%s Request=Create Message=unable to create ceph rbd image: %s", r.Name, err)
    }

	return nil
}


func (d *rbdDriver) List() (*volume.ListResponse, error) {
	logrus.Infof("volume-rbd Request=List")

	d.Lock()
	defer d.Unlock()


	err := d.Connect()
	if err != nil {
		return &volume.ListResponse{}, fmt.Errorf("volume-rbd Request=List Message=unable to connect to rbd pool: %s", err)
	}
	defer d.Shutdown()

	err, images := d.GetRbdImages()
	if err != nil {
		return &volume.ListResponse{}, fmt.Errorf("volume-rbd Request=List Message=getting volumes state give us error: %s", err)
	}

	var vols []*volume.Volume
	for _, imageName := range images {
		vols = append(vols, &volume.Volume{Name: imageName, Mountpoint: ""})
	}

	return &volume.ListResponse{Volumes: vols}, nil
}


func (d *rbdDriver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	logrus.Infof("volume-rbd Name=%s Request=Get", r.Name)

	d.Lock()
	defer d.Unlock()

	err := d.Connect()
	if err != nil {
		return &volume.GetResponse{}, fmt.Errorf("volume-rbd Name=%s Request=Get Message=unable to connect to rbd pool: %s", r.Name, err)
	}
	defer d.Shutdown()

	err, exists := d.RbdImageExists(r.Name)
	if err != nil {
		return &volume.GetResponse{}, fmt.Errorf("volume-rbd Name=%s Request=Get Message=unable to check if rbd image exists: %s", r.Name, err)
	}

	if !exists {
		return &volume.GetResponse{}, fmt.Errorf("volume-rbd Name=%s Request=Get Message=rbd image not found", r.Name)
	}

	return &volume.GetResponse{Volume: &volume.Volume{Name: r.Name, Mountpoint: d.GetMountPointPath(r.Name)}}, nil
}


func (d *rbdDriver) Remove(r *volume.RemoveRequest) error {
	logrus.Infof("volume-rbd Name=%s Request=Create", r.Name)

	d.Lock()
	defer d.Unlock()

	err := d.Connect()
	if err != nil {
		return fmt.Errorf("volume-rbd Name=%s Request=Remove Message=unable to connect to rbd pool: %s", r.Name, err)
	}
	defer d.Shutdown()


	err, exists := d.RbdImageExists(r.Name)
	if err != nil {
		return fmt.Errorf("volume-rbd Name=%s Request=Remove Message=unable to check if rbd image exists: %s", r.Name, err)
	}

	if exists {

        err = d.FreeUpRbdImage(r.Name)
        if err != nil {
            return fmt.Errorf("volume-rbd Name=%s Request=Remove Message=unable to free up rbd image: %s", r.Name, err)
        }

        err = d.RemoveRbdImageWithRetries(r.Name)
        if err != nil {
            return fmt.Errorf("volume-rbd Name=%s Request=Remove Message=unable to remove rbd image: %s", r.Name, err)
        }
	}

	return nil
}


func (d *rbdDriver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	logrus.Infof("volume-rbd Name=%s Request=Path", r.Name)

	d.Lock()
	defer d.Unlock()

	err := d.Connect()
	if err != nil {
		return &volume.PathResponse{}, fmt.Errorf("volume-rbd Name=%s Request=Path Message=unable to connect to rbd pool: %s", r.Name, err)
	}
	defer d.Shutdown()

	err, exists := d.RbdImageExists(r.Name)
	if err != nil {
		return &volume.PathResponse{}, fmt.Errorf("volume-rbd Name=%s Request=Path Message=unable to check if rbd image exists: %s", r.Name, err)
	}

	if !exists {
		return &volume.PathResponse{}, fmt.Errorf("volume-rbd Name=%s Request=Path Message=rbd image not found", r.Name)
	}

	return &volume.PathResponse{Mountpoint: d.GetMountPointPath(r.Name)}, nil
}


func (d *rbdDriver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	logrus.Infof("volume-rbd Name=%s Request=Mount", r.Name)

	d.Lock()
	defer d.Unlock()

	var err error

	if r.Name == "" {
		return &volume.MountResponse{}, fmt.Errorf("volume-rbd Name=%s Request=Mount Message=volume state not found", r.Name)
	}


	err, mountpoint := d.MountRbdImage(r.Name)
	if err != nil {
		return &volume.MountResponse{}, fmt.Errorf("volume-rbd Name=%s Request=Mount Message= %s", r.Name, err)
	}

	return &volume.MountResponse{Mountpoint: mountpoint}, nil
}


func (d *rbdDriver) Unmount(r *volume.UnmountRequest) error {
	logrus.Infof("volume-rbd Name=%s Request=Unmount", r.Name)

	d.Lock()
	defer d.Unlock()


	err := d.FreeUpRbdImage(r.Name)
	if err != nil {
		return err
	}

	return nil
}


func (d *rbdDriver) Capabilities() *volume.CapabilitiesResponse {
	logrus.Infof("volume-rbd Request=Capabilities")

	return &volume.CapabilitiesResponse{
		Capabilities: volume.Capability{
			Scope: "global",
		},
	}
}

