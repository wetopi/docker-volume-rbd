# Docker volume plugin for RBD

Docker Engine managed plugin to for RBD volumes.

This plugins is managed using Docker Engine plugin system.
[https://github.com/docker/docker/blob/master/docs/extend/index.md](https://github.com/docker/docker/blob/master/docs/extend/index.md)

## Requirements

1. Docker >=1.13.1 (recommended)
2. Ceph cluster
3. Consul. We need a KV store to persist state. 

## Why an external KV store?

Plugin runs on its own container 

## Using this volume driver

### 1 - Available driver options

Key value vars to pass when installing this plugin driver:

```conf
LOG_LEVEL=[0:ErrorLevel; 1:WarnLevel; 2:InfoLevel; 3:DebugLevel] defaults to 0

CONSUL_HTTP_ADDR=127.0.0.1:8500

RBD_CONF_MAP_DEVICE_ROOT="/dev/rbd"
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

Note: Consul connection params are set using the Consul env vars: https://www.consul.io/docs/commands/#environment-variables


### 2 - Install the plugin

```bash
docker plugin install wetopi/rbd \
  --alias=wetopi/rbd \
  LOG_LEVEL=1 \
  RBD_CONF_KEYRING_USER=client.admin \
  RBD_CONF_KEYRING_KEY="ASSDFGDFGSDGSDFGDSGDSFGSD=="
```

### 3 - Create and use a volume

#### Available volume driver options:

```conf
pool: required
fstype: optional, defauls to ext4
size: optional, defaults to 512 (512MB)
order: optional, defaults to 22 (4KB Objects)
```

#### 3.A - Create a volume: 

[https://docs.docker.com/engine/reference/commandline/volume_create/](https://docs.docker.com/engine/reference/commandline/volume_create/)

```sh
docker volume create -d wetopi/rbd -o pool=rbd -o size=206 my_rbd_volume

docker volume ls
DRIVER              VOLUME NAME
local               069d59c79366294d07b9102dde97807aeaae49dc26bb9b79dd5b983f7041d069
local               11db1fa5ba70752101be90a80ee48f0282a22a3c8020c1042219ed1ed5cb0557
local               2d1f2a8fac147b7e7a6b95ca227eba2ff859325210c7280ccb73fd5beda6e67a
wetopi/rbd          my_rbd_volume
```

#### 3.B - Run a container with a previously created volume: 

```bash
docker run -it -v my_rbd_volume:/data --volume-driver=wetopi/rbd busybox sh
```

#### 3.C - Run a container with an anonymous volume: 

```bash
docker run -it -v $(docker volume create -d wetopi/rbd -o pool=rbd -o size=206):/data --volume-driver=wetopi/rbd -o pool=rbd -o size=206 busybox sh
```
*NOTE: Docker 1.13.1 does not support volume opts on docker run or docker create*

#### 3.D - Create a service with a previously created volume: 

```bash
 docker service create --replicas=1 \
   --mount type=volume,source=my_rbd_volume,destination=/var/lib/mysql,volume-driver=wetopi/rbd \
   mariadb:latest
```

#### 3.E - Create a service with an anonymous volume: 

```bash
 docker service create --replicas=1 \
   -e MYSQL_ROOT_PASSWORD=my-secret-pw \
   --mount type=volume,destination=/var/lib/mysql,volume-driver=wetopi/rbd,volume-opt=pool=rbd,volume-opt=size=512 \
   mariadb:latest
```


### 4 - Upgrading the plugin

#### 4.1 Upgrade without tag versioning:


```bash
docker plugin disable -f wetopi/rbd 
docker plugin upgrade wetopi/rbd 
```

Update setting [Optional]:
```bash
docker plugin set wetopi/rbd \
  LOG_LEVEL=2 \
  RBD_CONF_KEYRING_USER=client.admin \
  ...
```

Enable the plugin:
```bash
docker plugin enable wetopi/rbd 
```


#### 4.2 Upgrade with tag versioning:

**IMPORTANT:** *currently (docker version 1.13.1) tag/version is considered part of plugins name. This produces name inconsistency during the upgrade process. Until it's solved we release upgrades under the latest tag.*

```bash
docker plugin disable -f wetopi/rbd:0.1.2
docker plugin upgrade wetopi/rbd:0.1.2 wetopi/rbd:0.1.3 
```

## Known problems:

1. **WHEN** node restart **THEN** rbd plugin breaks (modprobe: ERROR: could not insert 'rbd': Operation not permitted //rbd: failed to load rbd kernel module (1) // rbd: sysfs write failed // In some cases useful info is found in syslog - try "dmesg | tail" or so. // rbd: map failed: (2) No such file or directory
  **SOLUTION** rbd map anyImage --pool yourPool **THEN** plugin works (host rbd can load a kernel module that plugin container can't?)
  
  
2- **WHEN** docker plugin remove  + install **THEN** containers running in plugins node lost their volumes
  **SOLUTION** restart node (swarm moves containers to another node + restart free up the Rbd mapped + mounted images) 


## Troubleshooting

### Check your plugin is enabled:

```bash
docker plugin ls

ID                  NAME                DESCRIPTION               ENABLED
fff19fa9a622        wetopi/rbd:latest   RBD plugin for Docker     true
```

### Exec an interactiva bash in plugins container:

Find the full id:

```bash
docker-runc list | grep fff19fa9a622
```

Exec an interactive shell:

```bash
docker-runc exec -t fff19fa9a622885f5bcc30c0199046761825b037b25523540647b12ccf84403be bash
```

### Log your driver:

If this container is not running or restarting, then check your docker engine log i.e. 

`tail -f /var/log/upstart/docker` 

or its equivalent 

`journalctl -f -u docker.service`


### Check Consul Key Value:

Check if state stored in Consul KV is consistent:

```bash
curl -s curl http://localhost:8500/v1/kv/docker/volume/rbd/my_rbd_volume?raw
```

### Use curl to debug plugin socket issues.

To verify if the plugin API socket that the docker daemon communicates with is responsive, use curl. In this example, we will make API calls from the docker host to volume and network plugins using curl to ensure that the plugin is listening on the said socket. For a well functioning plugin, these basic requests should work. Note that plugin sockets are available on the host under /var/run/docker/plugins/<pluginID>

```bash
curl -H "Content-Type: application/json" -XPOST -d '{}' --unix-socket /var/run/docker/plugins/546ac5b9043ce0f49552b14e9fb73dc78f1028d2da7e894ab599e6546566c0df/rbd.sock http:/VolumeDriver.List

{"Mountpoint":"","Err":"","Volumes":[{"Name":"rbd_test","Mountpoint":"","Status":null},{"Name":"demo_test","Mountpoint":"/mnt/volumes/demo_test","Status":null}],"Volume":null,"Capabilities":{"Scope":""}}
```


## Developing

You can build and publish the plugin with:

```bash
make all
```


## THANKS

https://github.com/docker/go-plugins-helpers

https://github.com/yp-engineering/rbd-docker-plugin

## LICENSE

MIT
