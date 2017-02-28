PLUGIN_NAME=wetopi/rbd
PLUGIN_VERSION=0.1.2

all: clean docker rootfs create

clean:
	@echo "### rm ./plugin"
	@rm -rf ./plugin

docker:
	@echo "### docker build Dockerfile.dev: compile docker-volume-rbd"
	@docker build -q -t builder -f Dockerfile.dev .
	@echo "### extract binary docker-volume-rbd from builder image"
	@docker create --name tmp builder
	@docker cp tmp:/go/bin/docker-volume-rbd .
	@docker rm -vf tmp
	@docker rmi builder
	@echo "### docker build Dockerfile: create rootfs image ready to run docker-volume-rbd"
	@docker build -q -t ${PLUGIN_NAME}:rootfs .

rootfs:
	@echo "### create rootfs directory in ./plugin/rootfs"
	@mkdir -p ./plugin/rootfs
	@docker create --name tmp ${PLUGIN_NAME}:rootfs
	@docker export tmp | tar -x --exclude=dev/ -C ./plugin/rootfs
	@echo "### copy config.json to ./plugin/"
	@cp config.json ./plugin/
	@docker rm -vf tmp

create:
	@echo "### remove existing plugin ${PLUGIN_NAME}:${PLUGIN_VERSION} if exists"
	@docker plugin rm -f ${PLUGIN_NAME}:${PLUGIN_VERSION} || true
	@echo "### create new plugin ${PLUGIN_NAME}:${PLUGIN_VERSION} from ./plugin"
	@docker plugin create ${PLUGIN_NAME}:${PLUGIN_VERSION} ./plugin

enable:
	@echo "### enable plugin ${PLUGIN_NAME}:${PLUGIN_VERSION}"
	@docker plugin enable ${PLUGIN_NAME}:${PLUGIN_VERSION}

push: clean docker rootfs create enable
	@echo "### push plugin ${PLUGIN_NAME}:${PLUGIN_VERSION}"
	@docker plugin push ${PLUGIN_NAME}:${PLUGIN_VERSION}
