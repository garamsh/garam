#!/bin/sh
# enable-containerd-image-store switches a disposable runner's Docker
# to the containerd image store, merged into its daemon.json, and fails
# unless the restarted daemon reports it. The default `docker` driver
# exports a GitHub Actions cache only on that store; every other driver
# runs moby/buildkit from Docker Hub, the limit #1337 left (#1519).
set -eu

conf=/etc/docker/daemon.json
current='{}'
if sudo test -s "$conf"; then current=$(sudo cat "$conf"); fi
printf '%s' "$current" \
	| jq '.features["containerd-snapshotter"] = true' \
	| sudo tee "$conf" >/dev/null
sudo systemctl restart docker

driver=$(docker info -f '{{.DriverStatus}}')
case $driver in
*io.containerd.snapshotter*) echo "enable-containerd-image-store: $driver" ;;
*)
	echo "enable-containerd-image-store: the daemon reports $driver after the restart, not the containerd image store" >&2
	exit 1
	;;
esac
