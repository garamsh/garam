#!/bin/sh
# Checks how .github/workflows/test-image.yml reaches the registry
# (ADR-0093):
#
# - it triggers on a push to `dev` and on a dispatch, and on nothing
#   else;
# - every job names this repository in its `if:`, publish runs on the
#   push alone and dispatch only from `dev`;
# - each of those two jobs grants `contents: read` and `id-token: write`
#   and nothing else, runs scripts/image-test-target.sh before it
#   assumes the publisher role through
#   aws-actions/configure-aws-credentials, and ends in what it exists
#   for: publish in the push, dispatch in the pin;
# - the steps that build, push and pin take IMAGE_REPOSITORY from their
#   `env:`, built from the ECR login's registry and
#   TEST_IMAGE_ECR_REPOSITORY, and name it in their `run:`;
# - no `run:` holds a `${{` expression, which would put its value into
#   the command text rather than the environment;
# - no step on that path carries an `if:` or `continue-on-error`, and
#   no step uses an action outside actions/checkout and the two
#   aws-actions, each pinned to a full commit SHA, so nothing prunes
#   and nothing logs in to GHCR;
# - the workflow names the repository and the publisher role
#   garamsh/infra declared, and scripts/image-test-target.sh admits
#   them while it refuses the release role and every other account.
#
# The check runs on the workflow and then on copies of it, each with
# one of those properties removed, every one of which it must refuse:
# a check refusing nothing fails here as surely as one refusing
# everything.
set -eu

workflow=.github/workflows/test-image.yml
target=scripts/image-test-target.sh
guard="github.repository == 'garamsh/garam'"
role_expr='${{ env.TEST_IMAGE_PUBLISHER_ROLE }}'
repository_expr='${{ steps.ecr.outputs.registry }}/${{ env.TEST_IMAGE_ECR_REPOSITORY }}'
want_role=arn:aws:iam::486152169996:role/garamsh-garam-github-actions-dev
want_repository=garam/garam-test
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM
tab=$(printf '\t')

