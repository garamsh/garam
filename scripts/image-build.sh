#!/bin/sh
# Builds the images a release publishes (ADR-0039), each from what
# .github/workflows/release.yml says it is built from. That workflow
# names every context, Dockerfile and baked argument once, and a copy
# here would be the copy that goes stale — a check that stops building
# what the release builds is the divergence it exists to catch.
#
# Nothing is pushed. Each image takes `<repository>:ci` locally, and
# `--names` prints `<context> <repository>:<tag>` for each and builds
# nothing: a caller needing one image's name reads it from this parse
# instead of keeping a second copy that goes stale (#870).
#
# `--registry <host>` builds a development image instead: each is
# tagged `<host>/garam-dev/<repository>:<tag>`, the development
# namespace beside the release's `garam/` (ADR-0097), and `--tag` names
# a commit-derived or prerelease tag, defaulting to the first twelve
# hex digits of HEAD (ADR-0071). A bare release version is refused, as
# is any other tag, before anything is built. The resulting references
# are what a person pushes; this script pushes nothing.
set -eu

workflow=.github/workflows/release.yml
tag=ci
names=false
registry=
devnamespace=garam-dev
devtag=
dev=false

usage() {
	echo "image-build: $1" >&2
	echo 'usage: scripts/image-build.sh [--names] [--registry <host> [--tag <tag>]]' >&2
	exit 2
}

while [ $# -gt 0 ]; do
	case $1 in
	--names) names=true ;;
	--registry)
		[ $# -gt 1 ] || usage '--registry takes a host'
		registry=$2 dev=true
		shift
		;;
	--tag)
		[ $# -gt 1 ] || usage '--tag takes a tag'
		devtag=$2 dev=true
		shift
		;;
	*) usage "unknown argument $1" ;;
	esac
	shift
done

refuse() {
	echo "image-build: $1; nothing built" >&2
	exit 1
}

# A development tag is one of two forms, and every check on it runs
# here, before the workflow is parsed or anything is built:
#
#   - commit-derived: 7 to 40 lowercase hex digits, a prefix of HEAD,
#     so the tag and the IMAGE_REVISION label name one commit;
#   - prerelease: `X.Y.Z-<identifiers>` as semver 2.0.0 writes one,
#     without build metadata, which a registry tag cannot carry.
#
# A bare `X.Y.Z` is the release's (ADR-0039) and in an IMMUTABLE
# repository a development push of one would take that version from the
# release for good, so it is refused by name.
if [ "$dev" = true ]; then
	[ -n "$registry" ] \
		|| refuse "--tag names a development tag, and --registry names the registry it is for; none was given"
	printf '%s' "$registry" | grep -Eq '^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]+)?$' \
		|| refuse "registry $registry is not a host[:port]; the path under it is $devnamespace/<repository>"
	revision=$(git rev-parse --verify HEAD) \
		|| refuse "no commit to derive a development tag or its revision from"
	[ -n "$devtag" ] || devtag=$(printf '%s' "$revision" | cut -c1-12)
	num='(0|[1-9][0-9]*)'
	ident='([0-9]*[A-Za-z-][0-9A-Za-z-]*|0|[1-9][0-9]*)'
	if printf '%s' "$devtag" | grep -Eq "^v?$num\\.$num\\.$num\$"; then
		refuse "$devtag is a release version, which only a vX.Y.Z tag through $workflow publishes (ADR-0039)"
	elif printf '%s' "$devtag" | grep -Eq '^[0-9a-f]{7,40}$'; then
		case $revision in
		"$devtag"*) ;;
		*) refuse "$devtag is not a prefix of HEAD ($revision), so it would name a commit this build is not" ;;
		esac
	elif ! printf '%s' "$devtag" | grep -Eq "^$num\\.$num\\.$num-$ident(\\.$ident)*\$" \
		|| [ "${#devtag}" -gt 128 ]; then
		refuse "$devtag is neither a commit prefix of 7 to 40 hex digits nor an X.Y.Z-<prerelease> tag"
	fi
	tag=$devtag
fi

