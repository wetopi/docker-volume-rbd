package dockerVolumeRbd

import (
	"os"
	"strings"
)



// Configure Ceph
// get conf files
// create the ceph.conf
// and the ceph.keyring used to authenticate with cephx
//
func (d *rbdDriver) configure() error {

	// set default confs:
	d.conf["cluster"] = "ceph"
	d.conf["device_map_root"] = "/dev/rbd"

	d.loadEnvironmentRbdConfigVars();

	return nil
}


// Get only the env vars starting by RBD_CONF_*
// i.e. RBD_CONF_GLOBAL_MON_HOST is saved in d.conf[global_mon_host]
//
func (d *rbdDriver) loadEnvironmentRbdConfigVars() {
	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)

		if (strings.HasPrefix(pair[0], "RBD_CONF_")) {
			configPair := strings.Split(pair[0], "RBD_CONF_")
			d.conf[strings.ToLower(configPair[1])] = pair[1]
		}
	}

}

