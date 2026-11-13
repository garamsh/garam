#!/bin/sh
# Checks which tags tooling/release-version.sh takes for a release
# (ADR-0157), and that .github/workflows/release.yml reads its version
# from that script and from nothing else.
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

# refused <tag>: exits non-zero, says why, and prints no version.
refused() {
	cases=$((cases + 1))
	if "$guard" "$1" >"$out" 2>"$err"; then
		fail "accepted $1"
	elif ! grep -Fq 'nothing published' "$err"; then
		cp "$err" "$out"
		fail "refused $1 without saying nothing was published"
	elif [ -s "$out" ]; then
		fail "refused $1 after printing a version"
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

# The guard takes exactly one argument.
cases=$((cases + 1))
if "$guard" >"$out" 2>&1 || "$guard" v0.1.0 v0.1.0 >"$out" 2>&1; then
	fail 'accepted a call without exactly one tag'
fi

# The workflow takes its version from the guard: a pattern written back
# into release.yml would leave the guard tested and unused.
cases=$((cases + 1))
: >"$out"
if ! grep -Fq "$guard \"\$REF_NAME\"" .github/workflows/release.yml; then
	fail "release.yml does not call $guard with the tag name"
elif grep -Fq 'grep -Eq' .github/workflows/release.yml; then
	fail 'release.yml matches a tag against a pattern of its own'
fi

[ "$failed" -eq 0 ] || {
	echo "release-version-test: $failed of $cases cases failed" >&2
	exit 1
}
echo "release-version-test: $cases cases passed, read against $root/$guard"
