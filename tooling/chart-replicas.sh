#!/bin/sh
# chart-replicas reads off the render what ADR-0178 asks of every
# workload: two replicas by default, a value that moves the count, a
# PodDisruptionBudget of `maxUnavailable: 1`, a spread over Nodes
# (`maxSkew: 1`, `kubernetes.io/hostname`, `DoNotSchedule`) selecting
# the workload's own pods, a `RollingUpdate` with `maxUnavailable: 0`
# and `maxSurge: 1`, and a headless policy Service. Each is a line a
# render carries and a template edit can drop without any other check
# noticing: the pods still run, one replica at a time.
#
# CHART_DIR, CHART_DEFAULT_VALUES, CHART_FULL_VALUES and HELM arrive in
# the environment; deploy/charts/Makefile holds them. The full value
# file sets `web.replicas` to 3, so a count that is not the default
# reaches the render too.

set -e
workloads='api machine policy web site'
echo "chart-replicas: $(echo $workloads | wc -w) workloads in 2 value files — replicas, budget, spread, rollout and the policy Service only, $CHART_DIR/README.md §What the checks are complete over"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

checked=0
for values in "$CHART_DEFAULT_VALUES" "$CHART_FULL_VALUES"; do
	n="$(basename "$values")"
	rm -rf "$out/docs"
	mkdir "$out/docs"
	$HELM template garam "$CHART_DIR" --values "$values" >"$out/raw"
	awk -v d="$out/docs" '/^---$/ { i++ } { print > (d "/doc" i) }' "$out/raw"
	for w in $workloads; do
		want=2
		if [ "$n" = full-values.yaml ] && [ "$w" = web ]; then want=3; fi
		dep=""
		pdb=""
		for f in "$out"/docs/doc*; do
			grep -q "app.kubernetes.io/component: $w\$" "$f" || continue
			if grep -q '^kind: Deployment$' "$f"; then dep="$f"; fi
			if grep -q '^kind: PodDisruptionBudget$' "$f"; then pdb="$f"; fi
		done
		test -n "$dep" || { echo "chart-replicas: $n renders no $w Deployment — the parse is broken"; exit 1; }
		test -n "$pdb" || { echo "chart-replicas: $n renders no PodDisruptionBudget for $w"; exit 1; }
		grep -q "^  replicas: $want\$" "$dep" \
			|| { echo "chart-replicas: $n: $w does not run $want replicas"; exit 1; }
		# One block of the Deployment, compared as lines, so a field
		# moved under another key does not pass.
		grep -q '^    type: RollingUpdate$' "$dep" && grep -q '^      maxUnavailable: 0$' "$dep" && grep -q '^      maxSurge: 1$' "$dep" \
			|| { echo "chart-replicas: $n: $w is not RollingUpdate with maxUnavailable 0 and maxSurge 1"; exit 1; }
		! grep -q 'type: Recreate' "$dep" \
			|| { echo "chart-replicas: $n: $w still recreates"; exit 1; }
		spread="$(awk '/^      topologySpreadConstraints:$/ { p = 1; next } p && /^      [a-zA-Z]/ { p = 0 } p { print }' "$dep")"
		for line in '- maxSkew: 1' 'topologyKey: kubernetes.io/hostname' 'whenUnsatisfiable: DoNotSchedule' "app.kubernetes.io/component: $w"; do
			echo "$spread" | grep -qF -- "$line" \
				|| { echo "chart-replicas: $n: $w's spread lacks '$line'"; exit 1; }
		done
		grep -q '^  maxUnavailable: 1$' "$pdb" \
			|| { echo "chart-replicas: $n: $w's budget is not maxUnavailable 1"; exit 1; }
		grep -q "app.kubernetes.io/component: $w\$" "$pdb" \
			|| { echo "chart-replicas: $n: $w's budget selects no $w pod"; exit 1; }
		checked=$((checked + 1))
	done
	svc=""
	for f in "$out"/docs/doc*; do
		if grep -q '^kind: Service$' "$f" && grep -q "^  name: garam-policy\$" "$f"; then svc="$f"; fi
	done
	test -n "$svc" && grep -q '^  clusterIP: None$' "$svc" \
		|| { echo "chart-replicas: $n: the policy Service is not headless"; exit 1; }
	echo "chart-replicas: $n — 5 workloads at their counts, each with a budget, a spread and a rolling update; the policy Service headless"
done
test "$checked" -eq 10 || { echo "chart-replicas: $checked workloads read, expected 10"; exit 1; }
