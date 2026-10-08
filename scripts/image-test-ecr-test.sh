#!/bin/sh
# Checks what scripts/image-test-push.sh and scripts/image-test-pin.sh
# refuse (ADR-0093), and that each refusal made before AWS is called
# calls neither `aws` nor `docker`. Each case runs the scripts in a
# scratch repository holding this tree's copies of them, committed
# once, with stand-ins for the build and its check, and an `aws` and a
# `docker` on PATH that record their arguments and answer from a
# directory standing in for the registry: nothing reaches AWS or a
# daemon, and the tree is clean whatever state this one is in.
#
# Every refusal is read beside a control the same script accepts, so
# a script refusing everything fails here as surely as one refusing
# nothing.
set -eu

root=$(pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT INT TERM

repo=$scratch/repo
bin=$scratch/bin
registry=$scratch/registry
mkdir -p "$repo/scripts" "$repo/tests/image" "$bin" "$registry"
cp scripts/image-test-push.sh scripts/image-test-pin.sh "$repo/scripts/"
# The build and its check: each records that it ran, and the check
# fails when CHECK_FAILS is set.
cat >"$repo/scripts/image-test.sh" <<'EOF'
#!/bin/sh
echo "build $*" >>"$CALLS"
EOF
cat >"$repo/tests/image/test-image.sh" <<'EOF'
#!/bin/sh
echo "check $*" >>"$CALLS"
[ -z "${CHECK_FAILS:-}" ] || { echo 'test-image-e2e: stand-in check failed' >&2; exit 1; }
EOF
chmod +x "$repo/scripts/image-test.sh" "$repo/tests/image/test-image.sh"
git -C "$repo" init -q
git -C "$repo" -c user.name=test -c user.email=test@invalid add -A
git -C "$repo" -c user.name=test -c user.email=test@invalid commit -qm scratch
head=$(git -C "$repo" rev-parse HEAD)

# A tag in the registry is three files: <tag>.digest, <tag>.media and
# <tag>.manifest. AWS_FAILS makes every call fail as a missing
# credential does; PUT_FAILS makes put-image fail as an immutable tag
# does, and PUT_STORES stores another digest than the one asked for.
cat >"$bin/aws" <<'EOF'
#!/bin/sh
echo "aws $*" >>"$CALLS"
[ -z "${AWS_FAILS:-}" ] || { echo 'Unable to locate credentials' >&2; exit 253; }
op=$2
tag= query= digest=
while [ $# -gt 0 ]; do
	case $1 in
	--image-ids) tag=${2#imageTag=} ;;
	--image-tag) tag=$2 ;;
	--query) query=$2 ;;
	--image-digest) digest=$2 ;;
	esac
	shift
done
case $op in
get-login-password) echo password ;;
batch-get-image)
	case $query in
	*.imageDigest) file=$REGISTRY/$tag.digest ;;
	*.imageManifestMediaType) file=$REGISTRY/$tag.media ;;
	*.imageManifest) file=$REGISTRY/$tag.manifest ;;
	esac
	if [ -f "$file" ]; then cat "$file"; else echo None; fi
	;;
put-image)
	[ -z "${PUT_FAILS:-}" ] || { echo 'ImageTagAlreadyExistsException' >&2; exit 254; }
	echo "${PUT_STORES:-$digest}" >"$REGISTRY/$tag.digest"
	;;
*) exit 252 ;;
esac
EOF
# A push stores sha256:pushed under the tag it names, and the local
# image reports PUSH_REPORTS as its repository digest.
cat >"$bin/docker" <<'EOF'
#!/bin/sh
echo "docker $*" >>"$CALLS"
case $1 in
login) cat >/dev/null ;;
push) echo sha256:pushed >"$REGISTRY/${2##*:}.digest" ;;
image)
	eval "ref=\${$#}"
	echo "${ref%:*}@${PUSH_REPORTS:-sha256:pushed}"
	;;
esac
EOF
chmod +x "$bin/aws" "$bin/docker"

ecr=486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/garam-test
want=sha256:want
cases=0
failed=0
out=$scratch/out
calls=$scratch/calls

