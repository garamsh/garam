#!/bin/sh
# check-line-width counts the hard-wrap violations two rules define,
# and takes a file's rule from the file: a *.md path is measured
# against `documentation.md` §Format and every other path against
# `code-comments.md` §Line width, both under docs/convention/. Neither
# rule's count is the lines past column 72. Each exempts a line whose
# overflow cannot be broken without becoming something else, so a plain
# column count reports violations in a file that has none (#999). The
# two answers diverge exactly where the exception applies, which is
# where a reviewer looks, so the summary below prints both.
#
# A line is a violation when the token straddling column 72 would fit
# inside 72 on a line of its own at that line's own indent: a break was
# available and was not taken. Where that token would not fit there, no
# break shortens the line, and the line is exempt when the token is a
# kind its own rule's exception list names. A code span is one token
# however many spaces it holds, because a break inside a verbatim
# quotation makes it another quotation; every other kind either list
# names already is one by holding no space. A fenced block, a heading
# and a table row are exempt as shapes the document rule does not
# reach.
#
# The two exception lists are not the same list (#1058), so one line
# can be exempt in a document and a violation in a code file:
#
#   token straddling 72   document    code
#   URL                   exempt      exempt
#   path                  exempt      exempt
#   quotation             exempt      exempt
#   identifier            violation   exempt
#   commit title          exempt      violation
#
# A quotation is a code span; a commit title is the one this project
# can tell apart from a command or a message, by the `<type>: ` form
# `git.md` §Commits gives it.
#
# A document is measured on every line. A code file is measured on its
# comment lines, and the marker comes from the language: `//` in Go,
# TypeScript and proto, `#` in shell, YAML, the Makefile and a
# Dockerfile, `--` in SQL. A comment sharing a line with code is not
# reached, and neither is a language holding none of the three markers.
# The run names what it did not reach rather than counting it as clean.
#
# Generated output is outside docs/convention/README.md §Scope, so the
# code list drops every file carrying Go's "Code generated ... DO NOT
# EDIT." marker, which is the set `check-gen` owns.
#
# Counted in characters. LC_ALL=C fixes awk to bytes in every
# implementation and the count then subtracts UTF-8 continuation bytes,
# because mawk's own length() is bytes and reads a line of 72
# characters carrying an em dash as 74. A tab is one character, which
# is what both rules call a column, so it is one column here and
# nothing in either rule makes it eight. No other character in this
# corpus is wider than one column.
#
# Every run first classifies lines whose verdicts are known, so a zero
# here is readable as a zero and not as a query that matched nothing.
# Two pin the document boundary one column apart: breakable prose at 73
# characters, which must be reported, and a line at 72 characters and
# 74 bytes, which must not — the second fails the moment anything here
# counts bytes, and the Go control repeats it because that is the line
# #1056 counted wrong. Two more are carried past 72 by a link and by a
# code span and must come back exempt. Two pairs pin the asymmetry: one
# commit title is exempt in the document and a violation in Go, one
# identifier is the reverse. Three lines carry no comment marker and
# must produce no verdict at all, which is what says the code rule read
# comments rather than lines. The run fails if any of them classifies
# otherwise.
#
# Nothing runs this. #975 declined a width gate with its own
# measurement and named what would change that; a helper a reviewer
# runs is not that gate, and `make lint` reads this file as a shell
# script rather than as a check.
set -euf

export LC_ALL=C

scope="git ls-files --cached --others --exclude-standard"

