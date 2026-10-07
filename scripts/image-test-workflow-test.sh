#!/bin/sh
# Checks what .github/workflows/test-image.yml prunes (ADR-0092):
#
# - every cleanup step is dataaxiom/ghcr-cleanup-action at a full
#   commit SHA, on the package garam-test, keeping the 8 newest tagged
#   versions, excluding `pinned-*` and deleting untagged versions, and
#   takes no input beyond those and `dry-run`;
# - exactly one of them deletes, in a job needing the publish job,
#   whose `if:` is the repository guard alone and which nothing lets
#   run past a failure; every other one is a dry run;
# - the publish job ends in the push, which no `if:` or
#   `continue-on-error` can skip past;
# - every job names this repository in its `if:`, and the dispatch job
#   runs only from `dev`.
#
# The check runs on the workflow and then on copies of it, each with
# one of those properties removed, every one of which it must refuse:
# a check refusing nothing fails here as surely as one refusing
# everything.
set -eu

workflow=.github/workflows/test-image.yml
guard="github.repository == 'garamsh/garam'"
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM
tab=$(printf '\t')

# One tab-separated line per job and per step, read off the
# indentation the file is written in: jobs at 2, their keys at 4, steps
# at 6, step keys at 8 and `with:` keys at 10.
#
#   job <name> needs=<needs> if=<if> coe=<continue-on-error>
#   step <job> last|- <uses> push|run|- if=<if> coe=<coe> with=<k=v,...>
#
# <last> marks a job's final step, which is how the push is known to
# be what the publish job ends in. A key the file does not set reads
# `-`.
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
				steps[nsteps] = sprintf("%s\t%s\tif=%s\tcoe=%s\twith=%s", \
					uses, run, sif, scoe, (with == "" ? "-" : with))
			}
			instep = 0; uses = "-"; run = "-"; sif = "-"; scoe = "-"; with = ""
		}
		function job_end() {
			step_end()
			for (i = 1; i <= nsteps; i++)
				printf "step\t%s\t%s\t%s\n", job, (i == nsteps ? "last" : "-"), steps[i]
			if (job != "") printf "job\t%s\tneeds=%s\tif=%s\tcoe=%s\n", job, needs, jif, jcoe
			nsteps = 0; needs = "-"; jif = "-"; jcoe = "-"
		}
		/^jobs:/ { injobs = 1; next }
		!injobs { next }
		/^  [A-Za-z0-9_-]+:[[:space:]]*$/ { job_end(); job = $1; sub(/:$/, "", job); next }
		/^    needs:/ { needs = val($0); next }
		/^    if:/ { jif = val($0); next }
		/^    continue-on-error:/ { jcoe = val($0); next }
		/^      - / { step_end(); instep = 1; sub(/^      - /, "        ") }
		instep && /^        uses:/ { uses = val($0) }
		instep && /^        run:/ { run = ($0 ~ /image-push\.sh/) ? "push" : "run" }
		instep && /^        if:/ { sif = val($0) }
		instep && /^        continue-on-error:/ { scoe = val($0) }
		instep && /^          [A-Za-z0-9_-]+:/ {
			key = $1; sub(/:$/, "", key)
			with = with (with == "" ? "" : ",") key "=" val($0)
		}
		END { job_end() }
	' "$1"
}

# with_value <with=...> <key>: the value the step gives the key, or -.
with_value() {
	printf ',%s,' "${1#with=}" | sed -n "s/.*,$2=\\([^,]*\\),.*/\\1/p" | grep . || echo -
}

