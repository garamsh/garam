#!/bin/sh
# Fails on a Markdown file citing a §Heading that resolves to no heading: a claim nobody can point
# at, per docs/convention/documentation.md §Keep it current, "Every claim about this repository must
# be verifiable in it". That rule governs every Markdown document in this repository, so the set is
# selected by $scope and the count is printed with $scope. $scope runs through eval, as a shell
# runs it when pasted, so the text printed is the text run.
# A § outside a fence and outside a code span, not followed by a digit, is a reference. It resolves
# where a heading of the citing file, of a file named in a code span on its line, or of every file
# in a folder so named, is a prefix of the text after the §. A name resolves to each selected path
# equal to it or ending in "/" and it, so a bare basename such as `review.md` matches every file of
# that basename; a folder name ends in "/" and matches the files directly inside it.
# A shorter heading that prefixes a longer one resolves citations of the longer, so the check finds
# a citation matching nothing, not one matching the wrong heading. check-heading-prefixes.sh keeps
# such a pair out, per docs/convention/documentation.md §Keep it current,
# "No heading in a file is a prefix of another heading in the same file".
set -euf

scope="git ls-files --cached --others --exclude-standard -- '*.md'"

cd "$(dirname "$0")"

status=0
eval "$scope" | awk -v scope="$scope" '
	function read_headings(file,    line, r, fence, title) {
		fence = 0
		while ((r = (getline line < file)) > 0) {
			if (line ~ /^```/) { fence = 1 - fence; continue }
			if (fence || line !~ /^#+[ \t]/) continue
			title = line
			sub(/^#+[ \t]+/, "", title)
			sub(/[ \t]+$/, "", title)
			heading[file, ++headings[file]] = title
		}
		if (r < 0) { print file ": cannot be read" > "/dev/stderr"; exit 3 }
		close(file)
	}
	function carries(file, text,    i, h) {
		for (i = 1; i <= headings[file]; i++) {
			h = heading[file, i]
			if (h != "" && substr(text, 1, length(h)) == h) return 1
		}
		return 0
	}
	function folder_carries(folder, text,    i, dir, found) {
		found = 0
		for (i = 1; i <= files; i++) {
			dir = path[i]
			if (dir !~ /\//) dir = ""
			sub(/\/[^\/]*$/, "", dir)
			if (dir != folder && substr(dir, length(dir) - length(folder)) != "/" folder) continue
			if (!carries(path[i], text)) return 0
			found = 1
		}
		return found
	}
	function name_carries(name, text,    i) {
		for (i = 1; i <= files; i++)
			if ((path[i] == name || substr(path[i], length(path[i]) - length(name)) == "/" name) && carries(path[i], text))
				return 1
		return 0
	}
	function resolves(file, text, names, n,    i) {
		if (carries(file, text)) return 1
		for (i = 1; i <= n; i++) {
			if (names[i] ~ /\/$/) {
				if (folder_carries(substr(names[i], 1, length(names[i]) - 1), text)) return 1
			} else if (name_carries(names[i], text)) return 1
		}
		return 0
	}
	function check(file,    line, r, fence, seg, segs, names, name, n, i, p, offset, rest, at, text) {
		fence = 0
		lineno = 0
		while ((r = (getline line < file)) > 0) {
			lineno++
			if (line ~ /^```/) { fence = 1 - fence; continue }
			if (fence || index(line, sect) == 0) continue
			segs = split(line, seg, "`")
			n = 0
			for (i = 2; i < segs; i += 2) {
				name = seg[i]
				sub(/^\.\//, "", name)
				if (name ~ /\.md$/ || name ~ /\/$/) names[++n] = name
			}
			offset = 0
			for (i = 1; i <= segs; i++) {
				if (i % 2 == 1 || i == segs) {
					rest = seg[i]
					at = offset
					while ((p = index(rest, sect)) > 0) {
						at += p - 1 + length(sect)
						rest = substr(rest, p + length(sect))
						text = substr(line, at + 1)
						if (text ~ /^[0-9]/) continue
						references++
						if (resolves(file, text, names, n)) continue
						unresolved = 1
						print file ":" lineno ": \"" sect substr(text, 1, 50) "\" matches no heading in the citing file or in a file or folder named on its line"
					}
				}
				offset += length(seg[i]) + 1
			}
		}
		if (r < 0) { print file ": cannot be read" > "/dev/stderr"; exit 3 }
		close(file)
	}
	BEGIN { sect = "§" }
	NF { path[++files] = $0 }
	END {
		for (i = 1; i <= files; i++) read_headings(path[i])
		for (i = 1; i <= files; i++) check(path[i])
		print "checked " references + 0 " section references in " files + 0 " Markdown files, selected by: " scope
		if (files == 0 || references == 0) exit 2
		exit unresolved
	}
' || status=$?

if [ "$status" -eq 2 ]; then
	echo "no Markdown file or no section reference was read" >&2
	exit 1
fi
if [ "$status" -eq 1 ]; then
	echo "rule: docs/convention/documentation.md §Keep it current, \"Every claim about this repository must be verifiable in it\"" >&2
fi
exit "$status"
