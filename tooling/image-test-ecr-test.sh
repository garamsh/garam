#!/bin/sh
# Checks what tooling/image-test-push.sh and tooling/image-test-pin.sh
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
mkdir -p "$repo/tooling" "$repo/platform/tests/image" "$bin" "$registry"
cp tooling/image-test-push.sh tooling/image-test-pin.sh "$repo/tooling/"
# The build and its check: each records that it ran, and the check
# fails when CHECK_FAILS is set.
cat >"$repo/tooling/image-test.sh" <<'EOF'
#!/bin/sh
echo "build $*" >>"$CALLS"
EOF
cat >"$repo/platform/tests/image/test-image.sh" <<'EOF'
#!/bin/sh
echo "check $*" >>"$CALLS"
[ -z "${CHECK_FAILS:-}" ] || { echo 'test-image-e2e: stand-in check failed' >&2; exit 1; }
EOF
chmod +x "$repo/tooling/image-test.sh" "$repo/platform/tests/image/test-image.sh"
git -C "$repo" init -q
git -C "$repo" -c user.name=test -c user.email=test@invalid add -A
git -C "$repo" -c user.name=test -c user.email=test@invalid commit -qm scratch
head=$(git -C "$repo" rev-parse HEAD)
short=$(printf '%s' "$head" | cut -c1-12)

# A tag in the registry is three files: <tag>.digest, <tag>.media and
# <tag>.manifest. AWS_FAILS makes every call fail as a missing
# credential does; PUT_FAILS makes put-image fail as an immutable tag
# does, and PUT_STORES stores another digest than the one asked for.
# MANIFEST_FAILS fails reading a manifest, and READBACK_FAILS fails
# every read once a push or a put has written the .written marker.
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
	[ -z "${READBACK_FAILS:-}" ] || [ ! -f "$REGISTRY/.written" ] || exit 254
	case $query in
	*.imageManifest) [ -z "${MANIFEST_FAILS:-}" ] || exit 254 ;;
	esac
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
	: >"$REGISTRY/.written"
	;;
*) exit 252 ;;
esac
EOF
# A push stores sha256:pushed under the tag it names, and the local
# image reports PUSH_REPORTS as its repository digest. LOGIN_FAILS
# fails the login.
cat >"$bin/docker" <<'EOF'
#!/bin/sh
echo "docker $*" >>"$CALLS"
case $1 in
login)
	cat >/dev/null
	[ -z "${LOGIN_FAILS:-}" ]
	;;
push)
	echo sha256:pushed >"$REGISTRY/${2##*:}.digest"
	: >"$REGISTRY/.written"
	;;
image)
	eval "ref=\${$#}"
	echo "${ref%:*}@${PUSH_REPORTS:-sha256:pushed}"
	;;
esac
EOF
chmod +x "$bin/aws" "$bin/docker"

ecr=486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam-dev/garam-test
want=sha256:want
cases=0
failed=0
out=$scratch/out
calls=$scratch/calls

