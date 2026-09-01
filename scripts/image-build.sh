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
set -eu

workflow=.github/workflows/release.yml
tag=ci
names=false
[ "${1:-}" = --names ] && names=true

# One line per image the release builds:
#
#   <repository> <context> <dockerfile> <build-arg>...
#
# A step counts when it runs docker/build-push-action, whatever its id,
# so an image added to the release is built here without this file
# moving. The repository is `tags:`' last path segment without its
# version. `platforms:`, `provenance:` and `sbom:` are not read: they
# shape what a release publishes, not what a build proves.
#
# The steps seen are counted against the lines written, so a step whose
# shape this no longer reads fails here instead of leaving a run that
# built one image looking like the run that built both.
images=$(awk '
	function emit() {
		if (build && name != "") {
			printf "%s %s %s %s\n", name, context, file, args
			written++
		}
		build = 0; name = ""; context = ""; file = ""; args = ""
	}
	/^[[:space:]]*-[[:space:]]/ { emit() }
	$1 == "uses:" && $2 ~ /^docker\/build-push-action@/ { build = 1; seen++ }
	$1 == "context:" { context = $2 }
	$1 == "file:" { file = $2 }
	$1 ~ /^[A-Za-z_][A-Za-z0-9_]*=/ { args = args (args == "" ? "" : " ") $1 }
	$1 == "tags:" {
		name = $0
		sub(/^.*\//, "", name)
		sub(/:.*$/, "", name)
		if (name !~ /^[a-z0-9][a-z0-9._-]*$/) name = ""
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

if [ "$names" = true ]; then
	printf '%s\n' "$images" | awk -v tag="$tag" '{ print $2, $1 ":" tag }'
	exit 0
fi

echo "image-build: $(printf '%s\n' "$images" | wc -l | tr -d ' ') images, read from $workflow"

while read -r name context file args; do
	[ -n "$context" ] && [ -n "$file" ] || {
		echo "image-build: $workflow gives $name no context or no Dockerfile" >&2
		exit 1
	}
	echo "image-build: $name:$tag from $file, context $context${args:+, $args}"
	set --
	for arg in $args; do set -- "$@" --build-arg "$arg"; done
	docker build -t "$name:$tag" -f "$file" "$@" "$context"
	echo "image-build: built $name:$tag $(docker image inspect -f '{{.Id}}' "$name:$tag")"
done <<EOF
$images
EOF
