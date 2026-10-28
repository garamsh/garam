#!/bin/sh
# template-diff reports where every path the conventions template tracks
# stands here — the question #294 wants answered by a run, not a memory.
# The path set is the template's own tree, and a path's base is the newest
# template commit whose blob equals the one HEAD holds. No such commit
# means the content was never the template's — a local adaptation, or a
# file the two repositories only share the name of — and no revision
# describes that, so the path is measured against TEMPLATE_SYNCED
# instead: the template commits touching it since then are the ones
# nothing here shows this repository received (#775). A path absent here
# is reported rather than skipped, which is how a directory the template
# grows arrives without an edit to this file.
#
# TEMPLATE_SYNCED arrives in the environment; the Makefile holds it.

git fetch template --quiet || exit
git merge-base --is-ancestor "$TEMPLATE_SYNCED" template/main \
	|| { echo "template-diff: TEMPLATE_SYNCED $TEMPLATE_SYNCED is not in template/main history — the value is wrong, or the template rewrote what it named"; exit 1; }
paths="$(git ls-tree -r --name-only template/main)"
test -n "$paths" || { echo 'template-diff: template/main tracks no path — the tree listing is broken'; exit 1; }
synced="$(git rev-parse --short "$TEMPLATE_SYNCED")"; adapted=0; stale=0
echo "template-diff: $(printf '%s\n' "$paths" | wc -l) paths tracked by template/main, adapted paths measured against $synced"
for p in $paths; do
	hist="$(git log --format='%H' --no-renames --raw --abbrev=40 template/main -- "$p")"
	test "$(printf '%s\n' "$hist" | awk 'NF>1{print $4; exit}')" = "$(git rev-parse "template/main:$p")" \
		|| { echo "template-diff: $p: the newest commit touching it does not carry the blob template/main holds — the history parse below is reading the wrong field"; exit 1; }
	ours="$(git rev-parse -q --verify "HEAD:$p")" || { printf '  absent here  %s\n' "$p"; continue; }
	base="$(printf '%s\n' "$hist" | awk -v ours="$ours" 'NF==1{c=$0} $4==ours{print c; exit}')"
	if test -z "$base"; then
		adapted=$((adapted+1))
		since="$(git log --oneline --no-decorate "$TEMPLATE_SYNCED..template/main" -- "$p")"
		test -n "$since" || { printf '  adapted      %s  0 template commits since %s\n' "$p" "$synced"; continue; }
		stale=$((stale+1))
		printf '  adapted      %s  %s template commits since %s\n' "$p" "$(printf '%s\n' "$since" | wc -l)" "$synced"
		printf '%s\n' "$since" | sed 's/^/                 /'
		continue
	fi
	since="$(git log --oneline --no-decorate "$base..template/main" -- "$p")"
	test -n "$since" || { printf '  current      %s  %s\n' "$p" "$(git rev-parse --short "$base")"; continue; }
	printf '  behind       %s  ours matches %s\n' "$p" "$(git rev-parse --short "$base")"
	printf '%s\n' "$since" | sed 's/^/                 /'
done
echo "template-diff: $stale of $adapted adapted paths carry a template commit since $synced"
