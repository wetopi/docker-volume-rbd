package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"github.com/ceph/go-ceph/rados"
	"github.com/ceph/go-ceph/rbd"
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
)


// newDriver factory
// builds the driver struct,
// sets config and
// open the state file rbd-state.json
func NewDriver() (error, *rbdDriver) {
	logrus.WithField("rbd-driver.go", "rbdDriver.NewDriver").Debug("launching rbd driver")

	driver := &rbdDriver{
		root: filepath.Join("/mnt", "volumes"),
		conf: make(map[string]string),
	}

	err := driver.configure()
	if err != nil {
		return err, nil
	}

	return nil, driver
}



// mountPointOnHost returns the expected path on host
func (d *rbdDriver) getTheMountPointPath(name string) string {
	return filepath.Join(d.root, name)
}

// connect builds up the ceph conn and default pool
func (d *rbdDriver) connect(pool string) error {
	logrus.WithField("rbd-driver.go", "rbdDriver.connect").Debugf("connect to Ceph pool(%s)", pool)

	// create the go-ceph Client Connection
	var cephConn *rados.Conn
	var err error

	if d.conf["cluster"] == "" {
		cephConn, err = rados.NewConnWithUser(d.conf["keyring_user"])
	} else {
		cephConn, err = rados.NewConnWithClusterAndUser(d.conf["keyring_cluster"], d.conf["keyring_user"])
	}
	if err != nil {
		logrus.WithField("rbd-driver.go", "rbdDriver.connect").Errorf("unable to create ceph connection to cluster(%s) with user(%s): %s", d.conf["keyring_cluster"], d.conf["keyring_user"], err.Error())
		return err
	}

	// set conf
	err = cephConn.ReadDefaultConfigFile()
	if err != nil {
		logrus.WithField("rbd-driver.go", "rbdDriver.connect").Errorf("unable to read config /etc/ceph/ceph.conf: %s", err.Error())
		return err
	}

	err = cephConn.Connect()
	if err != nil {
		logrus.WithField("rbd-driver.go", "rbdDriver.connect").Errorf("unable to open the ceph cluster connection: %s", err.Error())
		return err
	}

	// can now set conn in driver
	d.conn = cephConn

	// setup the requested pool context
	ioctx, err := d.conn.OpenIOContext(pool)
	if err != nil {
		logrus.WithField("rbd-driver.go", "rbdDriver.connect").Errorf("unable to open context(%s): %s", pool, err.Error())
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
	logrus.WithField("rbd-driver.go", "rbdDriver.shutdown").Debug("connection shutdown from Ceph")

	if d.ioctx != nil {
		d.ioctx.Destroy()
	}
	if d.conn != nil {
		d.conn.Shutdown()
	}
}

func (d *rbdDriver) rbdImageExists(pool string, findName string) (error, bool) {
	logrus.WithField("rbd-driver.go", "rbdDriver.rbdImageExists").Debugf("checking if RBD Image(%s) name in pool %s", findName, pool)

	if findName == "" {
		return fmt.Errorf("error checking empty name in pool(%s)", pool), false
	}

	img := rbd.GetImage(d.ioctx, findName)
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


// createRbdImage will create a new Ceph block device and make a filesystem on it
func (d *rbdDriver) createRbdImage(pool string, imageName string, size uint64, order int, fstype string) error {
	logrus.WithField("rbd-driver.go", "rbdDriver.createRbdImage").Debugf("create image in pool(%s) name(%s) size(%dMB) fstype(%s)", pool, imageName, size, fstype)

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
		d.unmapImageDevice(pool, imageName)
		defer d.removeRbdImage(device)
		return err
	}


	// unmap until a container mounts it
	defer d.unmapImageDevice(pool, imageName)

	return nil
}


func (d *rbdDriver) removeRbdImage(name string) error {
	logrus.WithField("rbd-driver.go", "rbdDriver.removeRbdImage").Debugf("remove image(%s)", name)

	// build image struct
	rbdImage := rbd.GetImage(d.ioctx, name)

	// remove the block device image
	return rbdImage.Remove()
}


func (d *rbdDriver) freeUpRbdImage(pool string, imageName string, mountpoint string) error {

	// unmount
	err := d.unmountDevice(pool, imageName)
	if err != nil {
		// warn and continue. unmap knows if device is being used
		logrus.WithField("rbd-driver.go", "rbdDriver.freeUpRbdImage").Warnf("unable to unmount image(%s):", imageName, err)
	}

	// remove mountpoint
	err = os.Remove(mountpoint)
	if err != nil {
		logrus.WithField("rbd-driver.go", "rbdDriver.freeUpRbdImage").Warnf("unable to remove image(%s) mountpoint(%s): %s", imageName, mountpoint, err)
	}

	// unmap
	err = d.unmapImageDevice(pool, imageName)

	return err
}
