FROM ubuntu:16.04

MAINTAINER Joan Vega <joan@wetopi.com>

ENV CEPH_VERSION jewel

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



COPY docker-volume-rbd /
COPY templates /templates

CMD ["docker-volume-rbd"]
