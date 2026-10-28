#!/bin/sh
# record-runner-image writes the runner image the job's `runs-on`
# label resolved to into the job's step summary. `ubuntu-24.04` names
# no digest and the image behind it is rebuilt about weekly, so the
# run records what it got (docs/convention/ci.md §Verify a pinned
# dependency before adopting it).
#
# The value is the image version, not the provisioner version the job
# log prints four lines before it. runner-images writes it into the
# image's /etc/environment at build time,
# actions/runner-images@e3fa5414d230773024d4a669a8402b50cd28f036
# images/ubuntu/scripts/build/configure-environment.sh:12, and a step
# on a hosted runner reads it from its environment,
# actions/runner-images@381c440f34711afd641805c7ec44a1a2549d5f16
# .github/workflows/create_sbom_report.yml:53.
#
# An unset or empty value fails the step rather than record nothing,
# which would read the same as a record that was never made.

printf 'Runner image %s\n' \
	"${ImageVersion:?record-runner-image: ImageVersion is unset or empty — no runner image recorded}" \
	>>"${GITHUB_STEP_SUMMARY:?record-runner-image: GITHUB_STEP_SUMMARY is unset — not on a GitHub Actions runner, nothing recorded}"
