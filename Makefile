# build image for ovn overlay network cni plugin

# ovnkube-db.yaml, ovnkube-node.yaml, and onvkube-master.yaml use this image.
# This image is built from files in this directory and pushed to
# a docker registry that is accesseble on each node.

# For a user created registry, the registry must be setup ahead of time.
# The registry is configured in /etc/containers/registries.conf
# on each node in both "registries:" and "insecure_registries:" sections.

all: centos 

centos: bld
	podman build -t frr-daemonset --file Dockerfile.openshift
	podman tag frr-daemonset docker.io/frr-daemonset:latest

.PHONY: _output/frr

BRANCH = $(shell git rev-parse  --symbolic-full-name HEAD)
COMMIT = $(shell git rev-parse  HEAD)
bld: _output/frr
	echo "ref: ${BRANCH}  commit: ${COMMIT}" > git_info
