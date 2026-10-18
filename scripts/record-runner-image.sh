#!/bin/sh
# record-runner-image writes what the job's `runs-on` label resolved to
# into the job's step summary. Neither label names a digest: the image
# behind `ubuntu-24.04` is rebuilt about weekly, and the scale set
# behind `aurora` is whatever garamsh/aurora's main held when it last
# synced, so the run records what it got (docs/convention/ci.md §Verify
# a pinned dependency before adopting it, ADR-0112).
#
# RUNNER_ENVIRONMENT, which the runner sets, says which of the two the
# job is on. Any other value fails the step rather than guess.
#
# On a hosted runner the value is the image version, not the provisioner
# version the job log prints four lines before it. runner-images writes
# it into the image's /etc/environment at build time,
# actions/runner-images@e3fa5414d230773024d4a669a8402b50cd28f036
# images/ubuntu/scripts/build/configure-environment.sh:12, and a step
# on a hosted runner reads it from its environment,
# actions/runner-images@381c440f34711afd641805c7ec44a1a2549d5f16
# .github/workflows/create_sbom_report.yml:53.
#
# On aurora no image version is set. The runner prints its own version
# in the job's set-up log, so what is left to record is which runner
# took the job and which node it ran on: RUNNER_NAME, which the runner
# sets, and NODE_NAME, which aurora's hook template sets in the job
# container from the job pod's spec.nodeName
# (garamsh/aurora@4df93d567fddb9afe9e5120449053222dee2b7c7
# kubernetes/components/gha-runner-scale-set/templates/hook-template.yaml).
# The runner image reference reaches no variable in the job container,
# so it is not recorded here.
#
# An unset or empty value fails the step rather than record nothing,
# which would read the same as a record that was never made.

summary="${GITHUB_STEP_SUMMARY:?record-runner-image: GITHUB_STEP_SUMMARY is unset — not on a GitHub Actions runner, nothing recorded}"

case "${RUNNER_ENVIRONMENT:-}" in
github-hosted)
	printf 'Runner image %s\n' \
		"${ImageVersion:?record-runner-image: ImageVersion is unset or empty — no runner image recorded}" \
		>>"$summary"
	;;
self-hosted)
	printf 'Runner %s on node %s\n' \
		"${RUNNER_NAME:?record-runner-image: RUNNER_NAME is unset or empty — no runner recorded}" \
		"${NODE_NAME:?record-runner-image: NODE_NAME is unset or empty — no node recorded}" \
		>>"$summary"
	;;
*)
	echo "record-runner-image: RUNNER_ENVIRONMENT is '${RUNNER_ENVIRONMENT:-}', neither github-hosted nor self-hosted — nothing recorded" >&2
	exit 1
	;;
esac
