#!/bin/sh
# chart-signing-key reads off the render how the token signing key
# reaches the api and machine containers when a Secret names it: as a
# file the pod's own account owns, readable by it alone, on a memory
# directory. The process refuses a key file it does not own or that any
# group or other bit is set on (ADR-0015), and a refusal of that kind
# arrives at the pod's start, one deploy after a render that looked
# right.
#
# The mode is what the copy installs; the owner is the account the copy
# runs as, which is the pod's runAsUser when the container sets none, as
# the kubelet resolves it. The copy's group is the pod's fsGroup.
#
# CHART_DIR, CHART_FULL_VALUES and HELM arrive in the environment;
# deploy/charts/Makefile holds them. The value file is the one that
# names the Secret for both workloads.

set -e
echo "chart-signing-key: api and machine, from $CHART_FULL_VALUES — the signing key's mode, owner and directory only, $CHART_DIR/README.md §What the checks are complete over"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
$HELM template garam "$CHART_DIR" --values "$CHART_FULL_VALUES" >"$out/raw"
awk -v d="$out" '/^---$/ { n++ } { print > (d "/doc" n) }' "$out/raw"

dir=/run/garam/policy-signing
checked=0
for comp in api machine; do
	doc=""
	for f in "$out"/doc*; do
		if grep -q '^kind: Deployment$' "$f" && grep -q "app.kubernetes.io/component: $comp\$" "$f"; then doc="$f"; fi
	done
	test -n "$doc" || { echo "chart-signing-key: the render holds no $comp Deployment — the parse is broken"; exit 1; }
	# The init container: one named for the key, installing it 0400
	# under the directory, with no runAsUser of its own.
	init="$(awk '/^        - name: policy-signing-key$/ { p = 1; print; next } p && /^        - name: / { p = 0 } p { print }' "$doc")"
	test -n "$init" || { echo "chart-signing-key: $comp renders no policy-signing-key init container"; exit 1; }
	echo "$init" | grep -q "install -m 0400 \"/etc/garam/policy-signing-secret/signing.pem\" \"$dir/signing.pem\"" \
		|| { echo "chart-signing-key: $comp's copy does not install the key 0400 under $dir"; exit 1; }
	! echo "$init" | grep -q 'runAsUser' \
		|| { echo "chart-signing-key: $comp's copy sets its own runAsUser, so the file is owned by an account the process is not"; exit 1; }
	# The pod's account owns the copy, and its group reads the source.
	uid="$(sed -n 's/^        runAsUser: \([0-9]*\)$/\1/p' "$doc")"
	gid="$(sed -n 's/^        fsGroup: \([0-9]*\)$/\1/p' "$doc")"
	test -n "$uid" && test "$uid" != 0 && test -n "$gid" \
		|| { echo "chart-signing-key: $comp's pod names no non-root runAsUser and fsGroup"; exit 1; }
	# The directory is memory, and the process reads exactly the file the
	# copy wrote, from a read-only mount.
	awk '/^        - name: policy-signing$/ { p = 1; next } p && /^        - name: / { p = 0 } p { print }' "$doc" | grep -q '^            medium: Memory$' \
		|| { echo "chart-signing-key: $comp's $dir is not a memory-backed volume"; exit 1; }
	grep -A1 'name: GARAM_POLICY_SIGNING_KEY_FILE$' "$doc" | grep -q "value: \"$dir/signing.pem\"" \
		|| { echo "chart-signing-key: $comp's process does not read $dir/signing.pem"; exit 1; }
	awk '/^          volumeMounts:$/ { p = 1; next } /^[^ ]|^      [a-z]/ { p = 0 } p { print }' "$doc" | grep -A2 'name: policy-signing$' | grep -q 'readOnly: true' \
		|| { echo "chart-signing-key: $comp's container does not mount $dir read-only"; exit 1; }
	echo "chart-signing-key: $comp — copied 0400 as uid $uid (fsGroup $gid) onto a memory volume, read from $dir/signing.pem"
	checked=$((checked + 1))
done
test "$checked" -eq 2 || { echo "chart-signing-key: $checked workloads read, expected 2"; exit 1; }
