FROM ubuntu:20.04 as base

MAINTAINER Joan Vega <joan@wetopi.com>

ENV GO_VERSION 1.16
ENV CEPH_VERSION pacific

RUN apt-get update && apt-get install -yq software-properties-common wget \
    && wget -q -O- 'https://download.ceph.com/keys/release.asc' | apt-key add - \
    && add-apt-repository ppa:longsleep/golang-backports \
    && apt-add-repository "deb https://download.ceph.com/debian-$CEPH_VERSION/ $(lsb_release -sc) main" \
    && rm -rf /var/lib/apt/lists/*

RUN apt-get update && apt-get install -yq \
        libcephfs-dev librbd-dev librados-dev \
    && rm -rf /var/lib/apt/lists/*


FROM base as go-builder

RUN add-apt-repository ppa:longsleep/golang-backports \
    && apt-get update \
    && apt-get install -yq \
        git golang-$GO_VERSION-go

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH
ENV PATH /usr/lib/go-$GO_VERSION/bin:$PATH
RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"

COPY go.* main.go /go/src/github.com/wetopi/docker-volume-rbd/
COPY lib /go/src/github.com/wetopi/docker-volume-rbd/lib

WORKDIR /go/src/github.com/wetopi/docker-volume-rbd

RUN set -ex  \
 && go mod tidy \
 && go install


FROM base

RUN apt-get update && apt-get install -yq \
        ceph-common \
        xfsprogs \
        kmod vim \
    && mkdir -p /run/docker/plugins /mnt/state /mnt/volumes /etc/ceph \
    && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /go/bin/docker-volume-rbd .
CMD ["docker-volume-rbd"]
