#!/bin/sh
# db-prune drops the test databases whose worktree is gone. The
# Makefile's TEST_DB_NAME suffixes `garam_test` with git's own name for
# a linked worktree, so `make db-up` creates one database per worktree
# and, until this target, nothing removed it again: a worktree's
# database outlived the worktree, and only `make db-down` — which
# removes the container and takes the volume with it — ever collected
# them (#934). That is why the accumulation was invisible: the escape
# hatch is destructive, so anyone keeping their stack up, which
# deploy/local/README.md describes as the ordinary way to work,
# accumulated indefinitely.
#
# What it keeps is `garam_test` itself and one database per worktree
# still on disk. A worktree is on disk when the `gitdir` file in its
# administrative directory names a path that is there — git's own test
# for whether a worktree is prunable — and an administrative directory
# that cannot answer that question fails the run rather than losing its
# database to a read that came back empty.
#
# The two counts a zero could lie about are both held against a
# control. The database list is asked of pg_database unfiltered, which
# always holds template0, template1 and postgres, so a list that comes
# back empty is a broken read and not an empty server; the filtering to
# `garam_test` happens here, where an honest zero is distinguishable
# from that. The worktree enumeration is held against `git worktree
# list`, and then against the worktree this very run stands in: an
# enumeration that resolved the wrong path keeps nothing, and the
# database this worktree uses is the one name that is certainly in use.
#
# COMPOSE_LOCAL and TEST_DB_NAME arrive in the environment; the
# Makefile holds both.

fail() { echo "db-prune: $1" >&2; exit 1; }

test -n "$COMPOSE_LOCAL" || fail 'COMPOSE_LOCAL is empty — the Makefile holds it'
test -n "$TEST_DB_NAME" || fail 'TEST_DB_NAME is empty — the Makefile holds it'

# Whether a database exists is psql's output; whether there is an
# answer at all is its exit status. The two are read separately,
# because one non-zero status cannot tell a failed query from a clean
# negative — the same split `db-up` makes.
all="$($COMPOSE_LOCAL exec -T postgres psql -U garam -d postgres -tAc \
	'SELECT datname FROM pg_database ORDER BY datname')" \
	|| fail 'could not ask postgres for its databases — run make db-up'
total="$(printf '%s\n' "$all" | grep -c .)"
test "$total" -gt 0 \
	|| fail 'postgres named no database at all — pg_database always holds template0, template1 and postgres, so that is a broken read and not an empty server'

set -e

# The keep set: `garam_test`, plus one name per worktree still on disk.
# The suffix is the administrative directory's name, which is exactly
# what `git rev-parse --git-dir` gives the Makefile to cut at line 59.
common="$(cd "$(git rev-parse --git-common-dir)" && pwd)"
admin="$common/worktrees"
keep='garam_test'
found=0
live=0
for dir in "$admin"/*/; do
	test -d "$dir" || continue
	found=$((found + 1))
	name="${dir%/}"; name="${name##*/}"
	gitdir="$admin/$name/gitdir"
	test -r "$gitdir" \
		|| fail "$gitdir is not readable — whether that worktree is still on disk cannot be established, so nothing is dropped"
	test -e "$(cat "$gitdir")" || continue
	live=$((live + 1))
	keep="$keep
garam_test_$name"
done

# Control one: git's own count of the worktrees it tracks. An
# enumeration that read the wrong directory, or none, disagrees here
# rather than quietly keeping nothing.
listed="$(git worktree list --porcelain | grep -c '^worktree ' || true)"
test "$listed" -gt 0 \
	|| fail 'git worktree list named no worktree — a repository always has at least the one this runs in, so that is a broken read'
test "$found" -eq "$((listed - 1))" \
	|| fail "git tracks $((listed - 1)) linked worktrees and $admin holds $found — the enumeration the keep set rests on is broken"

# Control two: the database this run's own worktree uses. It is in use
# by definition, so an enumeration that dropped it would be dropping a
# live database, and the derivation is held against the Makefile's.
self="garam_test$(git rev-parse --git-dir | sed -n 's|.*/worktrees/|_|p')"
test "$self" = "$TEST_DB_NAME" \
	|| fail "the Makefile names $TEST_DB_NAME and this script derives $self — the Makefile's TEST_DB_NAME and the rule here no longer agree"
printf '%s\n' "$keep" | grep -qxF "$self" \
	|| fail "$self is the database this worktree runs against and the keep set does not hold it — the enumeration is broken"

echo "db-prune: $total databases on this server, $found worktrees tracked, $live of them still on disk"

# `garam_test%` is wider than what the naming creates: only `garam_test`
# and `garam_test_<worktree>` come from the Makefile's TEST_DB_NAME,
# and a third name matching that pattern was created by something else
# and is left alone rather than swept up by the width of the filter.
kept=''
doomed=''
foreign=''
while IFS= read -r db; do
	case $db in
	garam_test) kept="$kept$db
" ;;
	garam_test_*)
		if printf '%s\n' "$keep" | grep -qxF "$db"; then
			kept="$kept$db
"
		else
			doomed="$doomed$db
"
		fi
		;;
	garam_test*) foreign="$foreign$db
" ;;
	esac
done <<EOF
$all
EOF

nkept="$(printf '%s' "$kept" | grep -c . || true)"
ndoomed="$(printf '%s' "$doomed" | grep -c . || true)"
nforeign="$(printf '%s' "$foreign" | grep -c . || true)"

echo "db-prune: $((nkept + ndoomed + nforeign)) of them named garam_test or garam_test_<worktree> — $nkept kept, $ndoomed to drop, $nforeign matching the pattern and created by nothing here"
test "$nkept" -eq 0 || { echo 'db-prune: keeps'; printf '%s' "$kept" | sed 's/^/  /'; }
test "$nforeign" -eq 0 || { echo 'db-prune: leaves alone'; printf '%s' "$foreign" | sed 's/^/  /'; }

# `docker compose exec` passes stdin to the container even under -T, so
# a drop left to inherit it eats the rest of this heredoc and the loop
# ends after one database — reporting a drop of 1 that reads like a
# server holding one. It is handed /dev/null instead, and the count
# below is what would catch it again.
dropped=0
failed=''
while IFS= read -r db; do
	test -n "$db" || continue
	if $COMPOSE_LOCAL exec -T postgres dropdb -U garam "$db" </dev/null; then
		dropped=$((dropped + 1))
		echo "  dropped $db"
	else
		failed="$failed$db
"
	fi
done <<EOF
$doomed
EOF

echo "db-prune: dropped $dropped of $ndoomed"
test "$((dropped + $(printf '%s' "$failed" | grep -c . || true)))" -eq "$ndoomed" \
	|| fail "the loop attempted $dropped of $ndoomed drops and then ended — what is left is unknown rather than nothing"
test -z "$failed" || {
	echo "db-prune: $(printf '%s' "$failed" | grep -c .) refused to drop — a database with a connection open is one:"
	printf '%s' "$failed" | sed 's/^/  /'
	exit 1
}
