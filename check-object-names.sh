#!/bin/sh
# Fails on a Markdown file citing a commit object by fewer than its full 40 characters, per
# docs/convention/documentation.md §Keep it current. That rule binds a citation when it is written
# and a merged ADR body is frozen, so $scope takes every Markdown document except those under
# docs/architecture/adr/ other than its template, and the count is printed with $scope.
# $scope runs through eval, as a shell runs it when pasted, so the text printed is the text run.
# A token is a maximal run of letters and digits, with runs joined by `_` or `-` read as one; one
# of 7 to 39 lowercase hex characters with both a digit and a letter is an abbreviated object name.
# All-digit runs are numbers, all-letter runs words, and joined runs identifiers, not names.
set -euf

scope="git ls-files --cached --others --exclude-standard -- '*.md' ':(exclude)docs/architecture/adr/*' && git ls-files --cached --others --exclude-standard -- docs/architecture/adr/0000-template.md"

cd "$(dirname "$0")"

count=0
status=0
while IFS= read -r file; do
	[ -n "$file" ] || continue
	count=$((count + 1))
	problems=$(awk -v file="$file" '
		/^```/ { fence = 1 - fence; next }
		fence { next }
		{
			rest = $0
			while (match(rest, /[0-9A-Za-z]+([_-][0-9A-Za-z]+)*/)) {
				token = substr(rest, RSTART, RLENGTH)
				rest = substr(rest, RSTART + RLENGTH)
				if (token ~ /^[0-9a-f]+$/ && token ~ /[0-9]/ && token ~ /[a-f]/ && length(token) >= 7 && length(token) <= 39)
					print file ":" NR ": \"" token "\" is " length(token) " hex characters; an object name is cited by all 40"
			}
		}
	' "$file")
	if [ -n "$problems" ]; then
		printf '%s\n' "$problems"
		status=1
	fi
done <<EOF
$(eval "$scope")
EOF

echo "checked $count Markdown files, selected by: $scope"
if [ "$count" -eq 0 ]; then
	echo "no Markdown file was read" >&2
	exit 1
fi
if [ "$status" -ne 0 ]; then
	echo "rule: docs/convention/documentation.md §Keep it current, \"A commit is cited by its full object name\"" >&2
fi
exit "$status"