cd "$(dirname "$0")/.."

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# One definition of what marks a comment, used to select the files and
# again to read them, so the sweep and the count cannot disagree about
# a language.
markers='
function marker(f,   b) {
	b = f
	sub(/^.*\//, "", b)
	if (f ~ /\.(go|ts|tsx|mts|mjs|proto)$/) return "//"
	if (f ~ /\.sql$/) return "--"
	if (f ~ /\.(sh|yaml|yml)$/) return "#"
	if (b == "Makefile" || b ~ /^Dockerfile(\..+)?$/) return "#"
	return ""
}
function rule_of(f) {
	if (f ~ /\.md$/) return "doc"
	return marker(f) == "" ? "-" : "code"
}
'

classify="$markers"'
function crossing(m, start,   i, n, c, j, ts) {
	n = length(m)
	i = start + 1
	while (i <= n) {
		if (substr(m, i, 1) == " ") { i++; continue }
		ts = i
		while (i <= n) {
			c = substr(m, i, 1)
			if (c == " ") break
			if (c == "`") {
				j = index(substr(m, i + 1), "`")
				if (j > 0) { i = i + j + 1; continue }
			}
			i++
		}
		if (i - 1 > 72) { tok = substr(m, ts, i - ts); return i - ts }
	}
	tok = ""
	return 0
}
function kind_of(t,   q) {
	if (t ~ /`/) {
		q = t
		sub(/^[^`]*`/, "", q)
		sub(/`[^`]*$/, "", q)
		return q ~ /^(feat|fix|chore|docs|refactor|test): / \
			? "commit title" : "quotation"
	}
	if (t ~ /:\/\// || t ~ /^www\./) return "url"
	if (t ~ /\//) return "path"
	if (t ~ /_/ || t ~ /[a-z][A-Z]/ ||
		t ~ /[A-Za-z0-9][A-Za-z0-9]\.[A-Za-z_][A-Za-z_]/) return "identifier"
	return "prose"
}
function emit(verdict) {
	print chars "\t" bytes "\t" verdict "\t" FILENAME "\t" FNR "\t" kind \
		"\t" rule
}
BEGIN {
	exempt["doc", "url"] = 1
	exempt["doc", "path"] = 1
	exempt["doc", "quotation"] = 1
	exempt["doc", "commit title"] = 1
	exempt["code", "url"] = 1
	exempt["code", "path"] = 1
	exempt["code", "quotation"] = 1
	exempt["code", "identifier"] = 1
}
FNR == 1 {
	fence = 0
	rule = rule_of(FILENAME)
	mark = marker(FILENAME)
	if (mark == "//") opener = "^[ \t]*//+[ \t]*"
	else if (mark == "--") opener = "^[ \t]*--+[ \t]*"
	else opener = "^[ \t]*#+[ \t]*"
}
{
	m = $0
	gsub(/[\200-\277]/, "", m)
	chars = length(m)
	bytes = length($0)
	kind = ""
	if (rule == "code") {
		if (!match(m, opener)) next
		start = RLENGTH
		if (chars <= 72) { emit("within"); next }
	} else {
		if (m ~ /^[ \t]*```/) {
			fence = 1 - fence
			emit(chars > 72 ? "fenced" : "within")
			next
		}
		if (fence) { emit(chars > 72 ? "fenced" : "within"); next }
		if (chars <= 72) { emit("within"); next }
		if (m ~ /^[ \t]*#+ /) { emit("heading"); next }
		if (m ~ /^[ \t]*\|/) { emit("table"); next }
		if (match(m, /^[ \t]*([-*+]|[0-9]+[.)]) +/)) start = RLENGTH
		else if (match(m, /^[ \t]*> ?/)) start = RLENGTH
		else { match(m, /^[ \t]*/); start = RLENGTH }
	}
	width = crossing(m, start)
	if (width == 0) { emit("unbreakable"); next }
	kind = kind_of(tok)
	if (start + width <= 72) { emit("violation"); next }
	emit((rule, kind) in exempt ? "unbreakable" : "violation")
}
'

cat >"$work/control.md" <<'CONTROL'
control: ordinary prose, every word of it breakable, sitting at column 73
- [0000](./adr/0000-a-name-long-enough-to-carry-one-line-past-column-72.md)
  `docs: a commit title quoted verbatim that no break shortens without making it another title`
control — this line is 72 characters, and one em dash makes it 74 bytes.
TestCheckLineWidth_ExemptsAnIdentifierInCodeAndReportsTheSameOneInADocument
CONTROL
cat >"$work/control.go" <<'CONTROL'
// control — 72 characters and 74 bytes, the exact line #1056 got wrong.
// TestCheckLineWidth_ExemptsAnIdentifierInCodeAndReportsTheSameOneInADocument
// `docs: a commit title quoted verbatim that no break shortens without making it another title`
var notAComment = "this line of code runs past seventy-two characters, and is not a comment"
// control: ordinary prose, every word of it breakable, sitting at column 76
CONTROL
# The shell control's marker arrives as a variable. Written literally
# it would be a comment line of this file, past 72 columns, and the run
# would report its own control data as a violation of the rule that
# control exists to pin.
hash='#'
cat >"$work/control.sh" <<CONTROL
$hash control: ordinary prose, every word of it breakable, sitting at column 75
garam_release="a shell line past seventy-two characters that is not a comment"
CONTROL
cat >"$work/control.sql" <<'CONTROL'
-- control: ordinary prose, every word of it breakable, sitting at column 76
SELECT a_column_name_long_enough_to_carry_this_line_past_column_72 FROM a_table;
CONTROL

