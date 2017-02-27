# Docker volume plugin for RBD

Docker Engine managed plugin to manage RBD volumes.

[![Go Report Card](https://goreportcard.com/badge/github.com/wetopi/docker-volume-rbd)](https://goreportcard.com/report/github.com/wetopi/docker-volume-rbd)

This plugins is managed using Docker Engine plugin system.
[https://github.com/docker/docker/blob/master/docs/extend/index.md](https://github.com/docker/docker/blob/master/docs/extend/index.md)


## Usage

### 1 - Configure options

Key value vars to pass when installing this plugin driver:

```
DEBUG=1

CONSUL_ADDRESS=localhost:8500

RBD_CONF_CLUSTER=ceph

RBD_CONF_KEYRING_USER=client.admin
RBD_CONF_KEYRING_KEY="ASSDFGDFGSDGSDFGDSGDSFGSD=="
RBD_CONF_KEYRING_CAPS_MDS="allow *"
RBD_CONF_KEYRING_CAPS_MON="allow *"
RBD_CONF_KEYRING_CAPS_OSD="allow *"

RBD_CONF_GLOBAL_FSID="56779a1a-2dc1-1122-a152-f21221233dsd"
RBD_CONF_GLOBAL_MON_INITIAL_MEMBERS="ceph-mon1, ceph-mon2, ceph-mon3"
RBD_CONF_GLOBAL_MON_HOST="192.168.101.1,192.168.101.2,192.168.101.3"
RBD_CONF_GLOBAL_AUTH_CLUSTER_REQUIRED=cephx
RBD_CONF_GLOBAL_AUTH_SERVICE_REQUIRED=cephx
RBD_CONF_GLOBAL_AUTH_CLIENT_REQUIRED=cephx
RBD_CONF_GLOBAL_OSD_POOL_DEFAULT_SIZE=2
RBD_CONF_GLOBAL_PUBLIC_NETWORK="192.168.100.0/23"
RBD_CONF_CLIENT_RBD_DEFAULT_FEATURES=1
RBD_CONF_MDS_SESSION_TIMEOUT=120
RBD_CONF_MDS_SESSION_AUTOCLOSE=600
```

### 2 - Install the plugin

```
$ docker plugin install wetopi/rbd \
  DEBUG=1 \
  RBD_CONF_KEYRING_USER=client.admin \
  RBD_CONF_KEYRING_KEY=ASSDFGDFGSDGSDFGDSGDSFGSD== \
  ...
```

### 3 - Create a volume

Available options:

name: required
pool: required
fstype: optional, defauls to ext4
size: optional, defaults to 512 (512MB)
order: optional, defaults to 22 (4KB Objects)


[https://docs.docker.com/engine/reference/commandline/volume_create/](https://docs.docker.com/engine/reference/commandline/volume_create/)

```
$ docker volume create -d wetopi/rbd:0.1.1 -o name=my_rbd_volume -o pool=rbd -o size=206 my_rbd_volume

$ docker volume ls
DRIVER              VOLUME NAME
local               069d59c79366294d07b9102dde97807aeaae49dc26bb9b79dd5b983f7041d069
local               11db1fa5ba70752101be90a80ee48f0282a22a3c8020c1042219ed1ed5cb0557
local               2d1f2a8fac147b7e7a6b95ca227eba2ff859325210c7280ccb73fd5beda6e67a
wetopi/rbd          my_rbd_volume
```

### 4 - Use the volume

```
$ docker run -it -v my_rbd_volume:/data --volume-driver=wetopi/rbd:0.1.1 busybox sh
```

## Troubleshooting

### Check your plugin is enabled:

```sh
$ docker plugin ls

ID                  NAME                DESCRIPTION               ENABLED
fff19fa9a622        wetopi/rbd:0.1.1    RBD plugin for Docker     true
```

### Exec an interactiva bash in plugins container:

```
docker-runc exec -t fff19fa9a622 bash
```

If this container is not running or restarting, then check your docker engine log i.e. `tail -f /var/log/upstart/docker` 
or its equivalent `journalctl -f -u docker.service`


### Check Consul Key Value store:

```bash
curl 
```



## THANKS

https://github.com/docker/go-plugins-helpers

## LICENSE

MIT
