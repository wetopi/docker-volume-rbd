package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"github.com/ceph/go-ceph/rados"
	"github.com/ceph/go-ceph/rbd"
	"github.com/wetopi/docker-volume-rbd/lib/try"
	"sync"
	"path/filepath"
	"fmt"
	"os/exec"
	"time"
	"regexp"
	"os"
)

type rbdDriver struct {
	root  string            // scratch dir for mounts for this plugin
	conf  map[string]string // ceph config params

	sync.RWMutex            // mutex to guard operations that change volume maps or use conn

	conn  *rados.Conn       // create a connection for each API operation
	ioctx *rados.IOContext  // context for requested pool
}

var (
	rbdUnmapBusyRegexp = regexp.MustCompile(`^exit status 16$`)
    rbdHasNoWatchersRegexp = regexp.MustCompile(`^Watchers: none$`)
	rbdBusyRegexp = regexp.MustCompile(`ret=-16$`)
)


// newDriver factory
// builds the driver struct,
// and sets config and
func NewDriver() (error, *rbdDriver) {
	logrus.Debugf("volume-rbd Message=launching rbd driver")

	driver := &rbdDriver{
		root: filepath.Join("/mnt", "volumes"),
		conf: make(map[string]string),
	}

	driver.configure()

	return nil, driver
}


// connect builds up the ceph connection
func (d *rbdDriver) Connect() error {
	logrus.Debugf("volume-rbd Message=connect to ceph pool(%s)", d.conf["pool"])

	// create the go-ceph Client Connection
	var cephConn *rados.Conn
	var err error

	if d.conf["cluster"] == "" {
		cephConn, err = rados.NewConnWithUser(d.conf["keyring_user"])
	} else {
		cephConn, err = rados.NewConnWithClusterAndUser(d.conf["cluster"], d.conf["keyring_user"])
	}
	if err != nil {
		return fmt.Errorf("unable to create ceph connection to cluster(%s) with user(%s): %s", d.conf["cluster"], d.conf["keyring_user"], err)
	}

	err = cephConn.ReadDefaultConfigFile()
	if err != nil {
		return fmt.Errorf("unable to read default config /etc/ceph/ceph.conf: %s", err)
	}

	err = cephConn.Connect()
	if err != nil {
		return fmt.Errorf("unable to open the ceph cluster connection: %s", err)
	}

	// can now set conn in driver
	d.conn = cephConn

	// setup the requested pool context
	ioctx, err := d.conn.OpenIOContext(d.conf["pool"])
	if err != nil {
		return fmt.Errorf("unable to open context(%s): %s", d.conf["pool"], err)
	}

	d.ioctx = ioctx

	return nil
}


func (d *rbdDriver) Shutdown() {
	logrus.Debugf("volume-rbd Message=connection shutdown from ceph")

	if d.ioctx != nil {
		d.ioctx.Destroy()
	}
	if d.conn != nil {
		d.conn.Shutdown()
	}
}


func (d *rbdDriver) RbdImageExists(imageName string) (error, bool) {
	logrus.Debugf("volume-rbd Name=%s Message=checking if exists rbd image in pool(%s)", imageName, d.conf["pool"])

	if imageName == "" {
		return fmt.Errorf("error checking empty imageName in pool(%s)", d.conf["pool"]), false
	}

	img := rbd.GetImage(d.ioctx, imageName)
	err := img.Open(true)
	defer img.Close()

	if err != nil {
		if err == rbd.RbdErrorNotFound {
			return nil, false
		}
		return err, false
	}
	return nil, true
}


func (d *rbdDriver) GetRbdImages() (err error, imageNames []string) {

	imageNames, err = rbd.GetImageNames(d.ioctx)

    return err, imageNames
}


func (d *rbdDriver) CreateRbdImage(imageName string, size uint64, order int, fstype string) error {
	logrus.Debugf("volume-rbd Name=%s Message=create image in pool(%s) with size(%dMB) and fstype(%s)", imageName, d.conf["pool"], size, fstype)

	// check that fs is valid type (needs mkfs.fstype in PATH)
	mkfs, err := exec.LookPath("mkfs." + fstype)
	if err != nil {
		return fmt.Errorf("unable to find mkfs.(%s): %s", fstype, err)
	}


	// create the image
	sizeInBytes := size * 1024 * 1024
	_, err = rbd.Create(d.ioctx, imageName, sizeInBytes, order)
	if err != nil {
		return err
	}


	// map to kernel to let initialize fs
	err = d.mapImage(imageName)
	if err != nil {
		defer d.removeRbdImage(imageName)
		return err
	}

	// make the filesystem (give it some time)
	device := d.getTheDevice(imageName)
	_, err = shWithTimeout(5 * time.Minute, mkfs, device)
	if err != nil {
		d.unmapImage(imageName)
		defer d.removeRbdImage(imageName)
		return err
	}


	// leave the image unmaped
	defer d.unmapImage(imageName)

	return nil
}


func (d *rbdDriver) RemoveRbdImageWithRetries(imageName string) error {

	err := try.Do(func(attempt int) (bool, error) {
		var err error
		err = d.removeRbdImage(imageName)

		if err != nil && rbdBusyRegexp.MatchString(err.Error()) {
			const MAX_ATTEMPTS = 3;
			time.Sleep(2 * time.Second)

			return attempt < MAX_ATTEMPTS, err
		}

		return false, err
	})

	return err
}


func (d *rbdDriver) MountRbdImage(imageName string) (err error, mountpoint string) {
	logrus.Debugf("volume-rbd Name=%s Message=MountRbdImage map and mount", imageName)


    err = d.errIfRbdImageHasWatchers(imageName)
	if err != nil {
		return err, ""
	}



	err = d.mapImage(imageName)
	if err != nil {
		return fmt.Errorf("unable to map: %s", imageName, err), ""
	}


	// check for mountdir - create if necessary
	mountpoint = d.GetMountPointPath(imageName)
	err = os.MkdirAll(mountpoint, os.ModeDir | os.FileMode(int(0775)))
	if err != nil {
		defer d.FreeUpRbdImage(imageName)
		return fmt.Errorf("unable to make mountpoint: %s", mountpoint, err), ""
	}


	// mount
	err = d.mountImage(imageName)
	if err != nil {
		defer d.FreeUpRbdImage(imageName)
		return fmt.Errorf("unable to mount: %s", err), ""
	}

	return err, mountpoint

}

/**
 * Freeing Up an RBD image means
 * unmount + unmap and remove mountpoint
 *
 * We do all this silently, we want the freeUp process idempotent
 */
func (d *rbdDriver) FreeUpRbdImage(imageName string) error {
	logrus.Debugf("volume-rbd Name=%s Message=free up image", imageName)


    // silently unmount
    err := d.unmountDevice(imageName)
    if err != nil {
        logrus.Warnf("volume-rbd Name=%s Message=unable to unmount:", imageName, err)
    }


    // silently unmap
    err = d.unmapImage(imageName)
    if err != nil {
        logrus.Warnf("volume-rbd Name=%s Message=unable to unmap:", imageName, err)
    }


	// silently remove mountpoint
	mountpoint := d.GetMountPointPath(imageName)

    err = os.Remove(mountpoint)
    if err != nil {
        logrus.Warnf("volume-rbd Name=%s Message=unable to remove mountpoint(%s): %s", imageName, mountpoint, err)
    }


	return nil
}



// returns the expected path inside plugin container. The named "propagated mount"
func (d *rbdDriver) GetMountPointPath(imageName string) string {
	return filepath.Join(d.root, imageName)
}