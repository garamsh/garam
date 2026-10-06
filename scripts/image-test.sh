#!/bin/sh
# Builds the test image (ADR-0092) from tests/testprincipal/Dockerfile
# as `<repository>:<commit>`, the commit being HEAD's full hash, which
# the revision label records as well. The repository defaults to
# `garam-test`; the publishing workflow names its registry's. Nothing
# is pushed.
#
# The tag and the label both name HEAD, so a working tree that differs
# from it is refused before anything is built: the image would claim a
# commit whose tree was not what it holds.
set -eu

repository=${1:-garam-test}

refuse() {
	echo "image-test: $1; nothing built" >&2
	exit 1
}

revision=$(git rev-parse --verify HEAD) \
	|| refuse "no commit to tag the image and its revision label with"
status=$(git status --porcelain) \
	|| refuse "git status failed, so whether the working tree matches HEAD ($revision) is unknown"
[ -z "$status" ] \
	|| refuse "the working tree differs from HEAD ($revision), which the tag and the revision label would both name"

image=$repository:$revision
echo "image-test: $image from tests/testprincipal/Dockerfile, context ."
# One linux/amd64 image manifest and no attestation beside it, which
# would make the reference an index.
docker build -t "$image" -f tests/testprincipal/Dockerfile \
	--platform linux/amd64 --provenance=false --sbom=false \
	--build-arg "IMAGE_REVISION=$revision" .
echo "image-test: built $image $(docker image inspect -f '{{.Id}}' "$image")"
