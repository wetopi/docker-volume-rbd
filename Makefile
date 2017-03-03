PLUGIN_NAME=wetopi/rbd
PLUGIN_VERSION=

all: clean docker rootfs create push

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
	@echo "### remove existing plugin ${PLUGIN_NAME} if exists"
	@docker plugin rm -f ${PLUGIN_NAME} || true
	@echo "### create new plugin ${PLUGIN_NAME} from ./plugin"
	@docker plugin create ${PLUGIN_NAME} ./plugin

push:
	@echo "### push plugin ${PLUGIN_NAME}"
	@docker plugin push ${PLUGIN_NAME}

enable:
	@echo "### enable plugin ${PLUGIN_NAME}"
	@docker plugin enable ${PLUGIN_NAME}

upgrade:
	@echo "### disable plugin ${PLUGIN_NAME}"
	@docker plugin disable -f ${PLUGIN_NAME}
	@echo "### upgrade plugin ${PLUGIN_NAME}"
	@docker plugin upgrade ${PLUGIN_NAME}
	@echo "### enable plugin ${PLUGIN_NAME}"
	@docker plugin enable ${PLUGIN_NAME}
