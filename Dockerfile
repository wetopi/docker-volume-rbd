FROM golang:1.14 as builder

MAINTAINER Joan Vega <joan@wetopi.com>

COPY . /go/src/github.com/wetopi/docker-volume-rbd
WORKDIR /go/src/github.com/wetopi/docker-volume-rbd

RUN apt-get update \
    && apt-get install -y -q \
       gcc libc-dev \
       librados-dev \
       librbd-dev \
       \
    && set -ex \
    && go get -u github.com/golang/dep/cmd/dep \
    && dep ensure \
    && go install

CMD ["/go/bin/docker-volume-rbd"]





FROM ubuntu:18.04

ENV CEPH_VERSION nautilus

RUN apt-get update \
    && apt-get install -y -q \
       librados-dev \
       librbd-dev \
       ceph-common \
       xfsprogs \
       \
       \
       kmod vim \
       \
       \
    && mkdir -p /run/docker/plugins /mnt/state /mnt/volumes /etc/ceph \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* /tmp/*

COPY --from=builder /go/bin/docker-volume-rbd .
CMD ["docker-volume-rbd"]
