#!/bin/sh
# Pushes images a workflow has already built, named
# `<output>=<reference>` on the command line, and records what the
# registry answers with as `<output>-digest` on $GITHUB_OUTPUT
# (ADR-0039: the release records each image's digest beside its tag).
# It has two callers: .github/workflows/release.yml, which says why the
# push is separated from the build, and
# .github/workflows/test-image.yml, which pushes the test image after
# checking it (ADR-0092).
#
# Every reference is read out of the local image store before the first
# one is pushed, so a run that built one image of a pair pushes neither.
# The release repositories are IMMUTABLE and half a version cannot be
# re-pushed, so the repair for a partial release is the next version
# rather than this one (ADR-0039 §Consequences). The test image's GHCR
# package has mutable tags.
#
# The digest comes from the registry and not from the build. What a
# consumer resolves is the manifest the registry stores, and a build
# that only loaded locally reported the manifest it made rather than
# the one the push sent: the two agree on an image store that keeps the
# manifest and need not on one that rebuilds it, and this script fixes
# neither store. The same read holds each reference to a single image
# manifest, an index over per-platform manifests leaving the recorded
# digest naming a list rather than the image, and it runs once every
# push has, so a reference that reads back wrong fails the run instead
# of holding back the push after it.
set -eu

test $# -gt 0 \
	|| { echo 'image-push: no image named — a caller passes one <output>=<reference> per image it publishes, and nothing was pushed' >&2; exit 1; }

test -n "${GITHUB_OUTPUT:-}" \
	|| { echo 'image-push: GITHUB_OUTPUT is unset — the digests are recorded there, so nothing was pushed' >&2; exit 1; }

for pair in "$@"; do
	reference="${pair#*=}"
	test "$reference" != "$pair" && test -n "${pair%%=*}" && test -n "$reference" \
		|| { echo "image-push: $pair is not <output>=<reference>; nothing was pushed" >&2; exit 1; }
	docker image inspect "$reference" >/dev/null \
		|| { echo "image-push: $reference was not built into the local image store; nothing was pushed" >&2; exit 1; }
done

echo "image-push: $# images built, none pushed yet"

for pair in "$@"; do
	docker push "${pair#*=}"
done

for pair in "$@"; do
	reference="${pair#*=}"
	manifest="$(docker buildx imagetools inspect --format '{{.Manifest.MediaType}} {{.Manifest.Digest}}' "$reference")"
	case "${manifest%% *}" in
	application/vnd.oci.image.manifest.v1+json | application/vnd.docker.distribution.manifest.v2+json) ;;
	*)
		echo "image-push: $reference resolves to ${manifest%% *}, which names a manifest list and not the image" >&2
		exit 1
		;;
	esac
	echo "image-push: pushed $reference, $manifest"
	echo "${pair%%=*}-digest=${manifest##* }" >> "$GITHUB_OUTPUT"
done
