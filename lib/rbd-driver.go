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
	"log"
)

type rbdDriver struct {
	root      string             // scratch dir for mounts for this plugin
	conf      map[string]string  // ceph config params

	sync.RWMutex                 // mutex to guard operations that change volume maps or use conn

	conn      *rados.Conn        // create a connection for each API operation
	ioctx     *rados.IOContext   // context for requested pool
}


// Volume is the Docker concept which we map onto a Ceph RBD Image
type Volume struct {
	Name        string // RBD Image name
	Fstype      string
	Pool        string
	Size        uint64
	Order       int    // Specifies the object size expressed as a number of bits. The default is 22 (4KB).
	Mountpoint  string
	Device      string
}

var (
	rbdUnmapBusyRegexp = regexp.MustCompile(`^exit status 16$`)
)


// newDriver factory
// builds the driver struct,
// sets config and
// open the state file rbd-state.json
func NewDriver() (*rbdDriver, error) {

	logrus.WithField("method", "NewDriver").Debug()

	driver := &rbdDriver{
		root: filepath.Join("/mnt", "volumes"),
		conf: make(map[string]string),
	}

	err := driver.configure()
	if err != nil {
		return nil, err
	}

	return driver, nil
}



// mountPointOnHost returns the expected path on host
func (d *rbdDriver) getTheMountPointPath(name string) string {
	return filepath.Join(d.root, name)
}

// connect builds up the ceph conn and default pool
func (d *rbdDriver) connect(pool string) error {
	logrus.WithField("method", "connect").Debugf("connect to Ceph via go-ceph, with pool: %s", pool)

	// create the go-ceph Client Connection
	var cephConn *rados.Conn
	var err error

	if d.conf["cluster"] == "" {
		cephConn, err = rados.NewConnWithUser(d.conf["keyring_user"])
	} else {
		cephConn, err = rados.NewConnWithClusterAndUser(d.conf["keyring_cluster"], d.conf["keyring_user"])
	}
	if err != nil {
		logrus.WithField("method", "connect").Errorf("unable to create ceph connection to cluster=%s with user=%s: %s", d.conf["keyring_cluster"], d.conf["keyring_user"], err.Error())
		return err
	}

	// set conf
	err = cephConn.ReadDefaultConfigFile()
	if err != nil {
		logrus.WithField("method", "connect").Errorf("unable to read config /etc/ceph/ceph.conf: %s", err.Error())
		return err
	}

	err = cephConn.Connect()
	if err != nil {
		logrus.WithField("method", "connect").Errorf("unable to open the ceph cluster connection: %s", err.Error())
		return err
	}

	// can now set conn in driver
	d.conn = cephConn

	// setup the requested pool context
	ioctx, err := d.conn.OpenIOContext(pool)
	if err != nil {
		logrus.WithField("method", "connect").Errorf("unable to open context(%s): %s", pool, err.Error())
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
	logrus.Info("connection shutdown")

	if d.ioctx != nil {
		d.ioctx.Destroy()
	}
	if d.conn != nil {
		d.conn.Shutdown()
	}
}

func (d *rbdDriver) rbdImageExists(pool, findName string) (bool, error) {

	if findName == "" {
		return false, fmt.Errorf("error checking empty RBD Image name in pool %s", pool)
	}

	img := rbd.GetImage(d.ioctx, findName)
	err := img.Open(true)
	defer img.Close()

	if err != nil {
		if err == rbd.RbdErrorNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}


// createRBDImage will create a new Ceph block device and make a filesystem on it
func (d *rbdDriver) createRBDImage(pool string, imageName string, size uint64, order int, fstype string) error {
	log.Printf("INFO: Attempting to create new RBD Image pool=%s name=%s size=%dMB fstype=%s)", pool, imageName, size, fstype)

	// check that fs is valid type (needs mkfs.fstype in PATH)
	mkfs, err := exec.LookPath("mkfs." + fstype)
	if err != nil {
		msg := fmt.Sprintf("Unable to find mkfs for %s in PATH: %s", fstype, err)
		return errors.New(msg)
	}


	// create the image
	sizeInBytes := size*1024*1024
	_, err = rbd.Create(d.ioctx, imageName, sizeInBytes, order)
	if err != nil {
		return err
	}


	// map to kernel device only to initialize
	device, err := d.mapImage(pool, imageName)
	if err != nil {
		defer d.removeRBDImage(device)
		return err
	}

	// make the filesystem (give it some time)
	_, err = shWithTimeout(5 * time.Minute, mkfs, device)
	if err != nil {
		d.unmapImageDevice(device)
		defer d.removeRBDImage(device)
		return err
	}


	// unmap until a container mounts it
	defer d.unmapImageDevice(device)

	return nil
}


func (d *rbdDriver) removeRBDImage(name string) error {
	// build image struct
	rbdImage := rbd.GetImage(d.ioctx, name)

	// remove the block device image
	return rbdImage.Remove()
}
