#!/bin/sh
# Checks each Markdown file's "## Contents" list against its own "##" headings, per
# docs/convention/documentation.md §Format. That rule governs every Markdown document in
# this repository, so the set is selected by $scope rather than by a chosen directory, and
# the count is printed with $scope so the reported coverage stays the coverage read.
set -euf

scope="git ls-files --cached --others --exclude-standard -- *.md"

cd "$(dirname "$0")"

count=0
status=0
while IFS= read -r file; do
	[ -n "$file" ] || continue
	count=$((count + 1))
	problems=$(awk -v file="$file" '
		/^```/ { fence = 1 - fence; next }
		fence { next }
		/^## / {
			title = substr($0, 4)
			sub(/[ \t]+$/, "", title)
			if (title == "Contents") {
				contents = 1
				collecting = 1
				if (headings == 0) opens = 1
			} else {
				heading[++headings] = title
				collecting = 0
			}
			next
		}
		collecting && /^- / { entry[++entries] = substr($0, 3) }
		END {
			if (contents) {
				if (headings <= 3)
					print file ": Contents list with " headings " headings; three or fewer carries none"
				if (!opens)
					print file ": Contents list is not the first ## heading"
				if (entries != headings)
					print file ": Contents list indexes " entries " of " headings " headings"
				shorter = entries < headings ? entries : headings
				for (i = 1; i <= shorter; i++)
					if (entry[i] != heading[i])
						print file ": Contents entry " i " reads \"" entry[i] "\", heading " i " reads \"" heading[i] "\""
			} else if (headings > 8) {
				print file ": " headings " headings and no Contents list; more than eight opens with one"
			}
		}
	' "$file")
	if [ -n "$problems" ]; then
		printf '%s\n' "$problems"
		status=1
	fi
done <<EOF
$($scope)
EOF

echo "checked $count Markdown files, selected by: $scope"
if [ "$count" -eq 0 ]; then
	echo "no Markdown file was read" >&2
	exit 1
fi
if [ "$status" -ne 0 ]; then
	echo "rule: docs/convention/documentation.md §Format" >&2
fi
exit "$status"
