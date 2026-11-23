#!/bin/sh
# Tests tooling/chart-push.sh with a stand-in for helm that records what
# it was asked and answers as helm and ECR do: a push that succeeds with
# a digest, a push to a repository that does not exist, and one that
# answers no digest.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
fail() {
	echo "chart-push-test: $*" >&2
	exit 1
}

mkdir "$tmp/chart"
cat >"$tmp/helm" <<'STUB'
#!/bin/sh
echo "$*" >>"$STUB_LOG"
case $1 in
package)
	while [ $# -gt 0 ]; do
		[ "$1" = --destination ] && : >"$2/garam-9.9.9.tgz"
		shift
	done
	;;
push)
	case $STUB_MODE in
	ok) printf 'Pushed: %s/garam:0.1.0-dev.99\nDigest: sha256:%s\n' "${3#oci://}" "$STUB_DIGEST" ;;
	missing)
		echo "Error: unexpected status from HEAD request: name unknown: The repository with name 'garam/charts/garam' does not exist in the registry with id '1'" >&2
		exit 1
		;;
	nodigest) echo "Pushed: x" ;;
	esac
	;;
esac
STUB
chmod +x "$tmp/helm"
digest=$(printf '%064d' 0 | tr 0 a)

cat >"$tmp/inspect" <<'STUB'
#!/bin/sh
echo "$*" >>"$STUB_LOG"
echo "sha256:$STUB_STORED"
STUB
chmod +x "$tmp/inspect"

run() {
	INSPECT="$tmp/inspect" STUB_STORED="${STUB_STORED:-$digest}" STUB_LOG="$tmp/log" STUB_MODE=$1 STUB_DIGEST=$digest HELM="$tmp/helm" CHART_DIR="$tmp/chart" \
		GITHUB_OUTPUT="$tmp/output" GITHUB_STEP_SUMMARY="$tmp/summary" \
		sh "$here/chart-push.sh" reg.example 0.1.0-dev.99 >"$tmp/stdout" 2>"$tmp/stderr"
}

: >"$tmp/log"
: >"$tmp/output"
run ok || fail "a push that succeeds failed: $(cat "$tmp/stderr")"
grep -q -- "package $tmp/chart --version 0.1.0-dev.99 --app-version 0.1.0-dev.99 " "$tmp/log" ||
	fail "the chart was not packaged under the release version: $(cat "$tmp/log")"
grep -q 'push .*garam-9.9.9.tgz oci://reg.example/garam/charts$' "$tmp/log" ||
	fail "the push did not go to oci://reg.example/garam/charts: $(cat "$tmp/log")"
[ "$(cat "$tmp/output")" = "chart-digest=sha256:$digest" ] || fail "the digest was not recorded: $(cat "$tmp/output")"
grep -q "reg.example/garam/charts/garam:0.1.0-dev.99" "$tmp/summary" || fail "the summary names no reference"

grep -q 'reg.example/garam/charts/garam:0.1.0-dev.99$' "$tmp/log" ||
	fail "the digest was not read back from the registry by reference: $(cat "$tmp/log")"
if STUB_STORED=$(printf '%064d' 0 | tr 0 b) run ok; then fail "a stored digest that differs from the pushed one was accepted"; fi
grep -q 'holds sha256:b' "$tmp/stderr" || fail "a differing digest is not reported: $(cat "$tmp/stderr")"

if run missing; then fail "a missing repository was accepted"; fi
grep -q '::error::chart-push: the repository reg.example/garam/charts/garam was not found' "$tmp/stdout" "$tmp/stderr" ||
	fail "a missing repository is not reported as such: $(cat "$tmp/stdout" "$tmp/stderr")"

if run nodigest; then fail "a push with no digest was accepted"; fi
grep -q 'answered no sha256 digest' "$tmp/stderr" || fail "an answer with no digest is not reported"

if sh "$here/chart-push.sh" only-one >/dev/null 2>&1; then fail "one argument was accepted"; fi

echo "chart-push-test: package, push target, digest, missing repository, no digest, read-back and arguments checked"
