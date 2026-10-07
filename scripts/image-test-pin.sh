#!/bin/sh
# Moves the tag `pinned-<consumer>` to the test image of one commit
# (ADR-0092), for .github/workflows/test-image.yml, its only caller.
# The manifest is copied as it is, so the pinned tag and the commit's
# tag resolve to one digest, and the consumer's previous version under
# that tag loses it in the same push. What the repository's lifecycle
# policy keeps under the tag is ADR-0093's.
#
#   scripts/image-test-pin.sh <repository> <commit> <consumer>
set -eu

[ $# -eq 3 ] || {
	echo 'usage: scripts/image-test-pin.sh <repository> <commit> <consumer>' >&2
	exit 2
}
repository=$1 commit=$2 consumer=$3

refuse() {
	echo "image-test-pin: $1; nothing tagged" >&2
	exit 1
}

printf '%s' "$commit" | grep -Eq '^[0-9a-f]{40}$' \
	|| refuse "commit '$commit' is not a full lowercase commit hash, which is the only tag the image is published under"
# A tag is at most 128 characters, and `pinned-` takes 7 of them.
printf '%s' "$consumer" | grep -Eq '^[a-z0-9]([a-z0-9-]{0,119}[a-z0-9])?$' \
	|| refuse "consumer '$consumer' is not lowercase letters, digits and inner hyphens of at most 121 characters"

source=$repository:$commit
pinned=$repository:pinned-$consumer

digest() {
	docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$1"
}

want=$(digest "$source") || refuse "$source is not in the registry"
docker buildx imagetools create --prefer-index=false --tag "$pinned" "$source"
got=$(digest "$pinned")
[ "$got" = "$want" ] || {
	echo "image-test-pin: $pinned resolves to $got, not $source's $want" >&2
	exit 1
}
echo "image-test-pin: $pinned is $source, $want"
