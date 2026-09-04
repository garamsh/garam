#!/bin/sh
# check-line-width counts the hard-wrap violations
# docs/convention/documentation.md §Format defines, which is not the
# same number as the lines past column 72. That rule exempts a line
# whose overflow cannot be broken without becoming something else, so a
# plain column count reports violations in a file that has none (#999).
# The two answers diverge exactly where the exception applies, which is
# where a reviewer looks, so the summary below prints both.
#
# A line is a violation when the token straddling column 72 would fit
# inside 72 on a line of its own at that line's own indent: a break was
# available and was not taken. Where that token would not fit there, no
# break shortens the line and the exception covers it. A code span and
# a Markdown link destination are each one token, because a break
# inside either makes it something else. A fenced block, a heading and
# a table row are exempt as shapes the rule does not reach.
#
# Counted in characters. LC_ALL=C fixes awk to bytes in every
# implementation and the count then subtracts UTF-8 continuation bytes,
# because mawk's own length() is bytes and reads a line of 72
# characters carrying an em dash as 74. Characters are columns for this
# corpus: it holds no tab and no character wider than one column.
#
# Every run first classifies four lines whose verdicts are known, so a
# zero here is readable as a zero and not as a query that matched
# nothing: one ordinary violation, one carried past 72 by a link and
# one by a code span, and one sitting at 72 characters and 74 bytes
# that must not be reported at all. The run fails if any of the four
# comes back otherwise.
#
# Nothing runs this. #975 declined a width gate with its own
# measurement and named what would change that; a helper a reviewer
# runs is not that gate, and `make lint` reads this file as a shell
# script rather than as a check.
set -euf

export LC_ALL=C

scope="git ls-files --cached --others --exclude-standard -- *.md"

cd "$(dirname "$0")/.."

classify='
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
			if (c == "]" && substr(m, i + 1, 1) == "(") {
				j = index(substr(m, i + 2), ")")
				if (j > 0) { i = i + j + 2; continue }
			}
			i++
		}
		if (i - 1 > 72) return i - ts
	}
	return 0
}
function emit(verdict) {
	print chars "\t" bytes "\t" verdict "\t" FILENAME "\t" FNR
}
FNR == 1 { fence = 0 }
{
	m = $0
	gsub(/[\200-\277]/, "", m)
	chars = length(m)
	bytes = length($0)
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
	width = crossing(m, start)
	emit(width > 0 && start + width <= 72 ? "violation" : "unbreakable")
}
'

verdicts="$(awk "$classify" <<'CONTROL' | cut -f1,2,3,5
this control line is ordinary prose, every word of it breakable, and it runs past column 72
- [0000](./adr/0000-a-name-long-enough-to-carry-one-line-past-column-72.md)
  `docs: a commit title quoted verbatim that no break shortens without making it another title`
control — this line is 72 characters, and one em dash makes it 74 bytes.
CONTROL
)"
expected="$(printf '%s\n' \
	'91	91	violation	1' \
	'75	75	unbreakable	2' \
	'95	95	unbreakable	3' \
	'72	74	within	4')"
if [ "$verdicts" != "$expected" ]; then
	echo 'check-line-width: the control did not classify as it must, so no count below is readable' >&2
	printf 'expected:\n%s\ngot:\n%s\n' "$expected" "$verdicts" >&2
	exit 1
fi
echo 'check-line-width: control passed — a breakable line flagged, a link and a code span exempt, and 72 characters at 74 bytes not reported'

files="$($scope)"
if [ -z "$files" ]; then
	echo "check-line-width: no Markdown file was read, selected by: $scope" >&2
	exit 1
fi
count="$(printf '%s\n' "$files" | wc -l)"
echo "check-line-width: $count Markdown files, selected by: $scope"

printf '%s\n' "$files" | xargs awk "$classify" | awk -F'\t' '
$3 != "within" {
	if (!($4 in over)) name[++seen] = $4
	over[$4]++
	total++
	if ($3 == "violation") {
		violations++
		viol[$4]++
		detail[$4] = detail[$4] "  " $4 ":" $5 ": " $1 " characters\n"
	}
}
$2 > 72 { bytes++ }
END {
	for (i = 1; i <= seen; i++) {
		f = name[i]
		printf "%s: %d over 72 characters, %d exempt, %d violations\n", f, over[f], over[f] - viol[f], viol[f] + 0
		if (viol[f]) {
			printf "%s", detail[f]
			dirty++
		}
	}
	printf "check-line-width: %d lines over 72 bytes, %d over 72 characters, %d exempt, %d violations in %d files\n", \
		bytes + 0, total + 0, total - violations, violations + 0, dirty + 0
	if (violations) exit 1
}
' || {
	echo 'rule: docs/convention/documentation.md §Format' >&2
	exit 1
}
