#!/bin/sh
# affected prints the modules a change can affect, each with the reason
# it was chosen (ci.md §One entry point per task, #1661): the modules
# whose files the change touches, every module depending on one of
# those, and every module when the change touches a file the root
# owns. The modules and the edges no workspace file declares are
# tooling/ci/modules; the rest are read from go.work's go.mod files and
# from pnpm `workspace:` dependencies.
#
# Usage: tooling/ci/affected.sh <base>, from the repository root. The
# change is everything between the merge base of <base> and HEAD and the
# working tree, so a local run counts what is not committed yet.
#
# AFFECTED_EVERY, when set, selects every module with it as the reason:
# a promotion into the branch releases are cut from runs them all.
# AFFECTED_OUTPUT, when set, names a file the selection is appended to
# as `ci=` and `suites=` lines, each a JSON list of the selected
# modules defining that verb, in the form GitHub Actions reads from
# $GITHUB_OUTPUT.

set -eu

# What the repository root owns: a change to any of them can reach every
# module. A path ending in / is a folder.
ROOT_FILES='go.work go.work.sum package.json pnpm-lock.yaml pnpm-workspace.yaml Makefile .gitignore .dockerignore .github/ tooling/'

base=${1:-}
test -n "$base" || { echo 'affected: no base — usage: tooling/ci/affected.sh <base>' >&2; exit 2; }
table=tooling/ci/modules
test -f "$table" || { echo "affected: $table not found — run from the repository root" >&2; exit 2; }

fail() { echo "affected: $*" >&2; exit 1; }

# The table, comments dropped: one "module dep dep" line each.
rows="$(sed -e 's/#.*//' -e 's/[[:space:]]*$//' "$table" | grep -v '^$' || true)"
test -n "$rows" || fail "$table lists no module"
modules="$(printf '%s\n' "$rows" | awk '{print $1}')"
count="$(printf '%s\n' "$modules" | wc -l | tr -d ' ')"

listed() { printf '%s\n' "$modules" | grep -qxF "$1"; }

for m in $modules; do
	test -f "$m/Makefile" || fail "$table lists $m, and $m/Makefile does not exist"
done
for dep in $(printf '%s\n' "$rows" | awk '{for (i = 2; i <= NF; i++) print $i}'); do
	listed "$dep" || fail "$table names $dep as a dependency, and lists no such module"
done

# Every module a workspace file names is in the table.
workspace="$(sed -n 's|^[[:space:]]*\./\([^[:space:])]*\).*|\1|p' go.work 2>/dev/null || true)
$(sed -n 's|^[[:space:]]*-[[:space:]]*\([^[:space:]#]*\).*|\1|p' pnpm-workspace.yaml 2>/dev/null || true)"
for w in $workspace; do
	listed "$w" || fail "$w is a workspace module and $table does not list it"
done

# The edges: the table's, then go.mod requires between Go modules, then
# pnpm workspace: dependencies between packages. One "module dep" line
# each.
edges="$(printf '%s\n' "$rows" | awk '{for (i = 2; i <= NF; i++) print $1, $i}')"
for m in $modules; do
	if test -f "$m/go.mod"; then
		path="$(sed -n 's/^module[[:space:]]*//p' "$m/go.mod")"
		for n in $modules; do
			test "$n" != "$m" && test -f "$n/go.mod" || continue
			if grep -Eq "^([[:space:]]|require[[:space:]]+)$path[[:space:]]+v" "$n/go.mod"; then
				edges="$edges
$n $m"
			fi
		done
	fi
	if test -f "$m/package.json"; then
		name="$(sed -n 's/^  "name": "\(.*\)",$/\1/p' "$m/package.json")"
		for n in $modules; do
			test "$n" != "$m" && test -f "$n/package.json" || continue
			if grep -Fq "\"$name\": \"workspace:" "$n/package.json"; then
				edges="$edges
$n $m"
			fi
		done
	fi
done

# The module a path falls in: the longest module folder holding it.
module_of() {
	best=''
	for m in $modules; do
		case $1 in "$m"/*) test "${#m}" -gt "${#best}" && best=$m ;; esac
	done
	printf '%s' "$best"
}

root_file() {
	for r in $ROOT_FILES; do
		case $r in
		*/) case $1 in "$r"*) return 0 ;; esac ;;
		*) test "$1" = "$r" && return 0 ;;
		esac
	done
	return 1
}

# selected holds "module<TAB>reason" lines, in the order chosen.
selected=''
tab="$(printf '\t')"
is_selected() { printf '%s\n' "$selected" | cut -f1 | grep -qxF "$1"; }
select_module() { is_selected "$1" || selected="$selected$1$tab$2
"; }

if test -n "${AFFECTED_EVERY:-}"; then
	for m in $modules; do select_module "$m" "$AFFECTED_EVERY"; done
	echo "affected: every module — $AFFECTED_EVERY"
else
	mergebase="$(git merge-base "$base" HEAD)" || fail "git found no merge base of $base and HEAD"
	files="$(git diff --name-only "$mergebase" --)"
	nfiles="$(printf '%s' "$files" | grep -c '' || true)"
	echo "affected: $nfiles files changed since $base (merge base $(printf '%.12s' "$mergebase")), read by git diff against the working tree"
	outside=''
	for f in $files; do
		if root_file "$f"; then
			for m in $modules; do select_module "$m" "root file $f"; done
			continue
		fi
		m="$(module_of "$f")"
		if test -n "$m"; then
			select_module "$m" "changed ($f)"
		else
			outside="$outside $f"
		fi
	done
	test -z "$outside" || echo "affected: in no module, so selecting none:$outside"

	# Every module depending on a selected one, until none is added.
	while :; do
		added=''
		for m in $(printf '%s' "$selected" | cut -f1); do
			for n in $(printf '%s\n' "$edges" | awk -v m="$m" '$2 == m {print $1}'); do
				is_selected "$n" && continue
				select_module "$n" "depends on $m"
				added=1
			done
		done
		test -n "$added" || break
	done
fi

n="$(printf '%s' "$selected" | grep -c '' || true)"
echo "affected: $n of $count modules"
ci='' suites=''
for m in $modules; do
	is_selected "$m" || continue
	reason="$(printf '%s' "$selected" | awk -F "$tab" -v m="$m" '$1 == m {print $2}')"
	echo "  $m: $reason"
	grep -q '^ci:' "$m/Makefile" && ci="$ci,\"$m\""
	grep -q '^suites:' "$m/Makefile" && suites="$suites,\"$m\""
done

if test -n "${AFFECTED_OUTPUT:-}"; then
	printf 'ci=[%s]\nsuites=[%s]\n' "${ci#,}" "${suites#,}" >> "$AFFECTED_OUTPUT"
	echo "affected: ci=[${ci#,}] suites=[${suites#,}] written to $AFFECTED_OUTPUT"
fi
