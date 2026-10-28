#!/bin/sh
# check-toolchain reads the pairs `.github/dependabot.yml`'s docker entry
# predicts a half-done bump of and nothing checked: a version this
# repository declares, and the base image tag that has to carry the same
# one. #794 arrived exactly as that comment says such a bump does —
# Dockerfile.web's node base moved two majors while console/.nvmrc
# stayed at 24 — and a reviewer caught it rather than a check (#803).
#
# The two sites need not name a version to the same depth:
# console/.nvmrc names a Node major and the tag names a patch. They are
# compared over the components both name, so the looser site is held to
# what it declares and to no more.
#
# Every FROM line naming the image is read, counted against the FROM
# lines the file holds, so a stage added to a Dockerfile is covered the
# day it lands and a parse that stopped seeing them fails instead of
# passing an empty set. A tag no version reads out of is that same
# failure.
#
# TOOLCHAIN_PAIRS arrives in the environment; the Makefile holds it.

set -- $TOOLCHAIN_PAIRS; pairs=$#

echo "check-toolchain: $pairs pairs — a version this repository declares against the base image tag carrying it, the split .github/dependabot.yml's docker entry predicts"
test "$pairs" -gt 0 \
	|| { echo 'check-toolchain: TOOLCHAIN_PAIRS names no pair — the Makefile holds none, or what it read out of the tree came back empty'; exit 1; }
set -e

# The count of dot-separated components a version names.
depth() { printf '%s' "$1" | awk -F. '{print NF}'; }

for pair in $TOOLCHAIN_PAIRS; do
	site=${pair%%,*}; rest=${pair#*,}
	declared=${rest%%,*}; rest=${rest#*,}
	dockerfile=${rest%%,*}; image=${rest##*,}
	test "$site,$declared,$dockerfile,$image" = "$pair" \
		|| { echo "check-toolchain: $pair is not the site,version,dockerfile,image a pair is written as"; exit 1; }
	test -n "$declared" \
		|| { echo "check-toolchain: $site declared no version — the Makefile's read of it is broken"; exit 1; }
	case $declared in
	*[!0-9.]*|.*|*.|*..*)
		echo "check-toolchain: $site declares $declared, which is no dotted version — nothing to hold $image tags against"; exit 1 ;;
	esac
	test -f "$dockerfile" \
		|| { echo "check-toolchain: $dockerfile is not a file — the pair names a Dockerfile that is not there"; exit 1; }

	lines="$(grep -cE '^FROM([[:space:]]|$)' "$dockerfile" || true)"
	refs="$(sed -n 's/^FROM[[:space:]][[:space:]]*\([^[:space:]][^[:space:]]*\).*/\1/p' "$dockerfile")"
	test -n "$refs" \
		|| { echo "check-toolchain: $dockerfile: no image reads out of its $lines FROM lines — the parse is broken"; exit 1; }
	total="$(printf '%s\n' "$refs" | wc -l)"
	test "$total" -eq "$lines" \
		|| { echo "check-toolchain: $dockerfile: $total images read out of $lines FROM lines — the parse is broken"; exit 1; }

	named=0; over=0; split=''
	for ref in $refs; do
		nametag=${ref##*/}
		case ${nametag%%[:@]*} in "$image") ;; *) continue ;; esac
		named=$((named + 1))
		tag=${nametag%%@*}
		case $tag in
		*:*) tag=${tag#*:} ;;
		*) echo "check-toolchain: $dockerfile: $ref carries no tag — nothing to hold $site's $declared against"; exit 1 ;;
		esac
		version=${tag%%-*}
		case $version in
		''|*[!0-9.]*|.*|*.|*..*)
			echo "check-toolchain: $dockerfile: no version reads out of $ref — the tag parse is broken"; exit 1 ;;
		esac
		d="$(depth "$declared")"; t="$(depth "$version")"
		if test "$t" -lt "$d"; then n="$t"; else n="$d"; fi
		if test "$over" -eq 0 || test "$n" -lt "$over"; then over="$n"; fi
		test "$(printf '%s' "$declared" | cut -d. -f1-"$n")" = "$(printf '%s' "$version" | cut -d. -f1-"$n")" \
			|| split="$split  $ref
"
	done
	test "$named" -gt 0 \
		|| { echo "check-toolchain: $dockerfile: none of its $total FROM lines names $image — the image the pair asks about is not built from"; exit 1; }
	test -z "$split" || {
		echo "check-toolchain: $site declares $declared and $dockerfile names another version on $(printf '%s' "$split" | grep -c .) of its $named $image tags — the half-done bump .github/dependabot.yml's docker entry predicts:"
		printf '%s' "$split"; exit 1; }
	echo "check-toolchain: $site $declared — $named of $total FROM lines in $dockerfile name $image, every tag agreeing over $over of its components"
done
