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
	"errors"
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
	rbdBusyRegexp = regexp.MustCompile(`ret=-16$`)
)


// newDriver factory
// builds the driver struct,
// sets config and
// open the state file rbd-state.json
func NewDriver() (error, *rbdDriver) {
	logrus.Debugf("volume-rbd Message=launching rbd driver")

	driver := &rbdDriver{
		root: filepath.Join("/mnt", "volumes"),
		conf: make(map[string]string),
	}

	driver.configure()

	return nil, driver
}


// connect builds up the ceph connection and default pool
func (d *rbdDriver) connect(pool string) error {
	logrus.Debugf("volume-rbd Message=connect to ceph pool(%s)", pool)

	// create the go-ceph Client Connection
	var cephConn *rados.Conn
	var err error

	if d.conf["cluster"] == "" {
		cephConn, err = rados.NewConnWithUser(d.conf["keyring_user"])
	} else {
		cephConn, err = rados.NewConnWithClusterAndUser(d.conf["cluster"], d.conf["keyring_user"])
	}
	if err != nil {
		logrus.Errorf("volume-rbd Message=unable to create ceph connection to cluster(%s) with user(%s): %s", d.conf["cluster"], d.conf["keyring_user"], err.Error())
		return err
	}

	// set conf
	err = cephConn.ReadDefaultConfigFile()
	if err != nil {
		logrus.Errorf("volume-rbd Message=unable to read config /etc/ceph/ceph.conf: %s", err.Error())
		return err
	}

	err = cephConn.Connect()
	if err != nil {
		logrus.Errorf("volume-rbd Message=unable to open the ceph cluster connection: %s", err.Error())
		return err
	}

	// can now set conn in driver
	d.conn = cephConn

	// setup the requested pool context
	ioctx, err := d.conn.OpenIOContext(pool)
	if err != nil {
		logrus.Errorf("volume-rbd Message=unable to open context(%s): %s", pool, err.Error())
		return err
	}

	d.ioctx = ioctx

	return nil
}


// shutdown closes the connection - maybe not needed unless we recreate conn?
// more info:
// - https://github.com/ceph/go-ceph/blob/f251b53/rados/ioctx.go#L140
// - http://docs.ceph.com/docs/master/rados/api/librados/
func (d *rbdDriver) shutdown() {
	logrus.Debugf("volume-rbd Message=connection shutdown from ceph")

	if d.ioctx != nil {
		d.ioctx.Destroy()
	}
	if d.conn != nil {
		d.conn.Shutdown()
	}
}


func (d *rbdDriver) rbdImageExists(pool string, imageName string) (error, bool) {
	logrus.Debugf("volume-rbd Name=%s Message=checking if exists rbd image in pool(%s)", imageName, pool)

	if imageName == "" {
		return fmt.Errorf("error checking empty imageName in pool(%s)", pool), false
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


func (d *rbdDriver) createRbdImage(pool string, imageName string, size uint64, order int, fstype string) error {
	logrus.Debugf("volume-rbd Name=%s Message=create image in pool(%s) with size(%dMB) and fstype(%s)", imageName, pool, size, fstype)

	// check that fs is valid type (needs mkfs.fstype in PATH)
	mkfs, err := exec.LookPath("mkfs." + fstype)
	if err != nil {
		return errors.New(fmt.Sprintf("unable to find mkfs.(%s): %s", fstype, err))
	}


	// create the image
	sizeInBytes := size * 1024 * 1024
	_, err = rbd.Create(d.ioctx, imageName, sizeInBytes, order)
	if err != nil {
		return err
	}


	// map to kernel device only to initialize
	device, err := d.mapImage(pool, imageName)
	if err != nil {
		defer d.removeRbdImage(device)
		return err
	}

	// make the filesystem (give it some time)
	_, err = shWithTimeout(5 * time.Minute, mkfs, device)
	if err != nil {
		d.unmapImageDevice(device, imageName)
		defer d.removeRbdImage(device)
		return err
	}


	// unmap until a container mounts it
	defer d.unmapImageDevice(device, imageName)

	return nil
}


func (d *rbdDriver) removeRbdImage(imageName string) error {
	logrus.Debugf("volume-rbd Name=%s Message=remove rbd image", imageName)

	rbdImage := rbd.GetImage(d.ioctx, imageName)

	return rbdImage.Remove()
}


/**
In case of race condition (the unmount and remove can be called async from different swarm nodes)
we try to remove up to 3 times.
 */
func (d *rbdDriver) removeRbdImageWithRetries(imageName string) error {

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


func (d *rbdDriver) mountRbdImage(pool string, imageName string, fstype string) (err error, device string, mountpoint string) {
	logrus.Debugf("volume-rbd Name=%s Message=map and mount rbd image", imageName)

	mountpoint = d.getTheMountPointPath(imageName)


	// map the RBD image
	device, err = d.mapImage(pool, imageName)
	if err != nil {
		return fmt.Errorf("unable to map rbd image to kernel device: %s", err), "", ""
	}


	// check for mountdir - create if necessary
	err = os.MkdirAll(mountpoint, os.ModeDir | os.FileMode(int(0775)))
	if err != nil {
		defer d.freeUpRbdImage(pool, imageName, mountpoint)
		return fmt.Errorf("unable to make mountpoint(%s): %s", mountpoint, err), "", ""
	}


	// mount
	err = d.mountDevice(device, fstype, mountpoint)
	if err != nil {
		defer d.freeUpRbdImage(pool, imageName, mountpoint)
		return fmt.Errorf("unable to mount -t %s %s %s: %s", fstype, device, mountpoint, err), "", ""
	}

	return err, device, mountpoint

}

/**
Freeing Up an RBD image means
first find all maps done on current plugin node
then unmount + unmap and finally remove mountpoint dir

We do all this silently, we want the freeUp process idempotent
 */
func (d *rbdDriver) freeUpRbdImage(pool string, imageName string, mountpoint string) error {
	logrus.Debugf("volume-rbd Name=%s Message=free up image", imageName)

	err, devices := getImageMappingDevices(pool, imageName)
	if err != nil {
		return err
	}

	for _, device := range devices {

		// silently unmount
		err := d.unmountDevice(device, mountpoint)
		if err != nil {
			// warn and continue. unmap knows if device is being used
			logrus.Warnf("volume-rbd Name=%s Message=unable to unmount image device(%s):", imageName, device, err)
		}

		// silently unmap
		err = d.unmapImageDevice(device, imageName)
		if err != nil {
			// warn and continue. unmap knows if device is being used
			logrus.Warnf("volume-rbd Name=%s Message=unable to unmap image device(%s):", imageName, device, err)
		}
	}


	// remove mountpoint
	if mountpoint != "" {
		err = os.Remove(mountpoint)
		if err != nil {
			logrus.Warnf("volume-rbd Name=%s Message=unable to remove image mountpoint(%s): %s", imageName, mountpoint, err)
		}
	}

	return nil
}



// mountPointOnHost returns the expected path on host
func (d *rbdDriver) getTheMountPointPath(name string) string {
	return filepath.Join(d.root, name)
}

