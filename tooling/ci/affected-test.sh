#!/bin/sh
# Checks the modules tooling/ci/affected.sh selects, and the reason it
# prints for each (#1661). Each case commits a change to a scratch
# repository laid out as this one is — two Go modules in go.work, one
# requiring the other; a pnpm package depending on the first through
# the table; a folder no module owns — and runs this tree's copy of the
# script against the commit before it, so the tree is clean whatever
# state this one is in.
#
# Every case reads both what is selected and what is not, so a script
# selecting everything fails here as surely as one selecting nothing.
set -eu

root=$(pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

repo=$scratch/repo
mkdir -p "$repo/tooling/ci" "$repo/lib" "$repo/app" "$repo/web" "$repo/docs"
cp tooling/ci/affected.sh "$repo/tooling/ci/"
cat >"$repo/tooling/ci/modules" <<'TABLE'
# A comment, and a blank line after it.

lib
app
web lib
tooling
TABLE
printf 'go 1.27\n\nuse (\n\t./app\n\t./lib\n)\n' >"$repo/go.work"
printf 'packages:\n  - web\n' >"$repo/pnpm-workspace.yaml"
printf 'module example.com/lib\n\ngo 1.27\n' >"$repo/lib/go.mod"
printf 'module example.com/app\n\ngo 1.27\n\nrequire example.com/lib v0.0.0\n' >"$repo/app/go.mod"
printf '{\n  "name": "web",\n  "private": true\n}\n' >"$repo/web/package.json"
printf 'ci:\nsuites:\n' >"$repo/lib/Makefile"
printf 'ci:\n' >"$repo/app/Makefile"
printf 'ci:\nsuites:\n' >"$repo/web/Makefile"
printf 'ci:\n' >"$repo/tooling/Makefile"
printf 'package lib\n' >"$repo/lib/lib.go"
printf 'package main\n' >"$repo/app/main.go"
printf 'export {};\n' >"$repo/web/index.ts"
printf '# Docs\n' >"$repo/docs/README.md"
printf 'all:\n' >"$repo/Makefile"

git() { command git -C "$repo" -c user.name=test -c user.email=test@invalid "$@"; }
git init -q
git add -A
git commit -qm base

cases=0
failed=0
fail() {
	echo "affected-test: $1" >&2
	failed=$((failed + 1))
}

# change <path...>: commits a line appended to each path, and runs the
# script against the commit before it, into $out.
out=$scratch/out
change() {
	for p in "$@"; do echo "// changed" >>"$repo/$p"; done
	git add -A
	git commit -qm change
	(cd "$repo" && AFFECTED_OUTPUT="$scratch/github-output" tooling/ci/affected.sh HEAD~1) >"$out" 2>&1 \
		|| { fail "the script failed on a change to $*: $(cat "$out")"; return 0; }
}

# has <line>: the output holds this line, exactly.
has() {
	grep -qxF -- "$1" "$out" || fail "expected the line '$1', got: $(cat "$out")"
}

cases=$((cases + 1))
rm -f "$scratch/github-output"
change app/main.go
has '  app: changed (app/main.go)'
has 'affected: 1 of 4 modules'
has 'affected: ci=["app"] suites=[] written to '"$scratch/github-output"
printf 'ci=["app"]\nsuites=[]\n' | cmp -s - "$scratch/github-output" \
	|| fail "the output file holds other lines than ci=[\"app\"] and suites=[]: $(cat "$scratch/github-output")"

cases=$((cases + 1))
change lib/lib.go
has '  lib: changed (lib/lib.go)'
has '  app: depends on lib'
has '  web: depends on lib'
has 'affected: 3 of 4 modules'
grep -q '^  tooling:' "$out" && fail "tooling was selected by a change to lib: $(cat "$out")"

cases=$((cases + 1))
change go.work
has '  lib: root file go.work'
has '  app: root file go.work'
has '  web: root file go.work'
has '  tooling: root file go.work'
has 'affected: 4 of 4 modules'

cases=$((cases + 1))
change docs/README.md
has 'affected: in no module, so selecting none: docs/README.md'
has 'affected: 0 of 4 modules'

# The promotion into the branch releases are cut from: every module,
# whatever changed.
cases=$((cases + 1))
change docs/README.md
(cd "$repo" && AFFECTED_EVERY='a promotion into main' tooling/ci/affected.sh HEAD~1) >"$out" 2>&1 \
	|| fail "the script failed on AFFECTED_EVERY: $(cat "$out")"
has '  app: a promotion into main'
has 'affected: 4 of 4 modules'

# A workspace module the table leaves out fails rather than going
# unselected; the control above is the same tree with it listed.
cases=$((cases + 1))
sed -i.bak '/^app$/d' "$repo/tooling/ci/modules" && rm "$repo/tooling/ci/modules.bak"
if (cd "$repo" && tooling/ci/affected.sh HEAD) >"$out" 2>&1; then
	fail "a go.work module missing from the table was not refused: $(cat "$out")"
else
	has 'affected: app is a workspace module and tooling/ci/modules does not list it'
fi

[ "$failed" -eq 0 ] || {
	echo "affected-test: $failed failures over $cases cases" >&2
	exit 1
}
echo "affected-test: $cases cases passed, read against $root/tooling/ci/affected.sh"
