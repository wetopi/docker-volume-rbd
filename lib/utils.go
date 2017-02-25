package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"encoding/json"
	"io/ioutil"
	"github.com/docker/go-plugins-helpers/volume"
)

func (d *rbdDriver) saveState() {
	data, err := json.Marshal(d.volumes)
	if err != nil {
		logrus.WithField("statePath", d.statePath).Error(err)
		return
	}

	if err := ioutil.WriteFile(d.statePath, data, 0644); err != nil {
		logrus.WithField("savestate", d.statePath).Error(err)
	}
}

func responseError(err string) volume.Response {
	logrus.Error(err)
	return volume.Response{Err: err}
}
