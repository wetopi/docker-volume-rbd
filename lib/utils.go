package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)


func responseError(err string) volume.Response {
	logrus.Error(err)
	return volume.Response{Err: err}
}
