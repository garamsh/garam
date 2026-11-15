# Prints one line per field a rendered document sets: `<Kind> <path>`,
# where a path joins the keys above the field with dots and a list item
# is `[]`. A document's apiVersion is printed as `<Kind> apiVersion=<v>`.
# Only keys are printed, never a value but the apiVersion's, so no
# setting's value reaches the output.
#
# It reads what `helm template` writes: two-space indentation, block
# mappings and block lists, no flow collections and no multi-line
# scalars. A line it cannot place stops it with a message, because a
# field it skipped is a field the allowlist never saw.

function flush(   i) {
	if (n > 0 && kind == "") {
		print "paths.awk: a document with no kind" > "/dev/stderr"; bad = 1
	}
	for (i = 1; i <= n; i++) print kind " " out[i]
	n = 0; kind = ""; depth = 0
}

function prefix(   i, p) {
	p = ""
	for (i = 1; i <= depth; i++) p = p (name_at[i] == "[]" ? "[]" : (p == "" ? "" : ".") name_at[i])
	return p
}

function pop(at) {
	while (depth > 0 && ind_at[depth] >= at) depth--
}

/^---$/ { flush(); next }
/^[[:space:]]*#/ { next }
/^[[:space:]]*$/ { next }

{
	match($0, /^ */); ind = RLENGTH
	body = substr($0, ind + 1)
	dashed = 0
	while (body ~ /^- /) {
		pop(ind)
		depth++; ind_at[depth] = ind; name_at[depth] = "[]"
		ind += 2; body = substr(body, 3); sub(/^ +/, "", body)
		dashed = 1
	}
	if (body !~ /^[A-Za-z0-9_.\/-]+:( |$)/) {
		if (dashed) { out[++n] = prefix(); next }
		print "paths.awk: cannot place line: " $0 > "/dev/stderr"; bad = 1; next
	}
	key = body; sub(/:( .*)?$/, "", key)
	val = body; sub(/^[^:]*: */, "", val)
	pop(ind)
	p = prefix()
	full = (p == "" ? key : p "." key)
	if (full == "kind") kind = val
	out[++n] = (full == "apiVersion" ? "apiVersion=" val : full)
	depth++; ind_at[depth] = ind; name_at[depth] = key
}

END { flush(); exit bad }