# reset clears the registry and holds an image of $head in it, under
# its first twelve hex digits, unless told `empty`.
reset() {
	rm -f "$registry"/* "$registry/.written"
	[ "${1:-}" = empty ] && return
	echo "$want" >"$registry/$short.digest"
	echo application/vnd.oci.image.manifest.v1+json >"$registry/$short.media"
	echo '{"schemaVersion":2}' >"$registry/$short.manifest"
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

push=tooling/image-test-push.sh
pin=tooling/image-test-pin.sh

# test-image-push: the control checks HEAD under its full hash, pushes
# it under the first twelve digits, and reads the digest back.
reset empty
accepted "image-test-push: pushed $ecr:$short ($head), sha256:pushed" $push
cases=$((cases + 1))
called "^check $ecr:$head\$" && called "^docker push $ecr:$short\$" \
	|| fail "the push control did not check $ecr:$head and push $ecr:$short: $(cat "$calls")"

# Every call names the one repository: its account, its region and
# its name, and Docker logs in to and pushes to its host alone.
cases=$((cases + 1))
aws_calls=$(grep '^aws ecr batch-get-image ' "$calls" | wc -l)
aws_ours=$(grep -c -- '--region ap-northeast-2 --registry-id 486152169996 --repository-name garam-dev/garam-test ' "$calls" || true)
[ "$aws_calls" -gt 0 ] && [ "$aws_ours" -eq "$aws_calls" ] \
	&& called '^aws ecr get-login-password --region ap-northeast-2$' \
	&& called '^docker login --username AWS --password-stdin 486152169996\.dkr\.ecr\.ap-northeast-2\.amazonaws\.com$' \
	&& [ "$(grep -c '^docker push ' "$calls")" -eq 1 ] \
	|| fail "the push control reached another repository: $(cat "$calls")"

# A repository named on the command line is refused, whatever it is:
# another account, another region, the release namespace, or this one.
untouched 'usage: tooling/image-test-push.sh' $push 123456789012.dkr.ecr.ap-northeast-2.amazonaws.com/garam-dev/garam-test
untouched 'usage: tooling/image-test-push.sh' $push 486152169996.dkr.ecr.us-east-1.amazonaws.com/garam-dev/garam-test
untouched 'usage: tooling/image-test-push.sh' $push 486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/garam-test
untouched 'usage: tooling/image-test-push.sh' $push "$ecr"

touch "$repo/untracked"
reset empty
untouched 'differs from HEAD' $push
rm "$repo/untracked"
echo changed >>"$repo/tooling/image-test.sh"
untouched 'differs from HEAD' $push
git -C "$repo" checkout -q tooling/image-test.sh

# A `git` whose status fails and prints nothing, every other command
# reaching the real one: the failure is refused, not read as clean.
realgit=$(command -v git)
cat >"$bin/git" <<EOF
#!/bin/sh
[ "\$1" != status ] || exit 128
exec "$realgit" "\$@"
EOF
chmod +x "$bin/git"
untouched 'git status failed' $push
rm "$bin/git"

reset empty
with AWS_FAILS=1 refused "could not be read with the caller's AWS credentials" $push
cases=$((cases + 1))
! called '^docker ' || fail "a push without credentials called docker: $(cat "$calls")"

reset
accepted "image-test-push: $ecr:$short is already in the repository, $want; nothing built" $push
cases=$((cases + 1))
! called '^(docker|build) ' || fail "a commit already pushed was built or pushed again: $(cat "$calls")"

reset empty
with CHECK_FAILS=1 refused 'test-image-e2e: stand-in check failed' $push
cases=$((cases + 1))
! called '^docker push' || fail "an image whose check failed was pushed: $(cat "$calls")"

reset empty
with PUSH_REPORTS=sha256:other refused 'but the push reported sha256:other' $push

reset empty
with LOGIN_FAILS=1 refused 'docker could not log in to' $push
cases=$((cases + 1))
! called '^(build|docker push) ' || fail "a push whose login failed built or pushed: $(cat "$calls")"

reset empty
with READBACK_FAILS=1 refused 'was pushed as sha256:pushed and could not be read back' $push

# test-image-pin: the control moves the tag with the commit's manifest
# under the digest it was read with, the commit named in full or by
# the twelve digits it is pushed under.
reset
accepted "image-test-pin: $ecr:pinned-consumer is $short, $want" $pin "$head" consumer
cases=$((cases + 1))
grep -Fq -- "--image-tag pinned-consumer --image-manifest {\"schemaVersion\":2} --image-manifest-media-type application/vnd.oci.image.manifest.v1+json --image-digest $want" "$calls" \
	|| fail "the pin control put another image: $(cat "$calls")"

cases=$((cases + 1))
aws_calls=$(grep -c '^aws ecr ' "$calls" || true)
aws_ours=$(grep -c -- '--region ap-northeast-2 --registry-id 486152169996 --repository-name garam-dev/garam-test ' "$calls" || true)
[ "$aws_calls" -gt 0 ] && [ "$aws_ours" -eq "$aws_calls" ] && ! called '^docker ' \
	|| fail "the pin control reached another repository: $(cat "$calls")"

untouched 'usage: tooling/image-test-pin.sh' $pin "$head"
for other in 123456789012.dkr.ecr.ap-northeast-2.amazonaws.com/garam-dev/garam-test \
	486152169996.dkr.ecr.us-east-1.amazonaws.com/garam-dev/garam-test \
	486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/garam-test "$ecr"; do
	untouched 'usage: tooling/image-test-pin.sh' $pin "$other" "$head" consumer
done
reset
accepted "image-test-pin: $ecr:pinned-consumer is $short, $want" $pin "$short" consumer
for commit in "$(printf '%s' "$head" | cut -c1-11)" "$(printf '%s' "$head" | tr a-f A-F)" "${head}0" ''; do
	untouched 'is not 12 to 40 lowercase hex digits' $pin "$commit" consumer
done
long=$(printf 'a%.0s' $(seq 122))
for consumer in Consumer -consumer consumer- 'consumer/x' '' "$long"; do
	untouched 'is not lowercase letters, digits and inner hyphens' $pin "$head" "$consumer"
done
accepted "image-test-pin: $ecr:pinned-$(printf 'a%.0s' $(seq 121)) is $short, $want" \
	$pin "$head" "$(printf 'a%.0s' $(seq 121))"

reset empty
refused 'push it first with make test-image-push' $pin "$head" consumer
with AWS_FAILS=1 refused "could not be read with the caller's AWS credentials" $pin "$head" consumer

reset
echo "$want" >"$registry/pinned-consumer.digest"
accepted "image-test-pin: $ecr:pinned-consumer is already $short, $want" $pin "$head" consumer
cases=$((cases + 1))
! called put-image || fail "a pin already in place was put again: $(cat "$calls")"

reset
echo sha256:older >"$registry/pinned-consumer.digest"
with PUT_FAILS=1 refused 'moving pinned-consumer from sha256:older to' $pin "$head" consumer
cases=$((cases + 1))
grep -Fq 'tag immutability without the pinned-* exclusion filter' "$out" \
	|| fail 'a refused move does not name tag immutability'
reset
with PUT_FAILS=1 refused "the registry refused tagging $short as pinned-consumer" $pin "$head" consumer
cases=$((cases + 1))
! grep -Fq 'immutability' "$out" || fail 'a refused first pin names tag immutability'

reset
with MANIFEST_FAILS=1 refused "$ecr:$short's manifest could not be read" $pin "$head" consumer
cases=$((cases + 1))
! called put-image || fail "a pin whose manifest read failed was put: $(cat "$calls")"

reset
with READBACK_FAILS=1 refused "$ecr:pinned-consumer could not be read back" $pin "$head" consumer

reset
with PUT_STORES=sha256:other refused "pinned-consumer resolves to sha256:other, not $short's $want" $pin "$head" consumer

[ "$failed" -eq 0 ] || {
	echo "image-test-ecr-test: $failed of $cases cases failed" >&2
	exit 1
}
echo "image-test-ecr-test: $cases cases passed, read against $root/tooling/image-test-push.sh and $root/tooling/image-test-pin.sh"
