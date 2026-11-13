#!/bin/sh
# Prints the image version of a release tag, and refuses any tag that
# is not a release (ADR-0157). Its one caller is
# .github/workflows/release.yml, which runs it before anything is built.
#
# Until launch a release is `v0.1.0-dev.N`, a rehearsal candidate
# `v0.1.0-rc.N` and launch `v0.1.0`, N a positive integer without a
# leading zero. The image version is the tag without its `v`, so
# `v0.1.0-dev.16` publishes `0.1.0-dev.16`. The repositories are
# IMMUTABLE, so a tag outside these forms is refused here rather than
# published under a name nothing can take back.
set -eu

test $# -eq 1 \
	|| { echo 'release-version: takes one argument, the git tag name; nothing published' >&2; exit 2; }

tag=$1

refuse() {
	echo "release-version: tag $tag is not v0.1.0-dev.N, v0.1.0-rc.N or v0.1.0: nothing published (ADR-0157)" >&2
	exit 1
}

# grep reads a value line by line, so a tag holding a newline would
# match on one of its lines.
case $tag in
*'
'*) refuse ;;
esac

printf '%s' "$tag" | grep -Eq '^v0\.1\.0(-(dev|rc)\.[1-9][0-9]*)?$' || refuse

echo "${tag#v}"
