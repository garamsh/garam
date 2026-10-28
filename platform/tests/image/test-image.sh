#!/bin/sh
# Checks the test image (ADR-0092) the way its consumer reads it: with
# `docker create` and `docker cp`, never running the image. The three
# paths are there, both executables are static linux/amd64 and answer
# on this host, /migrations/ is this checkout's migrations/, and the
# revision label names HEAD.
#
#   tests/image/test-image.sh <image>
set -eu

image=${1:?usage: tests/image/test-image.sh <image>}
dir=$(mktemp -d)
container=
passed=0

cleanup() {
	if [ -n "$container" ]; then docker rm -f "$container" >/dev/null 2>&1 || true; fi
	rm -rf "$dir"
}
trap cleanup EXIT INT TERM

fail() {
	echo "test-image-e2e: $1" >&2
	exit 1
}

command -v readelf >/dev/null || fail "readelf is not on PATH, and it is what reads the linking"

platform=$(docker image inspect -f '{{.Os}}/{{.Architecture}}' "$image")
[ "$platform" = linux/amd64 ] || fail "$image is $platform, want linux/amd64"
passed=$((passed + 1))

revision=$(git rev-parse --verify HEAD)
label=$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
[ "$label" = "$revision" ] || fail "org.opencontainers.image.revision is '$label', want HEAD $revision"
passed=$((passed + 1))

container=$(docker create "$image")
for path in /garam /testprincipal /migrations; do
	docker cp "$container:$path" "$dir/" || fail "$image holds no $path"
done
passed=$((passed + 1))

# A dynamically linked executable names the loader it needs in an
# INTERP program header; a static one has none.
for bin in garam testprincipal; do
	[ -f "$dir/$bin" ] && [ -x "$dir/$bin" ] || fail "/$bin is not an executable file"
	header=$(readelf -hW "$dir/$bin") || fail "/$bin is not an ELF executable"
	printf '%s\n' "$header" | grep -Eq 'Machine:[[:space:]]+Advanced Micro Devices X86-64' \
		|| fail "/$bin is not built for amd64"
	if readelf -lW "$dir/$bin" | grep -q INTERP; then
		fail "/$bin is dynamically linked: $(readelf -lW "$dir/$bin" | grep -A1 INTERP | tail -1)"
	fi
done
passed=$((passed + 1))

# Each answers on this host, outside the image: testprincipal refuses
# an empty invocation with its usage and exit 2 (its README §Contract
# 1), and garam lists the serve command the consumer starts.
status=0
"$dir/testprincipal" >/dev/null 2>"$dir/usage" || status=$?
[ "$status" -eq 2 ] && grep -q 'testprincipal prepare' "$dir/usage" \
	|| fail "/testprincipal with no arguments exited $status, want 2 with its usage"
"$dir/garam" --help | grep -Eq '^[[:space:]]+serve[[:space:]]' \
	|| fail "/garam --help names no serve command"
passed=$((passed + 1))

diff -r migrations "$dir/migrations" >&2 \
	|| fail "/migrations/ differs from this checkout's migrations/"
passed=$((passed + 1))

echo "test-image-e2e: $passed checks passed against $image ($(find "$dir/migrations" -type f | wc -l | tr -d ' ') migration files, revision $revision)"
