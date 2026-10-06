#!/bin/sh
# Checks what .github/workflows/test-image.yml prunes (ADR-0092): every
# cleanup step is dataaxiom/ghcr-cleanup-action at a full commit SHA,
# keeps the 8 newest tagged versions and excludes `pinned-*`; the one
# that deletes runs in a job needing the publish job, whose steps end
# in the push, with no `if:` that could run it otherwise; and every
# other cleanup step is a dry run.
#
# The check runs on the workflow and then on copies of it, each with
# one of those properties removed, every one of which it must refuse:
# a check refusing nothing fails here as surely as one refusing
# everything.
set -eu

workflow=.github/workflows/test-image.yml
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

# One line per job and per step, read off the indentation the file is
# written in: jobs at 2, their keys at 4, steps at 6, step keys at 8
# and `with:` keys at 10. A step's fields are tab-separated, since an
# `if:` holds spaces.
#
#   job <name> needs=<needs> if=<if>
#   step <job> last|- <uses> push|run|- if=<if> keep=<n> exclude=<tags> dry=<b>
#
# <last> is `last` on a job's final step, which is how the push is
# known to be what the publish job ends in.
parse() {
	awk '
		function val(line) {
			sub(/^[^:]*:[[:space:]]*/, "", line)
			sub(/[[:space:]]+#.*/, "", line)
			return line == "" ? "-" : line
		}
		function step_end() {
			if (instep) {
				nsteps++
				steps[nsteps] = sprintf("%s\t%s\tif=%s\tkeep=%s\texclude=%s\tdry=%s", \
					uses, run, sif, keep, exclude, dry)
			}
			instep = 0; uses = "-"; run = "-"; sif = "-"; keep = "-"; exclude = "-"; dry = "-"
		}
		function job_end() {
			step_end()
			for (i = 1; i <= nsteps; i++)
				printf "step\t%s\t%s\t%s\n", job, (i == nsteps ? "last" : "-"), steps[i]
			if (job != "") printf "job %s needs=%s if=%s\n", job, needs, jif
			nsteps = 0; needs = "-"; jif = "-"
		}
		/^jobs:/ { injobs = 1; next }
		!injobs { next }
		/^  [A-Za-z0-9_-]+:[[:space:]]*$/ { job_end(); job = $1; sub(/:$/, "", job); next }
		/^    needs:/ { needs = val($0); next }
		/^    if:/ { jif = val($0); next }
		/^      - / { step_end(); instep = 1; sub(/^      - /, "        ") }
		instep && /^        uses:/ { uses = val($0) }
		instep && /^        run:/ { run = ($0 ~ /image-push\.sh/) ? "push" : "run" }
		instep && /^        if:/ { sif = val($0) }
		instep && /^          keep-n-tagged:/ { keep = val($0) }
		instep && /^          exclude-tags:/ { exclude = val($0) }
		instep && /^          dry-run:/ { dry = val($0) }
		END { job_end() }
	' "$1"
}

# check <file>: prints one line per property the file breaks.
check() {
	parse "$1" >"$scratch/parsed"
	cleanup='dataaxiom/ghcr-cleanup-action@'
	tab=$(printf '\t')
	steps=$(grep -c "^step$tab.*$tab$cleanup" "$scratch/parsed" || true)
	[ "$steps" -gt 0 ] || echo "no step uses $cleanup"
	grep "^step$tab.*$tab$cleanup" "$scratch/parsed" | while IFS=$tab read -r _ job _ uses _ sif keep exclude dry; do
		printf '%s' "${uses#"$cleanup"}" | grep -Eq '^[0-9a-f]{40}$' \
			|| echo "$job: $uses is not pinned to a full commit SHA"
		[ "$keep" = keep=8 ] || echo "$job: keep-n-tagged is ${keep#keep=}, want 8"
		printf ',%s,' "${exclude#exclude=}" | grep -Fq ',pinned-*,' \
			|| echo "$job: exclude-tags is ${exclude#exclude=}, which does not exclude pinned-*"
		if [ "$dry" != dry=true ]; then
			[ "$sif" = if=- ] || echo "$job: the deleting cleanup step has an if: (${sif#if=})"
			grep -q "^job $job needs=publish if=-\$" "$scratch/parsed" \
				|| echo "$job: the deleting cleanup runs in a job that does not need publish alone, or has an if:"
			echo "$job" >>"$scratch/deleters"
		fi
	done
	[ -s "$scratch/deleters" ] || echo "no cleanup step deletes"
	[ ! -s "$scratch/deleters" ] || [ "$(wc -l <"$scratch/deleters")" -eq 1 ] \
		|| echo "more than one cleanup step deletes"
	rm -f "$scratch/deleters"
	grep -q "^step${tab}publish${tab}last$tab-${tab}push$tab" "$scratch/parsed" \
		|| echo "publish does not end in scripts/image-push.sh"
}

cases=0
failed=0

cases=$((cases + 1))
out=$(check "$workflow")
if [ -n "$out" ]; then
	echo "image-test-workflow-test: $workflow:" >&2
	printf '%s\n' "$out" | sed 's/^/  | /' >&2
	failed=$((failed + 1))
fi

# refused <expected> <sed expression>: the workflow with that edit is
# refused, naming the expected property.
refused() {
	cases=$((cases + 1))
	sed "$2" "$workflow" >"$scratch/mutant.yml"
	if cmp -s "$workflow" "$scratch/mutant.yml"; then
		echo "image-test-workflow-test: '$2' changed nothing in $workflow" >&2
		failed=$((failed + 1))
		return
	fi
	out=$(check "$scratch/mutant.yml")
	printf '%s\n' "$out" | grep -Fq -- "$1" || {
		echo "image-test-workflow-test: with '$2', want a refusal naming '$1', got:" >&2
		printf '%s\n' "${out:-nothing}" | sed 's/^/  | /' >&2
		failed=$((failed + 1))
	}
}

refused 'does not exclude pinned-*' '/exclude-tags:/d'
refused 'does not exclude pinned-*' 's/exclude-tags: pinned-\*/exclude-tags: pinned/'
refused 'keep-n-tagged is 9, want 8' 's/keep-n-tagged: 8/keep-n-tagged: 9/'
refused 'is not pinned to a full commit SHA' 's/ghcr-cleanup-action@[0-9a-f]*/ghcr-cleanup-action@v1.2.2/'
refused 'does not need publish alone' '/^    needs: publish/d'
refused 'does not need publish alone' 's/^    needs: publish/    needs: publish\n    if: always()/'
refused 'has an if:' 's/^      - uses: dataaxiom/      - if: always()\n        uses: dataaxiom/'
refused 'publish does not end in scripts/image-push.sh' '/scripts\/image-push.sh/d'
refused 'more than one cleanup step deletes' '/dry-run: true/d'

[ "$failed" -eq 0 ] || {
	echo "image-test-workflow-test: $failed of $cases cases failed" >&2
	exit 1
}
echo "image-test-workflow-test: $cases cases passed, read against $workflow"
