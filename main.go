package main

import (
	"os"
	"github.com/sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
	"github.com/wetopi/docker-volume-rbd/lib"
)

const socketAddress = "/run/docker/plugins/rbd.sock"



func main() {

	dockerVolumeRbdVersion := os.Getenv("PLUGIN_VERSION")

	logLevel := os.Getenv("LOG_LEVEL")

		switch logLevel {
		case "3":
			logrus.SetLevel(logrus.DebugLevel)
			break;
		case "2":
			logrus.SetLevel(logrus.InfoLevel)
			break;
		case "1":
			logrus.SetLevel(logrus.WarnLevel)
			break;
		default:
			logrus.SetLevel(logrus.ErrorLevel)
		}


	err, rbdDriver := dockerVolumeRbd.NewDriver()
	if err != nil {
		logrus.Fatal(err)
	}

	h := volume.NewHandler(rbdDriver)
	logrus.Infof("plugin(rbd) version(%s) started with log level(%s) attending socket(%s)", dockerVolumeRbdVersion, logLevel, socketAddress)
	logrus.Error(h.ServeUnix(socketAddress, 0))
}