# reset clears the registry and holds an image of $head in it unless
# told `empty`.
reset() {
	rm -f "$registry"/*
	[ "${1:-}" = empty ] && return
	echo "$want" >"$registry/$head.digest"
	echo application/vnd.oci.image.manifest.v1+json >"$registry/$head.media"
	echo '{"schemaVersion":2}' >"$registry/$head.manifest"
}

run() {
	: >"$calls"
	(cd "$repo" && PATH="$bin:$PATH" CALLS="$calls" REGISTRY="$registry" \
		"$@") >"$out" 2>&1
}

fail() {
	echo "image-test-ecr-test: $1" >&2
	sed 's/^/  | /' "$out" >&2
	failed=$((failed + 1))
}

# refused <reason in output> <command...>: exits non-zero and says why.
refused() {
	want_line=$1
	shift
	cases=$((cases + 1))
	if run "$@"; then
		fail "accepted $*"
	elif ! grep -Fq -- "$want_line" "$out"; then
		fail "refused $* without naming: $want_line"
	fi
}

# untouched <reason in output> <command...>: refused before either
# `aws` or `docker` is called.
untouched() {
	before=$failed
	refused "$@"
	[ "$failed" -ne "$before" ] || [ ! -s "$calls" ] \
		|| fail "refused $* after calling: $(cat "$calls")"
}

# accepted <expected line> <command...>: exits zero and prints the line.
accepted() {
	want_line=$1
	shift
	cases=$((cases + 1))
	if ! run "$@"; then
		fail "refused $*"
	elif ! grep -Fxq -- "$want_line" "$out"; then
		fail "$* did not print: $want_line"
	fi
}

# with <VAR=value> <check...>: runs the check with VAR exported, and
# unset again after it.
with() {
	var=${1%%=*}
	export "$1"
	shift
	"$@"
	unset "$var"
}

called() {
	grep -Eq -- "$1" "$calls"
}

push=scripts/image-test-push.sh
pin=scripts/image-test-pin.sh

# test-image-push: the control pushes HEAD, after the check, and reads
# the digest back.
reset empty
accepted "image-test-push: pushed $ecr:$head, sha256:pushed" $push "$ecr"
cases=$((cases + 1))
called "^check $ecr:$head\$" && called "^docker push $ecr:$head\$" \
	|| fail "the push control did not check and push $ecr:$head: $(cat "$calls")"

untouched 'usage: scripts/image-test-push.sh' $push
untouched 'is not an ECR repository URI' $push garam/garam-test
untouched 'is not an ECR repository URI' $push "ghcr.io/garamsh/garam-test"

touch "$repo/untracked"
reset empty
untouched 'differs from HEAD' $push "$ecr"
rm "$repo/untracked"
echo changed >>"$repo/scripts/image-test.sh"
untouched 'differs from HEAD' $push "$ecr"
git -C "$repo" checkout -q scripts/image-test.sh

# A `git` whose status fails and prints nothing, every other command
# reaching the real one: the failure is refused, not read as clean.
realgit=$(command -v git)
cat >"$bin/git" <<EOF
#!/bin/sh
[ "\$1" != status ] || exit 128
exec "$realgit" "\$@"
EOF
chmod +x "$bin/git"
untouched 'git status failed' $push "$ecr"
rm "$bin/git"

reset empty
with AWS_FAILS=1 refused "could not be read with the caller's AWS credentials" $push "$ecr"
cases=$((cases + 1))
! called '^docker ' || fail "a push without credentials called docker: $(cat "$calls")"

reset
accepted "image-test-push: $ecr:$head is already in the repository, $want; nothing built" $push "$ecr"
cases=$((cases + 1))
! called '^(docker|build) ' || fail "a commit already pushed was built or pushed again: $(cat "$calls")"

reset empty
with CHECK_FAILS=1 refused 'test-image-e2e: stand-in check failed' $push "$ecr"
cases=$((cases + 1))
! called '^docker push' || fail "an image whose check failed was pushed: $(cat "$calls")"

reset empty
with PUSH_REPORTS=sha256:other refused 'but the push reported sha256:other' $push "$ecr"

# test-image-pin: the control moves the tag with the commit's manifest
# under the digest it was read with.
reset
accepted "image-test-pin: $ecr:pinned-consumer is $head, $want" $pin "$ecr" "$head" consumer
cases=$((cases + 1))
grep -Fq -- "--image-tag pinned-consumer --image-manifest {\"schemaVersion\":2} --image-manifest-media-type application/vnd.oci.image.manifest.v1+json --image-digest $want" "$calls" \
	|| fail "the pin control put another image: $(cat "$calls")"

untouched 'usage: scripts/image-test-pin.sh' $pin "$ecr" "$head"
untouched 'is not an ECR repository URI' $pin garam/garam-test "$head" consumer
for commit in "$(printf '%s' "$head" | cut -c1-12)" "$(printf '%s' "$head" | tr a-f A-F)" "${head}0" ''; do
	untouched 'is not a full lowercase commit hash' $pin "$ecr" "$commit" consumer
done
long=$(printf 'a%.0s' $(seq 122))
for consumer in Consumer -consumer consumer- 'consumer/x' '' "$long"; do
	untouched 'is not lowercase letters, digits and inner hyphens' $pin "$ecr" "$head" "$consumer"
done
accepted "image-test-pin: $ecr:pinned-$(printf 'a%.0s' $(seq 121)) is $head, $want" \
	$pin "$ecr" "$head" "$(printf 'a%.0s' $(seq 121))"

reset empty
refused 'push it first with make test-image-push' $pin "$ecr" "$head" consumer
with AWS_FAILS=1 refused "could not be read with the caller's AWS credentials" $pin "$ecr" "$head" consumer

reset
echo "$want" >"$registry/pinned-consumer.digest"
accepted "image-test-pin: $ecr:pinned-consumer is already $head, $want" $pin "$ecr" "$head" consumer
cases=$((cases + 1))
! called put-image || fail "a pin already in place was put again: $(cat "$calls")"

reset
echo sha256:older >"$registry/pinned-consumer.digest"
with PUT_FAILS=1 refused 'moving pinned-consumer from sha256:older to' $pin "$ecr" "$head" consumer
cases=$((cases + 1))
grep -Fq 'tag immutability without the pinned-* exclusion filter' "$out" \
	|| fail 'a refused move does not name tag immutability'
reset
with PUT_FAILS=1 refused "the registry refused tagging $head as pinned-consumer" $pin "$ecr" "$head" consumer
cases=$((cases + 1))
! grep -Fq 'immutability' "$out" || fail 'a refused first pin names tag immutability'

reset
with PUT_STORES=sha256:other refused "pinned-consumer resolves to sha256:other, not $head's $want" $pin "$ecr" "$head" consumer

[ "$failed" -eq 0 ] || {
	echo "image-test-ecr-test: $failed of $cases cases failed" >&2
	exit 1
}
echo "image-test-ecr-test: $cases cases passed, read against $root/scripts/image-test-push.sh and $root/scripts/image-test-pin.sh"