verdicts="$(awk "$classify" "$work/control.md" "$work/control.go" \
	"$work/control.sh" "$work/control.sql" | sed "s|$work/||" | cut -f1-6)"
expected="$(printf '%s\n' \
	'73	73	violation	control.md	1	prose' \
	'75	75	unbreakable	control.md	2	path' \
	'95	95	unbreakable	control.md	3	commit title' \
	'72	74	within	control.md	4	' \
	'75	75	violation	control.md	5	identifier' \
	'72	74	within	control.go	1	' \
	'78	78	unbreakable	control.go	2	identifier' \
	'96	96	violation	control.go	3	commit title' \
	'76	76	violation	control.go	5	prose' \
	'75	75	violation	control.sh	1	prose' \
	'76	76	violation	control.sql	1	prose')"
if [ "$verdicts" != "$expected" ]; then
	echo 'check-line-width: the control did not classify as it must, so no count below is readable' >&2
	printf 'expected:\n%s\ngot:\n%s\n' "$expected" "$verdicts" >&2
	exit 1
fi
echo 'check-line-width: control passed — 73 characters of breakable prose flagged, 72 characters at 74 bytes not, a link and a code span exempt, an identifier exempt in Go and reported in Markdown, a commit title the reverse, three lines carrying no marker unread'

$scope >"$work/all"
test -s "$work/all" || {
	echo "check-line-width: no file was read, selected by: $scope" >&2
	exit 1
}
awk "$markers"'{ print rule_of($0) "\t" $0 }' "$work/all" >"$work/tagged"
awk -F'\t' '$1 == "doc" { print $2 }' "$work/tagged" >"$work/documents"
awk -F'\t' '$1 == "code" { print $2 }' "$work/tagged" >"$work/authored"
awk -F'\t' '$1 == "-" { print $2 }' "$work/tagged" >"$work/unreached"

# The generated set is read the way check-gen reads it. An empty answer
# means that scan stopped matching, not that the tree holds no
# generated output, and it would silently widen the count below.
git grep -I -l --untracked -E '^// Code generated .* DO NOT EDIT' \
	>"$work/generated" || true
test -s "$work/generated" || {
	echo 'check-line-width: no generated file was found — the marker scan is broken, and generated output would be counted as authored' >&2
	exit 1
}
grep -vxF -f "$work/generated" "$work/authored" >"$work/code" || true
test -s "$work/documents" && test -s "$work/code" || {
	echo "check-line-width: one of the two lists came back empty, selected by: $scope" >&2
	exit 1
}

echo "check-line-width: $(wc -l <"$work/documents") Markdown files and $(wc -l <"$work/code") code files, selected by: $scope, less $(wc -l <"$work/generated") generated"
if [ -s "$work/unreached" ]; then
	echo "check-line-width: not measured — $(wc -l <"$work/unreached") files whose language names none of the three markers: $(sed 's|.*/||; s|^.*\.|.|' "$work/unreached" | sort | uniq -c | sort -rn | awk '{ printf "%s%s %s", (NR > 1 ? ", " : ""), $2, $1 } END { print "" }')"
fi

cat "$work/documents" "$work/code" | xargs awk "$classify" | awk -F'\t' '
BEGIN {
	label["doc"] = "docs/convention/documentation.md §Format"
	label["code"] = "docs/convention/code-comments.md §Line width"
	order[1] = "doc"
	order[2] = "code"
}
$3 != "within" {
	if (!($4 in over)) { name[++seen] = $4; owner[$4] = $7 }
	over[$4]++
	total[$7]++
	if ($3 == "violation") {
		violations[$7]++
		viol[$4]++
		detail[$4] = detail[$4] "  " $4 ":" $5 ": " $1 " characters, " $6 "\n"
	}
}
$2 > 72 { bytes[$7]++ }
END {
	for (i = 1; i <= seen; i++) {
		f = name[i]
		printf "%s: %d over 72 characters, %d exempt, %d violations\n", f, over[f], over[f] - viol[f], viol[f] + 0
		if (viol[f]) {
			printf "%s", detail[f]
			dirty[owner[f]]++
		}
	}
	bad = 0
	for (i = 1; i <= 2; i++) {
		r = order[i]
		printf "check-line-width: %s — %d lines over 72 bytes, %d over 72 characters, %d exempt, %d violations in %d files\n", \
			label[r], bytes[r] + 0, total[r] + 0, total[r] - violations[r], violations[r] + 0, dirty[r] + 0
		bad += violations[r]
	}
	if (bad) exit 1
}
'
