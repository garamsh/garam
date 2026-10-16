#!/bin/sh
# Fails on a Markdown file where one heading is a prefix of another heading in the same file, per
# docs/convention/documentation.md §Keep it current. That rule governs every Markdown document in
# this repository, so the set is selected by $scope and the count is printed with $scope. $scope
# runs through eval, as a shell runs it when pasted, so the text printed is the text run.
# A heading is read as check-section-citations.sh reads one: a line outside a fence opening with
# "#" and a blank, its text trimmed. Two equal headings count as a pair, since each prefixes the other.
set -euf

scope="git ls-files --cached --others --exclude-standard -- '*.md'"

cd "$(dirname "$0")"

status=0
eval "$scope" | awk -v scope="$scope" '
	function check(file,    line, r, lineno, fence, title, n, i) {
		fence = 0
		lineno = 0
		n = 0
		while ((r = (getline line < file)) > 0) {
			lineno++
			if (line ~ /^```/) { fence = 1 - fence; continue }
			if (fence || line !~ /^#+[ \t]/) continue
			title = line
			sub(/^#+[ \t]+/, "", title)
			sub(/[ \t]+$/, "", title)
			if (title == "") continue
			headings++
			for (i = 1; i <= n; i++) {
				if (substr(title, 1, length(heading[i])) == heading[i])
					report(file, heading[i], at[i], title, lineno)
				else if (substr(heading[i], 1, length(title)) == title)
					report(file, title, lineno, heading[i], at[i])
			}
			heading[++n] = title
			at[n] = lineno
		}
		if (r < 0) { print file ": cannot be read" > "/dev/stderr"; exit 3 }
		close(file)
	}
	function report(file, shorter, s, longer, l) {
		prefixed = 1
		print file ":" s ": heading \"" shorter "\" is a prefix of heading \"" longer "\" at line " l
	}
	NF { path[++files] = $0 }
	END {
		for (i = 1; i <= files; i++) check(path[i])
		print "checked " headings + 0 " headings in " files + 0 " Markdown files, selected by: " scope
		if (files == 0 || headings == 0) exit 2
		exit prefixed
	}
' || status=$?

if [ "$status" -eq 2 ]; then
	echo "no Markdown file or no heading was read" >&2
	exit 1
fi
if [ "$status" -eq 1 ]; then
	echo "rule: docs/convention/documentation.md §Keep it current, \"No heading in a file is a prefix of another heading in the same file\"" >&2
fi
exit "$status"