# check <file>: prints one line per property the file breaks.
check() {
	parse "$1" >"$scratch/parsed"
	: >"$scratch/deleters"
	cleanup='dataaxiom/ghcr-cleanup-action@'
	grep -q "^step$tab.*$tab$cleanup" "$scratch/parsed" || echo "no step uses $cleanup"
	grep "^step$tab.*$tab$cleanup" "$scratch/parsed" | while IFS=$tab read -r _ job _ uses _ sif scoe with; do
		printf '%s' "${uses#"$cleanup"}" | grep -Eq '^[0-9a-f]{40}$' \
			|| echo "$job: $uses is not pinned to a full commit SHA"
		for pair in $(printf '%s' "${with#with=}" | tr ',' ' '); do
			case ${pair%%=*} in
			package | keep-n-tagged | exclude-tags | delete-untagged | dry-run) ;;
			*) echo "$job: the cleanup takes ${pair%%=*}, which this check does not allow" ;;
			esac
		done
		[ "$(with_value "$with" package)" = garam-test ] \
			|| echo "$job: package is $(with_value "$with" package), want garam-test"
		[ "$(with_value "$with" keep-n-tagged)" = 8 ] \
			|| echo "$job: keep-n-tagged is $(with_value "$with" keep-n-tagged), want 8"
		printf ',%s,' "$(with_value "$with" exclude-tags)" | grep -Fq ',pinned-*,' \
			|| echo "$job: exclude-tags is $(with_value "$with" exclude-tags), which does not exclude pinned-*"
		[ "$(with_value "$with" delete-untagged)" = true ] \
			|| echo "$job: delete-untagged is $(with_value "$with" delete-untagged), want true"
		if [ "$(with_value "$with" dry-run)" != true ]; then
			[ "$sif" = if=- ] || echo "$job: the deleting cleanup step has an if: (${sif#if=})"
			[ "$scoe" = coe=- ] || echo "$job: the deleting cleanup step sets continue-on-error"
			grep -Fxq "job$tab$job${tab}needs=publish${tab}if=$guard${tab}coe=-" "$scratch/parsed" \
				|| echo "$job: the deleting cleanup runs in a job that does not need publish alone, or whose if: is more than the repository guard"
			echo "$job" >>"$scratch/deleters"
		fi
	done
	[ -s "$scratch/deleters" ] || echo "no cleanup step deletes"
	[ "$(wc -l <"$scratch/deleters")" -le 1 ] || echo "more than one cleanup step deletes"
	grep -q "^step${tab}publish${tab}last$tab-${tab}push${tab}if=-${tab}coe=-$tab" "$scratch/parsed" \
		|| echo "publish does not end in scripts/image-push.sh, or an if: or continue-on-error lets it be skipped"
	grep -q "^job${tab}publish$tab.*${tab}coe=-\$" "$scratch/parsed" \
		|| echo "publish sets continue-on-error, so the prune runs after a failed push"
	grep "^job$tab" "$scratch/parsed" | while IFS=$tab read -r _ job _ jif _; do
		case ${jif#if=} in
		"$guard" | "$guard && "*) ;;
		*) echo "$job: its if: does not open with $guard" ;;
		esac
	done
	grep "^job${tab}dispatch$tab" "$scratch/parsed" | grep -Fq "&& github.ref == 'refs/heads/dev'" \
		|| echo "dispatch: its if: does not hold it to refs/heads/dev"
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
refused 'whose if: is more than the repository guard' "s|^    if: $guard\$|    if: always()|"
refused 'has an if:' 's/^      - uses: dataaxiom/      - if: always()\n        uses: dataaxiom/'
refused 'sets continue-on-error' 's/^      - uses: dataaxiom/      - continue-on-error: true\n        uses: dataaxiom/'
refused 'publish does not end in scripts/image-push.sh' '/scripts\/image-push.sh/d'
refused 'an if: or continue-on-error lets it be skipped' 's/^      - run: scripts\/image-push.sh/      - if: always()\n        run: scripts\/image-push.sh/'
refused 'an if: or continue-on-error lets it be skipped' 's/^      - run: scripts\/image-push.sh/      - continue-on-error: true\n        run: scripts\/image-push.sh/'
refused 'publish sets continue-on-error' 's/^  publish:/  publish:\n    continue-on-error: true/'
refused 'more than one cleanup step deletes' '/dry-run: true/d'
refused 'which this check does not allow' 's/^          keep-n-tagged: 8/          keep-n-tagged: 8\n          delete-tags: "*"/'
refused 'which this check does not allow' 's/^          keep-n-tagged: 8/          keep-n-tagged: 8\n          older-than: 1 day/'
refused 'delete-untagged is -, want true' '/delete-untagged:/d'
refused 'delete-untagged is false, want true' 's/delete-untagged: true/delete-untagged: false/'
refused 'package is garam, want garam-test' 's/package: garam-test/package: garam/'
refused "publish: its if: does not open with $guard" "s|^    if: $guard \&\& github.event_name == 'push'|    if: github.event_name == 'push'|"
refused "prune: its if: does not open with $guard" "\\|^    if: $guard\$|d"
refused "dispatch: its if: does not open with $guard" "s|^    if: $guard \&\& github.event_name == 'workflow_dispatch'|    if: github.event_name == 'workflow_dispatch'|"

refused 'dispatch: its if: does not hold it to refs/heads/dev' "s| \&\& github.ref == 'refs/heads/dev'||"

[ "$failed" -eq 0 ] || {
	echo "image-test-workflow-test: $failed of $cases cases failed" >&2
	exit 1
}
echo "image-test-workflow-test: $cases cases passed, read against $workflow"
