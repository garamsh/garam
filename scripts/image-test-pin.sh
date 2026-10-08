#!/bin/sh
# Moves the tag `pinned-<consumer>` to the test image of one commit in
# its ECR repository (ADR-0093), for `make test-image-pin`, under the
# caller's AWS credentials. A commit is named by 12 to 40 lowercase
# hex digits and found under its first twelve, the tag
# scripts/image-test-push.sh pushes it as (ADR-0071's default). Its
# manifest is put again under the pinned tag with the digest it was
# read with, so the registry stores the same bytes rather than a copy
# that could name another digest, and the consumer's previous image
# under that tag loses it in the same call.
#
# Moving the tag depends on the repository exempting `pinned-*` from
# its tag immutability, an exclusion filter garamsh/infra declares
# (ADR-0093). Without it a consumer's first pin succeeds and every
# later one is refused by the registry.
#
#   scripts/image-test-pin.sh <commit> <consumer>
set -eu

[ $# -eq 2 ] || {
	echo 'usage: scripts/image-test-pin.sh <commit> <consumer>' >&2
	exit 2
}
commit=$1 consumer=$2

# The one repository the test image goes to (ADR-0093). It is held
# here rather than taken as an argument, so no invocation can point the
# image or a pin at another account, region or repository.
account=486152169996
region=ap-northeast-2
name=dev/garam-test
host=$account.dkr.ecr.$region.amazonaws.com
repository=$host/$name

refuse() {
	echo "image-test-pin: $1; nothing tagged" >&2
	exit 1
}

printf '%s' "$commit" | grep -Eq '^[0-9a-f]{12,40}$' \
	|| refuse "commit '$commit' is not 12 to 40 lowercase hex digits; the image is pushed under the commit's first twelve"
# A tag is at most 128 characters, and `pinned-` takes 7 of them.
printf '%s' "$consumer" | grep -Eq '^[a-z0-9]([a-z0-9-]{0,119}[a-z0-9])?$' \
	|| refuse "consumer '$consumer' is not lowercase letters, digits and inner hyphens of at most 121 characters"

source=$(printf '%s' "$commit" | cut -c1-12)
pinned=pinned-$consumer

# Answers one field of a tag's image, named by its path in the
# response, or `None` for an absent tag. An index is not an accepted
# type, so a tag naming one reads as absent.
image() {
	aws ecr batch-get-image --region "$region" --registry-id "$account" \
		--repository-name "$name" --image-ids "imageTag=$1" \
		--accepted-media-types application/vnd.oci.image.manifest.v1+json application/vnd.docker.distribution.manifest.v2+json \
		--query "images[0].$2" --output text
}

want=$(image "$source" imageId.imageDigest) \
	|| refuse "$repository could not be read with the caller's AWS credentials"
[ "$want" != None ] \
	|| refuse "$repository holds no image manifest tagged $source; push it first with make test-image-push from that commit"
held=$(image "$pinned" imageId.imageDigest) \
	|| refuse "$repository:$pinned could not be read"
if [ "$held" = "$want" ]; then
	echo "image-test-pin: $repository:$pinned is already $source, $want"
	exit 0
fi
media=$(image "$source" imageManifestMediaType) \
	|| refuse "$repository:$source's media type could not be read"
manifest=$(image "$source" imageManifest) \
	|| refuse "$repository:$source's manifest could not be read"
if ! aws ecr put-image --region "$region" --registry-id "$account" \
	--repository-name "$name" --image-tag "$pinned" \
	--image-manifest "$manifest" --image-manifest-media-type "$media" \
	--image-digest "$want" >/dev/null; then
	[ "$held" = None ] \
		|| refuse "the registry refused moving $pinned from $held to $source; the likely cause is tag immutability without the pinned-* exclusion filter"
	refuse "the registry refused tagging $source as $pinned"
fi
got=$(image "$pinned" imageId.imageDigest) \
	|| refuse "$repository:$pinned could not be read back"
[ "$got" = "$want" ] || {
	echo "image-test-pin: $repository:$pinned resolves to $got, not $source's $want" >&2
	exit 1
}
echo "image-test-pin: $repository:$pinned is $source, $want"
