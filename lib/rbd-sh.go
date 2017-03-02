package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"path/filepath"
)


//
// NOTE: the following are Shell commands for low level kernel RBD or Device
// operations - there are no go-ceph lib alternatives
//

// RBD subcommands


// mapImage will map the RBD Image to a kernel device
func (d *rbdDriver) mapImage(pool string, imageName string) (string, error) {
	device, err := d.rbdsh(pool, "map", imageName)
	// NOTE: ubuntu rbd map seems to not return device. if no error, assume "default" /dev/rbd/<pool>/<image> device
	if device == "" && err == nil {
		device = filepath.Join(d.conf["device_map_root"], pool, imageName)
	}

	return device, err
}

// unmapImageDevice will release the mapped kernel device
func (d *rbdDriver) unmapImageDevice(pool string, imageName string) error {

	_, err := d.rbdsh(pool, "unmap", imageName)

	if err != nil {
		logrus.WithField("rbd-sh.go", "unmapImageDevice").Errorf("rbd unmap %s --pool %s: %s", imageName, pool, err.Error())

		// NOTE: rbd unmap exits 16 if device is still being used - unlike umount.  try to recover differently in that case
		if rbdUnmapBusyRegexp.MatchString(err.Error()) {
			// can't always re-mount and not sure if we should here ... will be cleaned up once original container goes away
			logrus.WithField("rbd-sh.go", "unmapImageDevice").Errorf("unmap failed due to busy device, early exit from this Unmount request")
			return err
		}

		// other error, failsafe
	}

	return nil
}

func (d *rbdDriver) mountDevice(pool string, imageName string, fstype, mountdir string) error {
	device := filepath.Join(d.conf["device_map_root"], pool, imageName)
	_, err := shWithDefaultTimeout("mount", "-t", fstype, device, mountdir)
	return err
}

func (d *rbdDriver) unmountDevice(pool string, imageName string) error {
	device := filepath.Join(d.conf["device_map_root"], pool, imageName)
	_, err := shWithDefaultTimeout("umount", device)
	return err
}

// UTIL

// rbdsh will call rbd with the given command arguments, also adding config, user and pool flags
func (d *rbdDriver) rbdsh(pool, command string, args ...string) (string, error) {
	args = append([]string{"--name", d.conf["keyring_user"], command}, args...)
	if pool != "" {
		args = append([]string{"--pool", pool}, args...)
	}
	return shWithDefaultTimeout("rbd", args...)
}


