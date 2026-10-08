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
#   scripts/image-test-push.sh <repository-uri>
set -eu

[ $# -eq 1 ] || {
	echo 'usage: scripts/image-test-push.sh <repository-uri>' >&2
	exit 2
}
repository=$1

refuse() {
	echo "image-test-push: $1; nothing pushed" >&2
	exit 1
}

printf '%s' "$repository" | grep -Eq '^[0-9]{12}\.dkr\.ecr\.[a-z0-9-]+\.amazonaws\.com/[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$' \
	|| refuse "repository '$repository' is not an ECR repository URI, <account>.dkr.ecr.<region>.amazonaws.com/<name>"

# scripts/image-test.sh refuses this too; it is read here as well so a
# dirty tree reaches neither AWS nor the build.
revision=$(git rev-parse --verify HEAD) \
	|| refuse "no commit to tag the image with"
status=$(git status --porcelain) \
	|| refuse "git status failed, so whether the working tree matches HEAD ($revision) is unknown"
[ -z "$status" ] \
	|| refuse "the working tree differs from HEAD ($revision), which the tag and the revision label would both name"

host=${repository%%/*}
name=${repository#*/}
account=${host%%.*}
region=${host#*.dkr.ecr.}
region=${region%.amazonaws.com}
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

scripts/image-test.sh "$repository"
tests/image/test-image.sh "$repository:$revision"
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
