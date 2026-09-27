#!/bin/sh
# Checks which tags scripts/image-build.sh accepts for a development
# build (ADR-0071), and that a refused one reaches no build. Each case
# runs the script in a scratch repository holding this tree's copy of
# it and of .github/workflows/release.yml, committed once, with a
# `docker` on PATH that records its arguments: the tree is clean
# whatever state this one is in, and nothing reaches a daemon.
#
# Every refusal is read beside a control the same mechanism accepts,
# so a script refusing everything fails here as surely as one refusing
# nothing.
set -eu

root=$(pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

repo=$scratch/repo
mkdir -p "$repo/scripts" "$repo/.github/workflows" "$scratch/bin"
cp scripts/image-build.sh "$repo/scripts/"
cp .github/workflows/release.yml "$repo/.github/workflows/"
git -C "$repo" init -q
git -C "$repo" -c user.name=test -c user.email=test@invalid add -A
git -C "$repo" -c user.name=test -c user.email=test@invalid commit -qm scratch
head=$(git -C "$repo" rev-parse HEAD)
short=$(printf '%s' "$head" | cut -c1-12)

# Records each call and answers `image inspect` with a fixed id.
cat >"$scratch/bin/docker" <<'EOF'
#!/bin/sh
echo "$*" >>"$DOCKER_LOG"
[ "$1" != image ] || echo sha256:0
EOF
chmod +x "$scratch/bin/docker"

cases=0
failed=0
out=$scratch/out
log=$scratch/docker.log

run() {
	: >"$log"
	(cd "$repo" && PATH="$scratch/bin:$PATH" DOCKER_LOG="$log" \
		scripts/image-build.sh "$@") >"$out" 2>&1
}

fail() {
	echo "image-build-test: $1" >&2
	sed 's/^/  | /' "$out" >&2
	failed=$((failed + 1))
}

# refused <reason in output> <args...>: exits non-zero, says why, and
# calls docker not once.
refused() {
	want=$1
	shift
	cases=$((cases + 1))
	if run "$@"; then
		fail "accepted $*"
	elif ! grep -Fq -- "$want" "$out"; then
		fail "refused $* without naming: $want"
	elif [ -s "$log" ]; then
		fail "refused $* after calling docker: $(cat "$log")"
	fi
}

# accepted <expected line> <args...>: exits zero and prints the line.
accepted() {
	want=$1
	shift
	cases=$((cases + 1))
	if ! run "$@"; then
		fail "refused $*"
	elif ! grep -Fxq -- "$want" "$out"; then
		fail "$* did not print: $want"
	fi
}

reg=registry.invalid

# The check's own names are what they were before a development build
# existed, and a development build moves only the registry and the tag.
accepted '. garam-server:ci' --names
accepted 'web garam-web:ci' --names
accepted ". $reg/garam/garam-server:$short" --names --registry "$reg"
accepted "web $reg/garam/garam-web:$short" --names --registry "$reg"

# A release version against the prerelease it is one suffix from.
refused 'is a release version' --names --registry "$reg" --tag 1.2.3
refused 'is a release version' --registry "$reg" --tag 1.2.3
refused 'is a release version' --registry "$reg" --tag v1.2.3
accepted ". $reg/garam/garam-server:1.2.3-rc.1" --names --registry "$reg" --tag 1.2.3-rc.1
accepted ". $reg/garam/garam-server:1.2.3-0.dev.x-1" --names --registry "$reg" --tag 1.2.3-0.dev.x-1

# A commit prefix names HEAD or is refused.
refused 'is not a prefix of HEAD' --names --registry "$reg" --tag 0000000
accepted ". $reg/garam/garam-server:$head" --names --registry "$reg" --tag "$head"
accepted ". $reg/garam/garam-server:$(printf '%s' "$head" | cut -c1-7)" \
	--names --registry "$reg" --tag "$(printf '%s' "$head" | cut -c1-7)"

# Anything else is refused.
for tag in latest ci dev 1.2.3-01 1.2.3+build 1.2.3- v1.2.3-rc.1 1.2 \
	"$(printf '%s' "$head" | cut -c1-6)" "$(printf '%s' "$head" | tr a-f A-F)"; do
	refused 'is neither a commit prefix' --names --registry "$reg" --tag "$tag"
done

# A tag needs the registry it is for; a registry is a host alone.
refused 'names the registry it is for' --names --tag 1.2.3-rc.1
refused 'is not a host[:port]' --names --registry "$reg/garam"
accepted ". $reg:5000/garam/garam-server:1.2.3-rc.1" --names --registry "$reg:5000" --tag 1.2.3-rc.1

# The build itself: labels name HEAD and the tag, and a tree that is
# not HEAD is refused before docker is called.
cases=$((cases + 1))
if ! run --registry "$reg" --tag 1.2.3-rc.1; then
	fail 'refused a clean development build'
elif ! grep -Fq -- "build -t $reg/garam/garam-server:1.2.3-rc.1 " "$log" \
	|| ! grep -Fq -- "--build-arg IMAGE_REVISION=$head --build-arg IMAGE_VERSION=1.2.3-rc.1 " "$log"; then
	fail "built with other arguments: $(cat "$log")"
fi
touch "$repo/untracked"
refused 'differs from HEAD' --registry "$reg" --tag 1.2.3-rc.1
rm "$repo/untracked"

# A `git` whose status fails and prints nothing, every other command
# reaching the real one: the failure is refused, not read as clean,
# and the same wrapper with status passed through builds.
realgit=$(command -v git)
cat >"$scratch/bin/git" <<EOF
#!/bin/sh
[ "\$1" != status ] || [ -z "\${GIT_STATUS_FAILS:-}" ] || exit 128
exec "$realgit" "\$@"
EOF
chmod +x "$scratch/bin/git"
GIT_STATUS_FAILS=1
export GIT_STATUS_FAILS
refused 'git status failed' --registry "$reg" --tag 1.2.3-rc.1
unset GIT_STATUS_FAILS
cases=$((cases + 1))
if ! run --registry "$reg" --tag 1.2.3-rc.1 || ! grep -q '^build ' "$log"; then
	fail 'refused a clean build through the git wrapper'
fi
rm "$scratch/bin/git"

# The check's build passes neither per-run argument.
cases=$((cases + 1))
if ! run; then
	fail 'refused the check build'
elif ! grep -Fq -- 'build -t garam-server:ci -f deploy/local/Dockerfile .' "$log" \
	|| grep -q IMAGE_ "$log"; then
	fail "check build ran other arguments: $(cat "$log")"
fi

[ "$failed" -eq 0 ] || {
	echo "image-build-test: $failed of $cases cases failed" >&2
	exit 1
}
echo "image-build-test: $cases cases passed, read against $root/.github/workflows/release.yml"
