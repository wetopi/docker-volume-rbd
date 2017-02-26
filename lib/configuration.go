package dockerVolumeRbd

import (
	"os"
	"text/template"
	"github.com/Sirupsen/logrus"
	"strings"
)



// Configure Ceph
// get conf files
// create the ceph.conf
// and the ceph.keyring used to authenticate with cephx
//
func (d *rbdDriver) configure() error {

	var err error

	// set default conf:
	d.conf["cluster"] = "ceph"

	d.loadEnvironmentRbdConfigVars();

	err = createConf("templates/ceph.conf.tmpl", "/etc/ceph/ceph.conf", d.conf);
	if err != nil {
		return err
	}

	err = createConf("templates/ceph.keyring.tmpl", "/etc/ceph/ceph.keyring", d.conf);
	if err != nil {
		return err
	}

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

func createConf(templateFile string, outputFile string, config map[string]string) error {

	t, err := template.ParseFiles(templateFile)
	if err != nil {
		logrus.WithField("utils", "createConf").Error(err)
		return err
	}

	f, err := os.Create(outputFile)
	if err != nil {
		logrus.WithField("utils", "createConf").Error(err)
		return err
	}

	err = t.Execute(f, config)
	if err != nil {
		logrus.WithField("utils", "createConf").Error(err)
		return err
	}

	f.Close()

	return nil
}

