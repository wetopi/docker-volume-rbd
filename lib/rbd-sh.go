package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"path/filepath"
	"encoding/json"
)


//
// NOTE: the following are Shell commands for low level kernel RBD or Device
// operations - there are no go-ceph lib alternatives
//

// RBD subcommands


// mapImage will map the RBD Image to a kernel device
func (d *rbdDriver) mapImage(pool string, imageName string) (string, error) {
	logrus.Debugf("volume-rbd Name=%s Message=map image in pool(%s)", imageName, pool)

	device, err := d.rbdsh(pool, "map", imageName)
	// NOTE: ubuntu rbd map seems to not return device. if no error, assume "default" /dev/rbd/<pool>/<image> device
	if device == "" && err == nil {
		device = filepath.Join(d.conf["device_map_root"], pool, imageName)
	}

	return device, err
}

// unmapImageDevice will release the mapped kernel device
func (d *rbdDriver) unmapImageDevice(device string, imageName string) error {
	logrus.Debugf("volume-rbd Name=%s Message=rbd unmap %s", imageName, device)

	_, err := d.rbdsh("", "unmap", device)

	if err != nil {
		logrus.Errorf("volume-rbd Name=%s Message=rbd unmap %s: %s", imageName, device, err.Error())

		// NOTE: rbd unmap exits 16 if device is still being used - unlike umount.  try to recover differently in that case
		if rbdUnmapBusyRegexp.MatchString(err.Error()) {
			// can't always re-mount and not sure if we should here ... will be cleaned up once original container goes away
			logrus.Errorf("volume-rbd Name=%s Message=rbd unmap %s: unmap failed due to busy device", imageName, device)
			return err
		}

		// other error, failsafe
	}

	return nil
}

func (d *rbdDriver) mountDevice(device string, fstype, mountpoint string) error {
	logrus.Debugf("volume-rbd Message=mount -t %s %s %s", fstype, device, mountpoint)

	_, err := shWithDefaultTimeout("mount", "-t", fstype, device, mountpoint)
	return err
}

func (d *rbdDriver) unmountDevice(device string) error {
	logrus.Debugf("volume-rbd Message=umount device(%s) in mountpoint(%s)", device)
	_, err := shWithDefaultTimeout("umount", device)
	return err
}



/**
Get a list of devices mapped with our image
We do not want to relay on what driver state knows about mappings
its safer to ask rbd
 */
func getImageMappingDevices(pool string, imageName string) (error, []string) {
	logrus.Debugf("volume-rbd Name=%s Message=rbd showmapped", imageName)

	mappingsJson, err := shWithDefaultTimeout("rbd", "showmapped", "--format", "json")

	if err != nil {
		logrus.WithField("function", "getMappings").Error("failed to execute the `rbd showmapped` command.")
		return err, nil
	}

	var mappings map[string]map[string]string

	err = json.Unmarshal([]byte(mappingsJson), &mappings)
	if err != nil {
		logrus.WithField("rbd-driver.go", "getMappings").Errorf("failed to unmarshal json: %s", mappingsJson)
		return err, nil
	}

	//myImageMappings := make(map[string]map[string]string)
	var myImageMappings []string

	for _, v := range mappings {

		if v["pool"] == pool && v["name"] == imageName {
			logrus.WithField("rbd-sh.go", "getMappings").Debugf("image(%s) found in pool(%s)", v["name"], v["pool"])
			myImageMappings = append(myImageMappings, v["device"])
		}
	}

	return nil, myImageMappings
}




// rbdsh will call rbd with the given command arguments, also adding config, user and pool flags
func (d *rbdDriver) rbdsh(pool, command string, args ...string) (string, error) {
	args = append([]string{"--name", d.conf["keyring_user"], command}, args...)
	if pool != "" {
		args = append([]string{"--pool", pool}, args...)
	}
	return shWithDefaultTimeout("rbd", args...)
}