# One tab-separated line per trigger, workflow variable, job,
# permission and step, read off the indentation the file is written
# in: top-level keys at 0, their keys at 2, a trigger's keys at 4, job
# keys at 4, permissions at 6, steps at 6, step keys at 8, and `with:`
# and `env:` keys at 10. A `run:` block is every line under its key
# indented past 8.
#
#   on <event>
#   on-key <event> <key>=<value>
#   env <name>=<value>
#   job <name> if=<if> coe=<continue-on-error>
#   perm <job> <key>=<value>
#   step <job> <n> last|- uses=<uses> run=<run> if=<if> coe=<coe> with=<k=v,...> env=<k=v,...>
#   runexpr <job> <line>
#
# <run> is the key's own line; runexpr names each line of a `run:`,
# comments included, that holds `${{`.
# A key the file does not set reads `-`.
parse() {
	awk '
		inrun {
			if ($0 ~ /^[[:space:]]*$/) next
			match($0, /^ */)
			if (RLENGTH > 8) {
				if (index($0, "${{")) printf "runexpr\t%s\t%d\n", job, NR
				next
			}
			inrun = 0
		}
		function val(line) {
			sub(/^[^:]*:[[:space:]]*/, "", line)
			sub(/[[:space:]]+#.*/, "", line)
			return line == "" ? "-" : line
		}
		function key(line) {
			sub(/^[[:space:]]+/, "", line)
			sub(/:.*/, "", line)
			return line
		}
		function step_end() {
			if (instep) {
				nsteps++
				steps[nsteps] = sprintf("uses=%s\trun=%s\tif=%s\tcoe=%s\twith=%s\tenv=%s", \
					uses, run, sif, scoe, (with == "" ? "-" : with), (senv == "" ? "-" : senv))
			}
			instep = 0; inwith = 0; inenv = 0; uses = "-"; run = "-"; sif = "-"; scoe = "-"; with = ""; senv = ""
		}
		function job_end() {
			step_end()
			for (i = 1; i <= nsteps; i++)
				printf "step\t%s\t%d\t%s\t%s\n", job, i, (i == nsteps ? "last" : "-"), steps[i]
			if (job != "") printf "job\t%s\tif=%s\tcoe=%s\n", job, jif, jcoe
			nsteps = 0; jif = "-"; jcoe = "-"; inperm = 0
		}
		/^[[:space:]]*(#.*)?$/ { next }
		/^[^[:space:]]/ { job_end(); job = ""; top = key($0); next }
		top == "on" && /^  [A-Za-z0-9_-]+:/ { event = key($0); print "on\t" event; next }
		top == "on" && /^    [A-Za-z0-9_-]+:/ { printf "on-key\t%s\t%s=%s\n", event, key($0), val($0); next }
		top == "env" && /^  [A-Za-z0-9_-]+:/ { printf "env\t%s=%s\n", key($0), val($0); next }
		top != "jobs" { next }
		/^  [A-Za-z0-9_-]+:[[:space:]]*$/ { job_end(); job = key($0); next }
		/^    if:/ { jif = val($0); inperm = 0; next }
		/^    continue-on-error:/ { jcoe = val($0); inperm = 0; next }
		/^    permissions:/ { inperm = 1; next }
		/^    [A-Za-z0-9_-]+:/ { inperm = 0; next }
		inperm && /^      [A-Za-z0-9_-]+:/ { printf "perm\t%s\t%s=%s\n", job, key($0), val($0); next }
		/^      - / { step_end(); instep = 1; sub(/^      - /, "        ") }
		instep && /^        [A-Za-z0-9_-]+:/ {
			inwith = 0; inenv = 0
			k = key($0)
			if (k == "uses") uses = val($0)
			else if (k == "run") {
				run = val($0); inrun = 1
				if (index($0, "${{")) printf "runexpr\t%s\t%d\n", job, NR
			}
			else if (k == "if") sif = val($0)
			else if (k == "continue-on-error") scoe = val($0)
			else if (k == "with") inwith = 1
			else if (k == "env") inenv = 1
			next
		}
		instep && inwith && /^          [A-Za-z0-9_-]+:/ {
			with = with (with == "" ? "" : ",") key($0) "=" val($0)
		}
		instep && inenv && /^          [A-Za-z0-9_-]+:/ {
			senv = senv (senv == "" ? "" : ",") key($0) "=" val($0)
		}
		END { job_end() }
	' "$1"
}

# with_value <with=...|env=...> <key>: the value the step gives the
# key, or -.
with_value() {
	printf ',%s,' "${1#*=}" | sed -n "s/.*,$2=\\([^,]*\\),.*/\\1/p" | grep . || echo -
}

# env_value <parsed> <name>: the workflow variable's value, or -.
env_value() {
	sed -n "s/^env$tab$2=//p" "$1" | grep . || echo -
}

# first_step <parsed> <job> <field pattern>: the number of the job's
# first step whose line matches, or nothing.
first_step() {
	grep "^step$tab$2$tab" "$1" | grep -E "$3" | head -n 1 | cut -f 3
}

# check <file>: prints one line per property the file breaks.
check() {
	parsed=$scratch/parsed
	parse "$1" >"$parsed"

	events=$(sed -n "s/^on$tab//p" "$parsed" | sort | tr '\n' ' ')
	[ "$events" = 'push workflow_dispatch ' ] \
		|| echo "on: triggers on '$events', want push and workflow_dispatch alone"
	[ "$(grep -c "^on-key${tab}push$tab" "$parsed")" -eq 1 ] \
		&& grep -Fxq "on-key${tab}push${tab}branches=[dev]" "$parsed" \
		|| echo "on: push is not filtered to branches: [dev] alone"

	grep "^job$tab" "$parsed" | while IFS=$tab read -r _ job jif _; do
		case ${jif#if=} in
		"$guard" | "$guard && "*) ;;
		*) echo "$job: its if: does not open with $guard" ;;
		esac
	done
	grep -Fxq "job${tab}publish${tab}if=$guard && github.event_name == 'push'${tab}coe=-" "$parsed" \
		|| echo "publish: its if: is not the repository guard and the push, or it sets continue-on-error"
	grep -Fxq "job${tab}dispatch${tab}if=$guard && github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/dev'${tab}coe=-" "$parsed" \
		|| echo "dispatch: its if: does not hold it to a dispatch from refs/heads/dev, or it sets continue-on-error"

	grep "^step$tab" "$parsed" | while IFS=$tab read -r _ job _ _ uses _; do
		action=${uses#uses=}
		[ "$action" = - ] && continue
		case ${action%@*} in
		actions/checkout | aws-actions/configure-aws-credentials | aws-actions/amazon-ecr-login) ;;
		*) echo "$job: uses ${action%@*}, which this workflow does not allow" ;;
		esac
		printf '%s' "${action#*@}" | grep -Eq '^[0-9a-f]{40}$' \
			|| echo "$job: $action is not pinned to a full commit SHA"
	done

	for job in publish dispatch; do
		perms=$(sed -n "s/^perm$tab$job$tab//p" "$parsed" | sort | tr '\n' ' ')
		[ "$perms" = 'contents=read id-token=write ' ] \
			|| echo "$job: grants '$perms', want contents: read and id-token: write alone"

		auth=$(first_step "$parsed" "$job" "${tab}uses=aws-actions/configure-aws-credentials@")
		if [ -z "$auth" ]; then
			echo "$job: no step assumes a role with aws-actions/configure-aws-credentials"
			continue
		fi
		line=$(grep "^step$tab$job$tab$auth$tab" "$parsed")
		with=$(printf '%s' "$line" | cut -f 9)
		[ "$(with_value "$with" role-to-assume)" = "$role_expr" ] \
			|| echo "$job: role-to-assume is $(with_value "$with" role-to-assume), want $role_expr"
		[ "$(with_value "$with" aws-region)" = ap-northeast-2 ] \
			|| echo "$job: aws-region is $(with_value "$with" aws-region), want ap-northeast-2"

		refusal=$(first_step "$parsed" "$job" "${tab}run=$target \"\\\$TEST_IMAGE_PUBLISHER_ROLE\" \"\\\$TEST_IMAGE_ECR_REPOSITORY\"$tab")
		[ -n "$refusal" ] && [ "$refusal" -lt "$auth" ] \
			|| echo "$job: $target does not run on the workflow's role and repository before the role is assumed"

		# Every step from the refusal to the last is on the path to the
		# push or the pin, so none of them may be skipped past.
		grep "^step$tab$job$tab" "$parsed" | while IFS=$tab read -r _ _ n _ _ _ sif scoe _; do
			[ "$n" -ge "${refusal:-1}" ] || continue
			[ "$sif" = if=- ] || echo "$job: step $n has an if: (${sif#if=})"
			[ "$scoe" = coe=- ] || echo "$job: step $n sets continue-on-error"
		done
	done
	# Each step that reads the repository, by the command it opens with.
	for want in 'publish make test-image-e2e' 'publish scripts/image-push.sh' 'dispatch scripts/image-test-pin.sh'; do
		job=${want%% *} command=${want#* }
		line=$(grep "^step$tab$job$tab[0-9]*$tab[^$tab]*${tab}uses=-${tab}run=$command " "$parsed" | head -n 1)
		[ -n "$line" ] || continue
		env=$(printf '%s' "$line" | cut -f 10)
		[ "$(with_value "$env" IMAGE_REPOSITORY)" = "$repository_expr" ] \
			|| echo "$job: $command takes IMAGE_REPOSITORY $(with_value "$env" IMAGE_REPOSITORY), want $repository_expr"
		printf '%s' "$line" | cut -f 6 | grep -Eq '\$IMAGE_REPOSITORY([^A-Za-z0-9_]|$)' \
			|| echo "$job: $command does not name \$IMAGE_REPOSITORY in its run:"
	done
	sed -n "s/^runexpr$tab//p" "$parsed" | while IFS=$tab read -r job n; do
		echo "$job: the run: at line $n holds a \${{ expression"
	done

	grep -q "^step${tab}publish$tab[0-9]*${tab}last${tab}uses=-${tab}run=scripts/image-push.sh " "$parsed" \
		|| echo "publish does not end in scripts/image-push.sh"
	grep -q "^step${tab}dispatch$tab[0-9]*${tab}last${tab}uses=-${tab}run=scripts/image-test-pin.sh " "$parsed" \
		|| echo "dispatch does not end in scripts/image-test-pin.sh"

	role=$(env_value "$parsed" TEST_IMAGE_PUBLISHER_ROLE)
	repository=$(env_value "$parsed" TEST_IMAGE_ECR_REPOSITORY)
	[ "$role" = "$want_role" ] \
		|| echo "env: TEST_IMAGE_PUBLISHER_ROLE is $role, want $want_role"
	[ "$repository" = "$want_repository" ] \
		|| echo "env: TEST_IMAGE_ECR_REPOSITORY is $repository, want $want_repository"
	"$target" "$role" "$repository" >/dev/null 2>&1 \
		|| echo "env: $target refuses $role and $repository"
	return 0
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

publisher=$want_role

refused "on: push is not filtered to branches: [dev] alone" 's/branches: \[dev\]/branches: [dev, main]/'
refused "on: push is not filtered to branches: [dev] alone" '/branches: \[dev\]/d'
refused "on: triggers on 'pull_request push workflow_dispatch '" 's/^on:$/on:\n  pull_request:/'
refused "publish: its if: does not open with $guard" "s|^    if: $guard \&\& github.event_name == 'push'|    if: github.event_name == 'push'|"
refused "dispatch: its if: does not open with $guard" "s|^    if: $guard \&\& github.event_name == 'workflow_dispatch'|    if: github.event_name == 'workflow_dispatch'|"
refused 'dispatch: its if: does not hold it to a dispatch from refs/heads/dev' "s| \&\& github.ref == 'refs/heads/dev'||"
refused 'publish: its if: is not the repository guard and the push' "s| \&\& github.event_name == 'push'||"
refused 'publish: grants' 's/^      id-token: write/      id-token: write\n      packages: write/'
refused 'dispatch: grants' '/^      id-token: write/{x;s/^/x/;/^xx$/{x;d};x}'
refused 'publish: no step assumes a role' '0,/configure-aws-credentials@/{/configure-aws-credentials@/d}'
refused 'is not pinned to a full commit SHA' 's/configure-aws-credentials@[0-9a-f]*/configure-aws-credentials@v6.2.3/'
refused 'uses docker/login-action, which this workflow does not allow' 's|^      - id: ecr$|      - uses: docker/login-action@dbcb813823bdd20940b903addbd779551569679f\n\n      - id: ecr|'
refused 'role-to-assume is arn:aws:iam::486152169996:role/garamsh-garam-github-actions' 's|role-to-assume: .*|role-to-assume: arn:aws:iam::486152169996:role/garamsh-garam-github-actions|'
refused 'aws-region is us-east-1, want ap-northeast-2' 's/aws-region: ap-northeast-2/aws-region: us-east-1/'
refused "publish: $target does not run" "0,\\|run: $target|{\\|run: $target|d}"
refused "dispatch: $target does not run" "\\|run: $target|{x;s/^/x/;/^xx\$/{x;d};x}"
refused "publish: $target does not run" "s|^      - run: $target \"\\\$TEST_IMAGE_PUBLISHER_ROLE\" \"\\\$TEST_IMAGE_ECR_REPOSITORY\"|      - run: $target \"$publisher\" \"\\\$TEST_IMAGE_ECR_REPOSITORY\"|"
refused 'has an if:' "s|^      - run: $target|      - if: always()\n        run: $target|"
refused 'sets continue-on-error' 's/^      - uses: aws-actions\/configure-aws-credentials/      - continue-on-error: true\n        uses: aws-actions\/configure-aws-credentials/'
refused 'has an if:' 's/^      - env:/      - if: always()\n        env:/'
refused 'publish does not end in scripts/image-push.sh' '/scripts\/image-push.sh/d'
refused 'dispatch does not end in scripts/image-test-pin.sh' '/scripts\/image-test-pin.sh/d'
refused 'or it sets continue-on-error' 's/^  publish:/  publish:\n    continue-on-error: true/'
refused "env: $target refuses" "s|^  TEST_IMAGE_PUBLISHER_ROLE: .*|  TEST_IMAGE_PUBLISHER_ROLE: arn:aws:iam::486152169996:role/garamsh-garam-github-actions|"
refused "env: TEST_IMAGE_PUBLISHER_ROLE is arn:aws:iam::123456789012:role/garamsh-garam-github-actions-dev" "s|486152169996:role/garamsh-garam-github-actions-dev|123456789012:role/garamsh-garam-github-actions-dev|"
refused "env: TEST_IMAGE_ECR_REPOSITORY is garam/garam-test-2, want $want_repository" "s|^  TEST_IMAGE_ECR_REPOSITORY: .*|  TEST_IMAGE_ECR_REPOSITORY: garam/garam-test-2|"
refused "env: $target refuses" "s|^  TEST_IMAGE_ECR_REPOSITORY: .*|  TEST_IMAGE_ECR_REPOSITORY: Garam Test|"
refused 'publish: scripts/image-push.sh takes IMAGE_REPOSITORY 486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/other' '/^      - env:$/{n;/IMAGE_REPOSITORY/{N;/image-push/s|IMAGE_REPOSITORY: .*\n|IMAGE_REPOSITORY: 486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/other\n|;};}'
refused 'publish: make test-image-e2e takes IMAGE_REPOSITORY' '/^      - env:$/{n;/IMAGE_REPOSITORY/{N;/make test-image-e2e/s|IMAGE_REPOSITORY: .*\n|IMAGE_REPOSITORY: garam-test\n|;};}'
refused 'dispatch: scripts/image-test-pin.sh takes IMAGE_REPOSITORY -' '/^      - env:$/{n;/IMAGE_REPOSITORY/{N;/COMMIT:/d;};}'
refused 'publish: scripts/image-push.sh does not name $IMAGE_REPOSITORY in its run:' 's|scripts/image-push.sh "test-image=$IMAGE_REPOSITORY:|scripts/image-push.sh "test-image=garam/other:|'
refused 'dispatch: the run: at line' 's|scripts/image-test-pin.sh "$IMAGE_REPOSITORY" "$COMMIT"|scripts/image-test-pin.sh "$IMAGE_REPOSITORY" "${{ inputs.commit }}"|'
refused 'publish: the run: at line' 's|^      - run: scripts/oidc-subject.sh|      - run: \|\n          echo ok\n          echo "${{ github.ref_name }}"|'
refused 'publish: the run: at line' 's|^      - run: scripts/oidc-subject.sh|      - run: \|\n          # ${{ github.ref_name }}\n          scripts/oidc-subject.sh|'

# The script on its own, beyond what the workflow names today: the
# release role, a foreign account and an empty repository are
# refused, the declared pair is admitted.
admits() {
	cases=$((cases + 1))
	if "$target" "$2" "$3" >/dev/null 2>&1; then got=0; else got=1; fi
	[ "$got" = "$1" ] || {
		echo "image-test-target: '$2' '$3' exited $got, want $1" >&2
		failed=$((failed + 1))
	}
}
admits 0 "$publisher" garam/garam-test
admits 1 arn:aws:iam::486152169996:role/garamsh-garam-github-actions garam/garam-test
admits 1 arn:aws:iam::123456789012:role/garam-test-publisher garam/garam-test
admits 1 "$publisher" ''
admits 1 "$publisher" garam/Garam-Test

[ "$failed" -eq 0 ] || {
	echo "image-test-workflow-test: $failed of $cases cases failed" >&2
	exit 1
}
echo "image-test-workflow-test: $cases cases passed, read against $workflow and $target"
