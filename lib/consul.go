package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	"encoding/json"
	"os"
)

const KEY_PREFIX = "docker/volume/rbd/	"

func (d *rbdDriver) setVolume(v *Volume) error {

	err, kv := getConnection()
	if err != nil {
		return err
	}

	data, err := json.Marshal(v)
	if err != nil {
		logrus.WithField("consul", "saveVolume").Error(err)
		return err
	}

	p := &api.KVPair{Key: getKeyFromName(v.Name), Value: data}
	_, _, err = kv.CAS(p, nil)
	if err != nil {
		logrus.WithField("consul", "saveVolume").Error(err)
		panic(err)
	}

	return nil

}

func (d *rbdDriver) deleteVolume(name string) (error) {

	err, kv := getConnection()
	if err != nil {
		return err
	}

	_, err = kv.Delete(getKeyFromName(name), nil)
	if err != nil {
		logrus.WithField("consul", "deleteVolume").Error(err)
		return err
	}

	return nil
}

func (d *rbdDriver) getVolume(name string) (error, *Volume) {

	err, kv := getConnection()
	if err != nil {
		return err, nil
	}

	pair, _, err := kv.Get(getKeyFromName(name), nil)
	if err != nil {
		logrus.WithField("consul", "getVolume").Error(err)
		return err, nil
	}

	v := Volume{""}

	if (pair != nil) {
		logrus.WithField("consul.go", "getVolume").Debugf("pair: %s=%s ", pair.Key, pair.Value)

		if err := json.Unmarshal(pair.Value, &v); err != nil {
			logrus.WithField("consul", "getVolume").Error(err)
			return err, nil
		}
	}

	return nil, &v
}

func (d *rbdDriver) getVolumes() (error, *map[string]*Volume) {

	err, kv := getConnection()
	if err != nil {
		return err, nil
	}

	pairs, _, err := kv.List(getKeyFromName(""), nil)
	if err != nil {
		logrus.WithField("consul", "getVolumes").Error(err)
		return err, nil
	}

	volumes := map[string]*Volume{}

	for _, pair := range pairs {

		v := Volume{}

		if err := json.Unmarshal(pair.Value, &v); err != nil {
			logrus.WithField("consul", "getVolumes").Error(err)
			return err, nil
		}

		volumes[v.Name] = &v
	}

	return nil, &volumes
}

func getConnection() (error, *api.KV) {

	config := api.DefaultConfig()

	config.Address = os.Getenv("CONSUL_ADDRESS")

	if (config.Address == "") {
		config.Address = "localhost:8500"
	}

	client, err := api.NewClient(config)
	if err != nil {
		logrus.WithField("consul", "getConnection").Error(err)
		return err, nil
	}

	// Get a handle to the KV API
	return nil, client.KV()

}

func getKeyFromName(name string) string {
	return KEY_PREFIX + name
}
