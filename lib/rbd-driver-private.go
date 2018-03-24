package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"github.com/ceph/go-ceph/rbd"
	"golang.org/x/sys/unix"
	"path/filepath"
	"fmt"
)


func (d *rbdDriver) mapImage(imageName string) error {
	logrus.Debugf("volume-rbd Name=%s Message=rbd map", imageName)

	_, err := d.rbdsh("map", imageName)

	return err
}


func (d *rbdDriver) unmapImage(imageName string) error {
	logrus.Debugf("volume-rbd Name=%s Message=rbd unmap", imageName)

	_, err := d.rbdsh("unmap", imageName)

	if err != nil {
		// NOTE: rbd unmap exits 16 if device is still being used - unlike umount.  try to recover differently in that case
		if rbdUnmapBusyRegexp.MatchString(err.Error()) {
			return err
		}

		logrus.Errorf("volume-rbd Name=%s Message=rbd unmap: %s", imageName, err.Error())
		// other error, continue and fail safe
	}

	return nil
}


func (d *rbdDriver) mountImage(imageName string) error {

    device := d.getTheDevice(imageName)
    mountpoint := d.GetMountPointPath(imageName)

	logrus.Debugf("volume-rbd Name=%s Message=mount %s %s", imageName, device, mountpoint)

	// err := unix.Mount(device, mountpoint, "auto", 0, "")
    // note unix.Mount does not work with our aliased device, we user the sh version.
    _, err := shWithDefaultTimeout("mount", device, mountpoint)

    return err
}


func (d *rbdDriver) unmountDevice(imageName string) error {

    mountpoint := d.GetMountPointPath(imageName)
	logrus.Debugf("volume-rbd Message=umount %s", mountpoint)

	err := unix.Unmount(mountpoint, 0)

	return err
}


func (d *rbdDriver) errIfRbdImageHasWatchers(imageName string) error {

	status, err := d.rbdsh("status", imageName)
	if err != nil {
		return err
	}

    if rbdHasNoWatchersRegexp.MatchString(status) {
        return nil
    }

    return fmt.Errorf("image with %s", status)
}


func (d *rbdDriver) removeRbdImage(imageName string) error {
	logrus.Debugf("volume-rbd Name=%s Message=remove rbd image", imageName)

	rbdImage := rbd.GetImage(d.ioctx, imageName)

	return rbdImage.Remove()
}


// rbdsh will call rbd with the given command arguments, also adding config, user and pool flags
func (d *rbdDriver) rbdsh(command string, args ...string) (string, error) {

	args = append([]string{"--pool", d.conf["pool"], "--name", d.conf["keyring_user"], command}, args...)

	return shWithDefaultTimeout("rbd", args...)
}


// returns the aliased device under device_map_root
func (d *rbdDriver) getTheDevice(imageName string) string {
	return filepath.Join(d.conf["device_map_root"], d.conf["pool"], imageName)
}
