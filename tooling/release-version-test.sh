#!/bin/sh
# Checks which tags tooling/release-version.sh takes for a release
# (ADR-0157), how it marks each on GitHub, and that
# .github/workflows/release.yml reads its version and those marks from
# that script and from nothing else.
#
# Every refusal is read beside an accepted control one edit away from
# it, so a guard refusing everything fails here as surely as one
# refusing nothing.
set -eu

root=$(pwd)
guard=tooling/release-version.sh
out=$(mktemp)
err=$(mktemp)
trap 'rm -f "$out" "$err"' EXIT INT TERM

cases=0
failed=0

fail() {
	echo "release-version-test: $1" >&2
	sed 's/^/  | /' "$out" >&2
	failed=$((failed + 1))
}

# accepted <tag> <version>: exits zero and prints the version alone.
accepted() {
	cases=$((cases + 1))
	if ! "$guard" "$1" >"$out" 2>&1; then
		fail "refused $1"
	elif [ "$(cat "$out")" != "$2" ]; then
		fail "$1 printed other than $2"
	fi
}

# marked <tag> <prerelease> <make_latest>: with --marks it prints those
# two lines and nothing else.
marked() {
	cases=$((cases + 1))
	if ! "$guard" --marks "$1" >"$out" 2>&1; then
		fail "--marks refused $1"
	elif [ "$(cat "$out")" != "$(printf 'prerelease=%s\nmake_latest=%s' "$2" "$3")" ]; then
		fail "--marks $1 printed other than prerelease=$2, make_latest=$3"
	fi
}

# refused <tag>: exits non-zero, says why, and prints no version and no
# marks.
refused() {
	cases=$((cases + 1))
	if "$guard" "$1" >"$out" 2>"$err"; then
		fail "accepted $1"
	elif ! grep -Fq 'nothing published' "$err"; then
		cp "$err" "$out"
		fail "refused $1 without saying nothing was published"
	elif [ -s "$out" ]; then
		fail "refused $1 after printing a version"
	elif "$guard" --marks "$1" >"$out" 2>"$err"; then
		fail "--marks accepted $1"
	elif [ -s "$out" ]; then
		fail "--marks refused $1 after printing marks"
	fi
}

# beside <accepted tag> <refused tag>: the accepted one is the control
# the refused one is a single edit from, and prints its tag without the
# `v`.
beside() {
	accepted "$1" "${1#v}"
	refused "$2"
}

accepted v0.1.0-dev.16 0.1.0-dev.16
accepted v0.1.0-dev.10 0.1.0-dev.10

# A development build or a candidate is a pre-release and not latest,
# and launch is the one release marked latest.
marked v0.1.0-dev.1 true false
marked v0.1.0-dev.16 true false
marked v0.1.0-rc.1 true false
marked v0.1.0-rc.12 true false
marked v0.1.0 false true

beside v0.1.0 v0.9.0
beside v0.1.0 v1.0.0
beside v0.1.0 v0.1.1
beside v0.1.0 v0.1
beside v0.1.0 v0.1.0.0
beside v0.1.0 v0.1.0+build
beside v0.1.0 v0.1.0-
beside v0.1.0 0.1.0
beside v0.1.0 vv0.1.0
beside v0.1.0 V0.1.0
beside v0.1.0 ' v0.1.0'
beside v0.1.0 'v0.1.0 '
beside v0.1.0 ''
beside v0.1.0-dev.1 v0.1.0-dev.0
beside v0.1.0-dev.1 v0.1.0-dev.01
beside v0.1.0-dev.1 v0.1.0-dev
beside v0.1.0-dev.1 v0.1.0-dev.
beside v0.1.0-dev.1 v0.1.0-dev.1.2
beside v0.1.0-dev.1 v0.1.0-dev.1.
beside v0.1.0-dev.1 v0.1.0-dev.x
beside v0.1.0-dev.1 v0.1.0-dev.-1
beside v0.1.0-dev.1 v0.1.0-dev.1-rc.1
beside v0.1.0-dev.1 v0.1.0-DEV.1
beside v0.1.0-dev.1 0.1.0-dev.1
beside v0.1.0-dev.1 v0.1.0-alpha.1
beside v0.1.0-dev.1 "$(printf 'v0.1.0-dev.1\nx')"
beside v0.1.0-dev.1 "$(printf 'x\nv0.1.0-dev.1')"
beside v0.1.0-rc.1 v0.1.0-rc.0
beside v0.1.0-rc.1 v0.1.0-rc.01
beside v0.1.0-rc.1 v0.1.0-rc
beside v0.1.0-rc.1 v0.1.0-rc.1.2
beside v0.1.0-rc.1 v0.1.0-rc.1+build
beside v0.1.0-rc.9 v0.1.0-rc.09

# The guard takes exactly one tag, after the one option it has.
cases=$((cases + 1))
if "$guard" >"$out" 2>&1 || "$guard" v0.1.0 v0.1.0 >"$out" 2>&1 \
	|| "$guard" --marks >"$out" 2>&1 \
	|| "$guard" --marks v0.1.0 v0.1.0 >"$out" 2>&1 \
	|| "$guard" --other v0.1.0 >"$out" 2>&1; then
	fail 'accepted a call without exactly one tag'
fi

# The workflow takes its version and its marks from the guard: a
# pattern written back into release.yml would leave the guard tested
# and unused.
workflow=.github/workflows/release.yml
cases=$((cases + 1))
: >"$out"
if ! grep -Fq "$guard \"\$REF_NAME\"" "$workflow"; then
	fail "release.yml does not call $guard with the tag name"
elif ! grep -Fq "$guard --marks \"\$REF_NAME\" >> \"\$GITHUB_OUTPUT\"" "$workflow"; then
	fail "release.yml does not take its marks from $guard --marks"
elif grep -Fq 'grep -Eq' "$workflow"; then
	fail 'release.yml matches a tag against a pattern of its own'
fi

# Nothing outside a comment names a release form or reads the tag in an
# expression, so no second rule decides whether a release is a
# pre-release or latest. The marks reach the action as the step
# outputs and as nothing derived from them.
cases=$((cases + 1))
grep -v '^[[:space:]]*#' "$workflow" >"$out" || :
if grep -Eq -e '-(dev|rc)|v0\.1\.0|startsWith|endsWith|contains\(|ref_name.*(==|!=)' "$out"; then
	grep -En -e '-(dev|rc)|v0\.1\.0|startsWith|endsWith|contains\(|ref_name.*(==|!=)' "$out" >"$err" || :
	cp "$err" "$out"
	fail 'release.yml decides on a tag form of its own'
fi
cases=$((cases + 1))
: >"$out"
if ! grep -Fq 'prerelease: ${{ needs.images.outputs.prerelease }}' "$workflow" \
	|| ! grep -Fq 'make_latest: ${{ needs.images.outputs.make-latest }}' "$workflow" \
	|| ! grep -Fq 'prerelease: ${{ steps.version.outputs.prerelease }}' "$workflow" \
	|| ! grep -Fq 'make-latest: ${{ steps.version.outputs.make_latest }}' "$workflow"; then
	fail 'release.yml does not pass the guard marks to the release action'
fi

[ "$failed" -eq 0 ] || {
	echo "release-version-test: $failed of $cases cases failed" >&2
	exit 1
}
echo "release-version-test: $cases cases passed, read against $root/$guard"
