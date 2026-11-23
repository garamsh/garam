#!/bin/sh
# Packages the chart under the release's version and pushes it to the
# registry's chart repository, `<registry>/garam/charts/garam`, then
# records the digest the registry answers with. Its one caller is
# `make -C deploy/charts chart-publish`, which release.yml runs.
#
# The published chart takes the release's version, both as its own
# version and as its appVersion, so one version names the images and the
# chart (ADR-0039, ADR-0157) and an IMMUTABLE repository never receives
# the same chart version twice. Chart.yaml's own `version` is the
# source's and is not what is published.
#
# A repository that does not exist fails the run with a message saying
# so, which is what a re-run of the job after the repository is created
# repairs; nothing else about the release depends on this job.
#
# HELM names the helm command, CHART_DIR the chart, and the registry and
# the version are the two arguments. The digest is appended to
# $GITHUB_OUTPUT as `chart-digest` and to $GITHUB_STEP_SUMMARY, each
# when set.
set -eu

test $# -eq 2 \
	|| { echo 'chart-push: takes <registry> and <version>; nothing pushed' >&2; exit 2; }
registry=$1
version=$2
test -n "${HELM:-}" && test -n "${CHART_DIR:-}" \
	|| { echo 'chart-push: HELM and CHART_DIR are unset; nothing pushed' >&2; exit 2; }

out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

$HELM package "$CHART_DIR" --version "$version" --app-version "$version" --destination "$out" >/dev/null
set -- "$out"/*.tgz
test $# -eq 1 && test -f "$1" \
	|| { echo "chart-push: helm package wrote $# archives, expected 1; nothing pushed" >&2; exit 1; }

reference="$registry/garam/charts/garam"
if ! $HELM push "$1" "oci://$registry/garam/charts" >"$out/push" 2>&1; then
	cat "$out/push" >&2
	if grep -qiE 'does not exist|name unknown|not found' "$out/push"; then
		echo "::error::chart-push: the repository $reference was not found: it is created by the infrastructure repository, and this job is re-run once it exists"
	fi
	exit 1
fi
cat "$out/push"

digest="$(sed -n 's/^Digest: *//p' "$out/push")"
case $digest in
sha256:????????????????????????????????????????????????????????????????) ;;
*)
	echo "chart-push: the push answered no sha256 digest; read: '$digest'" >&2
	exit 1
	;;
esac
echo "chart-push: $reference:$version $digest"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
	echo "chart-digest=$digest" >>"$GITHUB_OUTPUT"
fi
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	printf '%s:%s\n%s\n' "$reference" "$version" "$digest" >>"$GITHUB_STEP_SUMMARY"
fi