# One line per image the release builds:
#
#   <repository> <context> <dockerfile> <per-run> <build-arg>...
#
# Every docker/build-push-action step counts, whatever its id, and its
# repository is the last path segment of its `tags:` (#857). A build
# argument holding `${{` is one only a run resolves, so it is listed in
# <per-run> (`-` for none) and passed nothing outside a development
# build (#1020). Steps seen are counted against lines written, so a step
# this no longer parses fails rather than reading as both images built.
images=$(awk '
	function emit() {
		if (build && name != "") {
			printf "%s %s %s %s %s\n", name, context, file, \
				(perrun == "" ? "-" : perrun), args
			written++
		}
		build = 0; name = ""; path = ""; context = ""; file = ""
		args = ""; perrun = ""
	}
	/^[[:space:]]*-[[:space:]]/ { emit() }
	$1 == "uses:" && $2 ~ /^docker\/build-push-action@/ { build = 1; seen++ }
	$1 == "context:" { context = $2 }
	$1 == "file:" { file = $2 }
	$1 ~ /^[A-Za-z_][A-Za-z0-9_]*=/ {
		if ($0 ~ /\$\{\{/) {
			argname = $1
			sub(/=.*/, "", argname)
			perrun = perrun (perrun == "" ? "" : ",") argname
		} else {
			args = args (args == "" ? "" : " ") $1
		}
	}
	$1 == "tags:" {
		path = $0
		sub(/^[[:space:]]*tags:[[:space:]]*/, "", path)
		sub(/^.*\}\}\//, "", path)
		sub(/:.*$/, "", path)
		if (path !~ /^[a-z0-9][a-z0-9._-]*(\/[a-z0-9][a-z0-9._-]*)*$/) path = ""
		name = path
		sub(/^.*\//, "", name)
	}
	END {
		emit()
		if (seen != written) {
			printf "image-build: %d image steps in %s, %d of them readable\n", \
				seen, FILENAME, written > "/dev/stderr"
			exit 1
		}
	}
' "$workflow")

[ -n "$images" ] || {
	echo "image-build: $workflow builds no image" >&2
	exit 1
}

# The reference an image takes: `<repository>:ci` for the check, and
# that repository in the development namespace under the given registry
# for a development build. The release's path is never one, so no
# development push lands where a release lives.
ref() {
	if [ "$dev" = true ]; then
		echo "$registry/$devnamespace/$1:$tag"
	else
		echo "$1:$tag"
	fi
}

# A development build passes a value for every per-run argument, and
# it knows two. Any other is refused here rather than after the first
# image has been built.
if [ "$dev" = true ]; then
	for arg in $(printf '%s\n' "$images" | awk '$4 != "-" { print $4 }' | tr ',' ' '); do
		case $arg in
		IMAGE_REVISION | IMAGE_VERSION) ;;
		*) refuse "$workflow resolves $arg per run and a development build has no value for it" ;;
		esac
	done
fi

if [ "$names" = true ]; then
	while read -r name context _; do
		echo "$context $(ref "$name")"
	done <<EOF
$images
EOF
	exit 0
fi

# The revision label is read off HEAD, so a tree that differs from HEAD
# would be labelled and tagged as a commit it is not. The check covers
# untracked files as well, since the build context reads them. A
# status that fails says nothing about the tree, so it refuses too
# rather than reading as an empty, clean answer.
if [ "$dev" = true ]; then
	status=$(git status --porcelain) \
		|| refuse "git status failed, so whether the working tree matches HEAD ($revision) is unknown"
	[ -z "$status" ] \
		|| refuse "the working tree differs from HEAD ($revision), which the tag and the revision label would both name"
fi

echo "image-build: $(printf '%s\n' "$images" | wc -l | tr -d ' ') images, read from $workflow"

while read -r name context file perrun args; do
	[ -n "$context" ] && [ -n "$file" ] || {
		echo "image-build: $workflow gives $name no context or no Dockerfile" >&2
		exit 1
	}
	image=$(ref "$name")
	set --
	for arg in $args; do set -- "$@" --build-arg "$arg"; done
	for arg in $(printf '%s' "$perrun" | tr ',' ' '); do
		[ "$arg" != - ] || continue
		if [ "$dev" = false ]; then
			echo "image-build: $arg is resolved per run in $workflow; nothing here passes it a value" >&2
			continue
		fi
		case $arg in
		IMAGE_REVISION) set -- "$@" --build-arg "IMAGE_REVISION=$revision" ;;
		IMAGE_VERSION) set -- "$@" --build-arg "IMAGE_VERSION=$tag" ;;
		esac
	done
	echo "image-build: $image from $file, context $context${args:+, $args}"
	docker build -t "$image" -f "$file" "$@" "$context"
	echo "image-build: built $image $(docker image inspect -f '{{.Id}}' "$image")"
done <<EOF
$images
EOF

[ "$dev" = false ] || echo "image-build: nothing pushed; each reference above is a development build of $revision"
