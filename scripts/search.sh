#!/bin/sh
# search finds a phrase this repository hard-wraps. A phrase that
# straddles a wrap is on no single line, so grep, git grep and any
# `[^.]{0,N}` span report zero hits, and a reader reads that as absence
# (#624). One flag does not close it: `grep -z` makes newline an
# ordinary character, but the continuation line still opens with its
# comment marker, so the joined text reads `so no file in # this stack`
# and the phrase is still not there. Each file is joined into one line
# with leading whitespace and one run of `#`, `//` or `>` dropped; a
# blank line joins as the double space that keeps two paragraphs apart.
#
# The corpus is every tracked file git itself calls text, read from the
# working tree — an untracked file is outside what a run covers. A
# corpus that came back a subset reports a smaller zero and reads like
# the whole sweep, so it is not trusted on its own word: git's diff
# machinery names the tracked paths it counts lines for, and a corpus
# that is not exactly that set fails the run.
#
# A run that found nothing and a run that searched nothing read alike,
# so every run first finds a phrase that is there. The control is
# derived rather than written down — the first phrase spanning a line
# break that a line-based git grep cannot find — so it cannot go stale,
# and that failed grep is the contrast: it is the join the control
# proves, not that the machinery ran.
#
# No hits is an answer, so the target prints the count and exits 0.
# What would make that zero a lie fails instead: an empty corpus, no
# control candidate, or a control the join cannot find.
#
# SEARCH_Q arrives in the environment; the Makefile holds it.

# One program holds the join and the control candidates: the same
# stripping decides both, and a second copy of it would drift.
SEARCH_AWK='
function head3(s,   n, a) {
	n = split(s, a, " ")
	return n < 3 ? "" : a[1] " " a[2] " " a[3]
}
function tail3(s,   n, a) {
	n = split(s, a, " ")
	return n < 3 ? "" : a[n-2] " " a[n-1] " " a[n]
}
function lineat(pos,   i) {
	for (i = nlines; i > 1; i--) if (start[i] <= pos) return line[i]
	return line[1]
}
function excerpt(pos,   from) {
	from = pos - 24 < 1 ? 1 : pos - 24
	return substr(text, from, qlen + 48)
}
function report(   low, p, hit, at, i, before, after) {
	if (nlines == 0) return
	if (mode == "candidates") {
		if (ncand >= 50) return
		for (i = 2; i <= nlines; i++) {
			if (raw[i-1] ~ /[.:;,]$/) continue
			before = tail3(raw[i-1]); after = head3(raw[i])
			if (before == "" || after == "") continue
			printf "%s:%d:%s %s\n", path, line[i-1], before, after
			if (++ncand >= 50) exit
		}
		return
	}
	low = tolower(text)
	for (p = 0; (hit = index(substr(low, p + 1), query)) > 0; p = at + qlen - 1) {
		at = p + hit
		printf "%s:%d: %s\n", path, lineat(at), excerpt(at)
	}
}
BEGIN {
	mode = ENVIRON["SEARCH_MODE"]
	query = tolower(ENVIRON["SEARCH_Q"])
	gsub(/[ \t]+/, " ", query); sub(/^ /, "", query); sub(/ $/, "", query)
	qlen = length(query)
	if (mode != "candidates" && qlen == 0) { print "search: the phrase normalised to nothing" > "/dev/stderr"; exit 2 }
}
FNR == 1 { report(); path = FILENAME; text = ""; nlines = 0 }
{
	s = $0
	sub(/^[ \t]*/, "", s)
	sub(/^(\/\/+|#+|>+)[ \t]*/, "", s)
	gsub(/[ \t]+/, " ", s); sub(/ $/, "", s)
	nlines++
	start[nlines] = nlines == 1 ? 1 : length(text) + 2
	line[nlines] = FNR
	raw[nlines] = s
	text = nlines == 1 ? s : text " " s
}
END { report() }
'

test -n "$(printf '%s' "$SEARCH_Q" | tr -d '[:space:]')" \
	|| { echo 'search: no phrase — usage: make search Q="the phrase to look for"'; exit 1; }
set -e
out="$(mktemp -d)"; trap 'rm -rf "$out"' EXIT
git diff --numstat "$(git hash-object -t tree /dev/null)" \
	| awk '$1 != "-" && $1 > 0 { print $3 }' | sort > "$out/tracked"
rc=0; git grep -I -l -e '' > "$out/listed" || rc=$?
if test "$rc" -gt 1; then echo "search: git grep exited $rc listing the corpus — that is an error, not an empty repository"; exit 1; fi
sort "$out/listed" > "$out/corpus"
test -s "$out/corpus" || { echo 'search: no tracked text file — the corpus collection is broken'; exit 1; }
diff "$out/tracked" "$out/corpus" \
	|| { echo 'search: < tracked and holding lines, > read — the corpus is not the repository'; exit 1; }
files="$(cat "$out/corpus")"
echo "search: $(wc -l < "$out/corpus") tracked text files, $(cat $files | wc -l) lines, each joined into one line"
SEARCH_MODE=candidates awk "$SEARCH_AWK" $files > "$out/candidates"
test -s "$out/candidates" \
	|| { echo 'search: no phrase in the corpus spans a line break — the join read nothing'; exit 1; }
control=''; site=''
while IFS= read -r c; do
	phrase="${c#*:*:}"; rc=0
	git grep -I -F -i -q -e "$phrase" || rc=$?
	if test "$rc" -eq 0; then continue; fi
	if test "$rc" -ne 1; then echo "search: git grep exited $rc on a control candidate — that is an error, not a clean negative"; exit 1; fi
	control="$phrase"; site="$(printf '%s' "$c" | cut -d: -f1,2)"; break
done < "$out/candidates"
test -n "$control" \
	|| { echo "search: every one of $(wc -l < "$out/candidates") phrases spanning a line break is on a line too — the contrast the control rests on is gone"; exit 1; }
found="$(SEARCH_Q="$control" awk "$SEARCH_AWK" $files | wc -l)"
test "$found" -gt 0 \
	|| { echo "search: control \"$control\" spans the break at $site and the join did not find it — the join is broken"; exit 1; }
echo "search: control \"$control\" — $site, 0 hits line-based, $found joined"
awk "$SEARCH_AWK" $files > "$out/hits"
sed 's/^/  /' "$out/hits"
echo "search: $(wc -l < "$out/hits") hits for \"$SEARCH_Q\""
