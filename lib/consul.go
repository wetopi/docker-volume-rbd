package dockerVolumeRbd

import (
	"github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	"encoding/json"
)

// TODO create a "state" interface and factory in order to use different state backends


const KEY_PREFIX = "docker/volume/rbd/"

func (d *rbdDriver) setVolume(v *Volume) error {

	err, kv := getConnection()
	if err != nil {
		return err
	}

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	logrus.Debugf("volume-rbd Message=consul.setVolume(%s)", v.Name)

	p := &api.KVPair{Key: getKeyFromName(v.Name), Value: data}
	_, err = kv.Put(p, nil)
	if err != nil {
		return err
	}

	return nil

}

func (d *rbdDriver) deleteVolume(name string) (error) {
	logrus.Debugf("volume-rbd Message=consul.deleteVolume(%s)", name)

	err, kv := getConnection()
	if err != nil {
		return err
	}

	_, err = kv.Delete(getKeyFromName(name), nil)
	if err != nil {
		return err
	}

	return nil
}

func (d *rbdDriver) getVolume(name string) (error, *Volume) {
	logrus.Debugf("volume-rbd Message=consul.getVolume(%s)", name)

	err, kv := getConnection()
	if err != nil {
		return err, nil
	}

	pair, _, err := kv.Get(getKeyFromName(name), nil)
	if err != nil {
		return err, nil
	}

	v := Volume{}

	if (pair != nil) {
		if err := json.Unmarshal(pair.Value, &v); err != nil {
			return err, nil
		}
	}

	return nil, &v
}

func (d *rbdDriver) getVolumes() (error, *map[string]*Volume) {
	logrus.Debugf("volume-rbd Message=consul.getVolumes()")

	err, kv := getConnection()
	if err != nil {
		return err, nil
	}

	pairs, _, err := kv.List(getKeyFromName(""), nil)
	if err != nil {
		return err, nil
	}

	volumes := map[string]*Volume{}

	for _, pair := range pairs {

		v := Volume{}

		if err := json.Unmarshal(pair.Value, &v); err != nil {
			return err, nil
		}

		volumes[v.Name] = &v
	}

	return nil, &volumes
}

// This will pool and reuse idle connections to Consul
//
// All connection params are set using the Consul API env vars:
// https://www.consul.io/docs/commands/#environment-variables
//
func getConnection() (error, *api.KV) {
	logrus.Debugf("volume-rbd Message=consul.getConnection()")

	config := api.DefaultConfig()

	client, err := api.NewClient(config)
	if err != nil {
		return err, nil
	}

	// Get a handle to the KV API
	return nil, client.KV()

}

func getKeyFromName(name string) string {
	return KEY_PREFIX + name
}
