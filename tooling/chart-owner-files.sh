#!/bin/sh
# chart-owner-files reads off the render how a Secret's files reach the
# process that reads them as files it owns: the machine listener's key
# material, the policy service's pair, and the token signing key of the
# api and the machine listener. The process refuses a key file it does
# not own or that any group or other bit is set on (ADR-0015), and a
# refusal of that kind arrives at the pod's start, one deploy after a
# render that looked right.
#
# For each copy it holds: an init container of the copy's name that
# installs the files with the mode the copy is meant to have and sets no
# `runAsUser` of its own, so the file is owned by the pod's account; a
# pod that names a non-root `runAsUser` and an `fsGroup`, which is what
# lets the copy read the 0440 projection; a memory-backed volume of that
# name; and the container that reads it mounting it read-only. The mode
# is what the copy's command installs and the owner is the account the
# copy runs as, which is the pod's runAsUser when the container sets
# none, as the kubelet resolves it. For the signing key it also holds
# that the process reads the path the copy wrote.
#
# CHART_DIR, CHART_FULL_VALUES and HELM arrive in the environment;
# deploy/charts/Makefile holds them. The value file is the one that
# names the signing key's Secret for both workloads.

set -e
echo "chart-owner-files: key material, policy pair and signing key, from $CHART_FULL_VALUES — mode, owner and directory only, $CHART_DIR/README.md §What the checks are complete over"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
$HELM template garam "$CHART_DIR" --values "$CHART_FULL_VALUES" >"$out/raw"
awk -v d="$out" '/^---$/ { n++ } { print > (d "/doc" n) }' "$out/raw"

checked=0

# copy <workload> <name> <mode> <dir> [<file>]: the copy named <name> in
# the <workload> pod, installed <mode> under <dir>; with <file> the
# process also reads <dir>/<file> from GARAM_POLICY_SIGNING_KEY_FILE.
copy() {
	comp="$1"
	name="$2"
	mode="$3"
	dir="$4"
	file="${5:-}"
	doc=""
	for f in "$out"/doc*; do
		if grep -q '^kind: Deployment$' "$f" && grep -q "app.kubernetes.io/component: $comp\$" "$f"; then doc="$f"; fi
	done
	test -n "$doc" || { echo "chart-owner-files: the render holds no $comp Deployment — the parse is broken"; exit 1; }
	init="$(awk -v n="$name" '$0 == "        - name: " n { p = 1; print; next } p && /^        - name: / { p = 0 } p { print }' "$doc")"
	test -n "$init" || { echo "chart-owner-files: $comp renders no $name init container"; exit 1; }
	echo "$init" | grep -q "install -m $mode \"[^\"]*/\$f\" \"$dir/\$f\"" \
		|| { echo "chart-owner-files: $comp's $name copy does not install its files $mode under $dir"; exit 1; }
	! echo "$init" | grep -q 'runAsUser' \
		|| { echo "chart-owner-files: $comp's $name copy sets its own runAsUser, so the file is owned by an account the process is not"; exit 1; }
	uid="$(sed -n 's/^        runAsUser: \([0-9]*\)$/\1/p' "$doc")"
	gid="$(sed -n 's/^        fsGroup: \([0-9]*\)$/\1/p' "$doc")"
	test -n "$uid" && test "$uid" != 0 && test -n "$gid" \
		|| { echo "chart-owner-files: $comp's pod names no non-root runAsUser and fsGroup"; exit 1; }
	awk -v n="$name" '$0 == "        - name: " n { p = 1; next } p && /^        - name: / { p = 0 } p { print }' "$doc" | grep -q '^            medium: Memory$' \
		|| { echo "chart-owner-files: $comp's $name volume is not memory-backed"; exit 1; }
	awk '/^          volumeMounts:$/ { p = 1; next } /^[^ ]|^      [a-z]/ { p = 0 } p { print }' "$doc" | grep -A2 "name: $name\$" | grep -q 'readOnly: true' \
		|| { echo "chart-owner-files: $comp's container does not mount $dir read-only"; exit 1; }
	if [ -n "$file" ]; then
		grep -A1 'name: GARAM_POLICY_SIGNING_KEY_FILE$' "$doc" | grep -q "value: \"$dir/$file\"" \
			|| { echo "chart-owner-files: $comp's process does not read $dir/$file"; exit 1; }
	fi
	echo "chart-owner-files: $comp $name — installed $mode as uid $uid (fsGroup $gid) onto a memory volume, mounted read-only at $dir"
	checked=$((checked + 1))
}

copy machine key-material 0600 /run/garam/key-material
copy policy tls 0600 /run/garam/policy-tls
copy api policy-signing 0400 /run/garam/policy-signing signing.pem
copy machine policy-signing 0400 /run/garam/policy-signing signing.pem
test "$checked" -eq 4 || { echo "chart-owner-files: $checked copies read, expected 4"; exit 1; }
