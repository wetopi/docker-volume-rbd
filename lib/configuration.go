package dockerVolumeRbd

import (
	"os"
	"strings"
)



// Read
func (d *rbdDriver) configure() {

	// set default confs:
	d.conf["pool"] = "ssd"
	d.conf["cluster"] = "ceph"
	d.conf["device_map_root"] = "/dev/rbd"

	d.loadEnvironmentRbdConfigVars();

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

