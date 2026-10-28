#!/bin/sh
# Builds the test image of HEAD, checks it as `make test-image-e2e`
# does, and pushes it to its ECR repository (ADR-0093), for
# `make test-image-push`, under the caller's AWS credentials. No
# workflow holds a credential for that repository, so this is the only
# way an image reaches it.
#
# The pushed tag is HEAD's first twelve hex digits, the commit-derived
# form ADR-0071 gives a development build at its default length. The
# build and the check name the full hash, as `make test-image-e2e`
# does, and the checked image is tagged again for the push.
#
# The tag is immutable in that repository, so a commit already there
# is reported and not built again: a second build would differ in its
# bytes and be refused at the push. The digest is read back from the
# registry, and must be the one the push reported.
#
#   tooling/image-test-push.sh
set -eu

[ $# -eq 0 ] || {
	echo 'usage: tooling/image-test-push.sh, which takes no argument' >&2
	exit 2
}

# The one repository the test image goes to (ADR-0093). It is held
# here rather than taken as an argument, so no invocation can point the
# image or a pin at another account, region or repository.
account=486152169996
region=ap-northeast-2
name=garam-dev/garam-test
host=$account.dkr.ecr.$region.amazonaws.com
repository=$host/$name

refuse() {
	echo "image-test-push: $1; nothing pushed" >&2
	exit 1
}

# tooling/image-test.sh refuses this too; it is read here as well so a
# dirty tree reaches neither AWS nor the build.
revision=$(git rev-parse --verify HEAD) \
	|| refuse "no commit to tag the image with"
status=$(git status --porcelain) \
	|| refuse "git status failed, so whether the working tree matches HEAD ($revision) is unknown"
[ -z "$status" ] \
	|| refuse "the working tree differs from HEAD ($revision), which the tag and the revision label would both name"

tag=$(printf '%s' "$revision" | cut -c1-12)
image=$repository:$tag

digest() {
	aws ecr batch-get-image --region "$region" --registry-id "$account" \
		--repository-name "$name" --image-ids "imageTag=$tag" \
		--accepted-media-types application/vnd.oci.image.manifest.v1+json application/vnd.docker.distribution.manifest.v2+json \
		--query 'images[0].imageId.imageDigest' --output text
}

held=$(digest) || refuse "$repository could not be read with the caller's AWS credentials"
if [ "$held" != None ]; then
	echo "image-test-push: $image is already in the repository, $held; nothing built"
	exit 0
fi

aws ecr get-login-password --region "$region" \
	| docker login --username AWS --password-stdin "$host" \
	|| refuse "docker could not log in to $host with the caller's AWS credentials"

tooling/image-test.sh "$repository"
platform/tests/image/test-image.sh "$repository:$revision"
docker tag "$repository:$revision" "$image"

docker push "$image" || refuse "the registry refused $image"
pushed=$(docker image inspect -f '{{range .RepoDigests}}{{println .}}{{end}}' "$image" \
	| sed -n "s|^$repository@||p")
got=$(digest) || {
	echo "image-test-push: $image was pushed as ${pushed:-an unreported digest} and could not be read back" >&2
	exit 1
}
[ -n "$pushed" ] && [ "$got" = "$pushed" ] || {
	echo "image-test-push: $image reads back as $got, but the push reported ${pushed:-no digest}" >&2
	exit 1
}
echo "image-test-push: pushed $image ($revision), $got"
