#!/bin/sh
# chart-security reads the PodSecurity `restricted` fields off the
# render, on every workload and every container. Nothing read them
# before: #763 put them on all five workloads and checked the render by
# hand, and deleting one include line afterwards still left `make ci`
# green — the refusal arrived from an apiserver, one deploy later (#766).
#
# The effective runAsUser is the container's own or, where it sets none,
# the pod's, which is the rule the kubelet applies. Only the two
# containers in machine.yaml set one, so a check reading the container
# level alone reports every other container as missing.
#
# The workloads are the ones §Workload identities declares, the list
# chart-identity already counts by, so a sixth is checked the day it is
# declared. The containers are counted against the render's own `image:`
# lines, so a parse that stopped seeing them fails instead of passing an
# empty set.
#
# CHART_DIR, CHART_IDENTITIES, CHART_VALUES and HELM arrive in the
# environment; the Makefile holds them.

set -- $CHART_IDENTITIES; workloads=$#
set -- $CHART_VALUES; values=$#

echo "chart-security: $workloads workloads declared by $CHART_DIR/README.md §Workload identities, $values value files — the PodSecurity restricted fields only, $CHART_DIR/README.md §What the checks are complete over"
test -n "$CHART_IDENTITIES" \
	|| { echo "chart-security: $CHART_DIR/README.md §Workload identities declared no workload — the parse is broken"; exit 1; }
set -e
out="$(mktemp -d)"; trap 'rm -rf "$out"' EXIT
printf '%s\n' $CHART_IDENTITIES | cut -d= -f1 | sort -u > "$out/declared"
for v in $CHART_VALUES; do
	n="$(basename "$v")"
	$HELM template garam "$CHART_DIR" --values "$v" > "$out/raw"
	awk '
		function flush(   i, u, from) {
			if (kind != "Deployment" && kind != "Job") { reset(); return }
			printf "pod %s runAsNonRoot=%s seccompProfile=%s\n", comp, nonroot, seccomp;
			for (i = 1; i <= n; i++) {
				from = (cuid[i] != "unset" ? "container" : (puid != "unset" ? "pod" : "none"));
				u = (cuid[i] != "unset" ? cuid[i] : puid);
				printf "container %s %s effectiveRunAsUser=%s allowPrivilegeEscalation=%s capabilities.drop=%s from=%s\n",
					comp, cname[i], u, cape[i], cdrop[i], from }
			reset() }
		function reset() { kind = ""; comp = ""; nonroot = "unset"; seccomp = "unset"; puid = "unset"; n = 0; sec = ""; list = "" }
		BEGIN { reset() }
		/^---$/ { flush(); next }
		/^kind: / { kind = $2; next }
		/app.kubernetes.io\/component: / { if (comp == "") comp = $2; next }
		/^      [a-zA-Z]/ {
			list = ($0 ~ /^      (initContainers|containers):/) ? "yes" : "";
			sec = ($0 ~ /^      securityContext:/) ? "pod" : ""; next }
		sec == "pod" && /^        [a-zA-Z]/ {
			psub = ($0 ~ /^        seccompProfile:/) ? "seccomp" : "";
			if ($0 ~ /^        runAsNonRoot: /) nonroot = $2;
			if ($0 ~ /^        runAsUser: /) puid = $2; next }
		sec == "pod" && psub == "seccomp" && /^          type: / { seccomp = $2; next }
		list == "yes" && /^        - name: / {
			n++; cname[n] = $3; cuid[n] = "unset"; cape[n] = "unset"; cdrop[n] = "unset"; sec = ""; next }
		list == "yes" && /^          [a-zA-Z]/ { sec = ($0 ~ /^          securityContext:/) ? "con" : ""; next }
		sec == "con" && /^            [a-zA-Z]/ {
			csub = ($0 ~ /^            capabilities:/) ? "caps" : "";
			if ($0 ~ /^            runAsUser: /) cuid[n] = $2;
			if ($0 ~ /^            allowPrivilegeEscalation: /) cape[n] = $2; next }
		sec == "con" && csub == "caps" && /^              drop: / {
			d = $0; sub(/^ *drop: */, "", d); gsub(/[][", ]+/, ",", d); sub(/^,/, "", d); sub(/,$/, "", d);
			cdrop[n] = d; next }
		END { flush() }' "$out/raw" > "$out/fields"
	sed -n 's/^pod \([a-z0-9-]*\) .*/\1/p' "$out/fields" | sort -u > "$out/read"
	diff "$out/declared" "$out/read" \
		|| { echo "chart-security: $n: < workload declared in the README, > carrying a pod spec this check read"; exit 1; }
	containers="$(grep -c '^container ' "$out/fields" || true)"
	images="$(grep -c '^          image: ' "$out/raw" || true)"
	test "$containers" -eq "$images" && test "$images" -ge "$workloads" \
		|| { echo "chart-security: $n: $containers containers read and $images rendered — the container parse is broken"; exit 1; }
	short="$(awk '
		$1 == "pod" && ($3 != "runAsNonRoot=true" || $4 !~ /^seccompProfile=(RuntimeDefault|Localhost)$/) { print "  " $0 }
		$1 == "container" {
			d = substr($6, index($6, "=") + 1);
			if ($4 !~ /^effectiveRunAsUser=[0-9]+$/ || $5 != "allowPrivilegeEscalation=false" || ("," d ",") !~ /,ALL,/) print "  " $0
		}' "$out/fields")"
	test -z "$short" || { echo "chart-security: $n: short of PodSecurity restricted, which an admitting apiserver refuses and a render does not:"; echo "$short"; exit 1; }
	echo "chart-security: $n — $(grep -c '^pod ' "$out/fields" || true) workloads, $containers containers, $(grep -c ' from=pod$' "$out/fields" || true) taking the pod's runAsUser"
done
