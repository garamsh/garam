#!/bin/sh
# enable-containerd-image-store switches a GitHub-hosted runner's Docker
# daemon to the containerd image store and restarts it. The default
# `docker` driver exports a build cache to the GitHub Actions cache only
# on that store, and every other driver runs BuildKit from
# moby/buildkit, which is published on Docker Hub alone: an anonymous
# pull from the limit #1337 moved the bases away from (#1519).
#
# It merges into whatever daemon.json the runner image ships rather than
# replacing it, and fails unless the restarted daemon reports the store.
# It edits the host's Docker, so it is for a disposable runner and not a
# contributor's machine.
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
