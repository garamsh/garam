#!/bin/sh
# Runs a web image and checks it: the standalone server starts,
# /healthz and /readyz answer (ADR-0023), `/api/*` reaches the backend
# at the address the container is given (ADR-0009) and no other, and the
# other app's address is likewise the one the container is given rather
# than one the build held (ADR-0131). The subject is the image a
# release publishes, which neither app's e2e suite opens — each builds
# this tree's app and serves it.
#
# The one image runs twice, each time against its own stub backend, so
# an address chosen when the image was built could not answer both. The
# stubs echo the method, the path they were reached on and the number
# of body bytes they read, which shows the `/api` segment was stripped
# and a request body arrived whole; one of them also streams a
# response, which shows the first chunk reaches the client before the
# backend has finished.
#
# APP names the app's folder: `platform-web`, the default, or
# `site-web`.
#
# 3000 below is the PORT deploy/local/Dockerfile.web sets. The chart
# probes 3000 as well and nothing ties the two, so nothing here
# establishes anything about the chart's probes (#870).
#
# GARAM_WEB_IMAGE names an image to check instead of building one, so the
# same checks run against an image pulled from a registry by digest.
set -eu

image=${GARAM_WEB_IMAGE:-}
app=${APP:-platform-web}
case $app in
platform-web)
	other=GARAM_SITE_URL
	other_url=http://site.image-e2e.invalid
	sent=/login
	;;
site-web)
	other=GARAM_CONSOLE_URL
	other_url=http://console.image-e2e.invalid
	sent=/console/orgs
	;;
*)
	echo "web-image-e2e: APP is '$app', and the apps are platform-web and site-web" >&2
	exit 1
	;;
esac
net=garam-web-image-$$
started=
passed=0

cleanup() {
	for name in $started; do docker rm -f "$name" >/dev/null 2>&1 || true; done
	docker network rm "$net" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

fail() {
	echo "web-image-e2e: $1" >&2
	for name in $started; do docker logs "$name" >&2 2>&1 || true; done
	exit 1
}

if [ -z "$image" ]; then
	image=garam-$app:image-e2e
	docker build -t "$image" --build-arg APP="$app" -f deploy/local/Dockerfile.web .
fi

# The image holds no address of a backend: not the one the release used
# to bake, and not the placeholder the build's own stage is given. The
# search reads the app's build output and its server, where a rewrite
# target lands; the control names the variable the proxy reads, which
# the compiled code does hold, so a search that read nothing fails.
held=$(docker run --rm --entrypoint grep "$image" -rIl -e 'garam-api' -e 'api\.invalid' \
	"/app/$app/server.js" "/app/$app/.next" || true)
[ -z "$held" ] || fail "the image holds a backend address, in: $held"
docker run --rm --entrypoint grep "$image" -rIlq -e 'GARAM_API_URL' \
	"/app/$app/server.js" "/app/$app/.next" ||
	fail "the control search found no GARAM_API_URL in $image, so it read nothing"
passed=$((passed + 1))

docker network create "$net" >/dev/null

# A stub backend named $1, answering as $2 does. It is the web image's
# own node, so no other image is pulled.
stub() {
	name=$net-stub-$1
	started="$started $name"
	docker run -d --name "$name" --network "$net" --entrypoint node "$image" -e '
const id = process.argv[1];
require("http")
  .createServer((q, s) => {
    let bytes = 0;
    q.on("data", (c) => (bytes += c.length));
    q.on("end", () => {
      if (q.url === "/stream") {
        s.writeHead(200, { "content-type": "text/plain" });
        s.write("first\n");
        setTimeout(() => s.end("second\n"), 3000);
        return;
      }
      s.writeHead(200, { "content-type": "text/plain" });
      s.end(id + " " + q.method + " " + q.url + " " + bytes);
    });
  })
  .listen(8080, "0.0.0.0");
' "$1" >/dev/null
}

# The host's kernel picks the port, not the daemon: a rootless daemon
# allocates from 32768 up knowing nothing of the other daemons on the
# host, so two of them pick the same one (#1436). The port is free
# again between this bind's close and the daemon's own, a window an
# unrelated process can still take it in.
reserve_port() {
	python3 -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])' || fail "could not reserve a host port"
}

# The image started as $1 with GARAM_API_URL set to $2, or unset when
# $2 is empty, published on $3. GARAM_API_URL is given as the chart
# gives it. GARAM_COWORK_URL is required of the site as well (ADR-0135),
# and nothing below goes there.
web() {
	name=$net-web-$1
	started="$started $name"
	docker run -d --name "$name" --network "$net" -e "$other=$other_url" \
		${2:+-e "GARAM_API_URL=$2"} -e GARAM_COWORK_URL=http://cowork.image-e2e.invalid \
		-p "127.0.0.1:$3:3000" "$image" >/dev/null
}

# Wait on the condition the checks depend on — the server answering —
# and stop early when the container is gone, which is how a standalone
# server missing a module at startup presents.
await_healthz() {
	i=0
	until curl -fsS -o /dev/null "http://127.0.0.1:$2/healthz" 2>/dev/null; do
		[ "$(docker inspect -f '{{.State.Running}}' "$net-web-$1")" = "true" ] ||
			fail "the container $1 exited before it served /healthz"
		i=$((i + 1))
		[ "$i" -lt 60 ] || fail "/healthz of $1 did not answer within 30s"
		sleep 0.5
	done
}

stub a
stub b
port_a=$(reserve_port)
web a "http://$net-stub-a:8080" "$port_a"
port_b=$(reserve_port)
web b "http://$net-stub-b:8080" "$port_b"
echo "web-image-e2e: publishing on 127.0.0.1:$port_a and 127.0.0.1:$port_b"
await_healthz a "$port_a"
await_healthz b "$port_b"
passed=$((passed + 1))

status=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port_a/readyz")
[ "$status" = "200" ] || fail "GET /readyz answered $status, want 200"
passed=$((passed + 1))

# One image, two addresses: each run reaches the backend it was given
# and not the other, with the `/api` segment and the query as sent.
body=$(curl -fsS "http://127.0.0.1:$port_a/api/healthz")
[ "$body" = "a GET /healthz 0" ] ||
	fail "GET /api/healthz on the run given stub a reached '$body', want 'a GET /healthz 0'"
body=$(curl -fsS "http://127.0.0.1:$port_b/api/healthz")
[ "$body" = "b GET /healthz 0" ] ||
	fail "GET /api/healthz on the run given stub b reached '$body', want 'b GET /healthz 0'"
body=$(curl -fsS "http://127.0.0.1:$port_b/api/orgs/acme/people?limit=5")
[ "$body" = "b GET /orgs/acme/people?limit=5 0" ] ||
	fail "GET /api/orgs/acme/people?limit=5 reached '$body'"
passed=$((passed + 1))

# A request body arrives whole: 4 MiB is past the 1 MiB the backend
# accepts, so what is asked is that the web app passes it on, not that
# the backend takes it.
body=$(head -c 4194304 /dev/zero | curl -fsS --data-binary @- -H 'content-type: application/octet-stream' \
	"http://127.0.0.1:$port_a/api/echo")
[ "$body" = "a POST /echo 4194304" ] ||
	fail "POST of 4194304 bytes reached the backend as '$body'"
passed=$((passed + 1))

# A streamed response is not held back: the stub sends its first chunk,
# waits three seconds and sends its second. The first line is read, and
# the second is read after the gap, so a response buffered to its end
# would put both at the three-second mark.
timing=$(
	start=$(date +%s%N)
	curl -fsSN "http://127.0.0.1:$port_a/api/stream" | {
		IFS= read -r one
		t1=$(date +%s%N)
		IFS= read -r two
		t2=$(date +%s%N)
		echo "$one $two $(((t1 - start) / 1000000)) $(((t2 - t1) / 1000000))"
	}
)
set -- $timing
[ "${1:-}" = first ] && [ "${2:-}" = second ] ||
	fail "the streamed response read as '$timing', want 'first second ...'"
[ "$3" -lt 2000 ] && [ "$4" -ge 2000 ] ||
	fail "the streamed response's chunks arrived $3 ms and then $4 ms apart, want the first under 2000 and the gap past 2000"
passed=$((passed + 1))

# The build held a placeholder for the other app's address
# (deploy/local/Dockerfile.web), so a redirect naming the one this
# container was given shows the address is read when a request arrives.
location=$(curl -s -o /dev/null -w '%{redirect_url}' "http://127.0.0.1:$port_a$sent")
[ "$location" = "$other_url$sent" ] ||
	fail "GET $sent redirected to '$location', want '$other_url$sent' from $other"
passed=$((passed + 1))

# With no address the image reaches no backend and answers none from a
# default: the proxy's environment fails to parse, so every request,
# the probes included, is a 500 and the readiness probe holds the pod out
# of service (the control is the 200 the runs above answered).
port_c=$(reserve_port)
web c "" "$port_c"
i=0
status=000
until [ "$status" != 000 ]; do
	[ "$(docker inspect -f '{{.State.Running}}' "$net-web-c")" = "true" ] ||
		fail "the run with no GARAM_API_URL exited, and a start that refuses is not what this image does"
	i=$((i + 1))
	[ "$i" -lt 60 ] || fail "the run with no GARAM_API_URL answered nothing within 30s"
	sleep 0.5
	status=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port_c/api/healthz" || true)
done
case $status in
5??) ;;
*) fail "GET /api/healthz with no GARAM_API_URL answered $status, want a 5xx" ;;
esac
status=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port_c/readyz")
[ "$status" = 500 ] || fail "GET /readyz with no GARAM_API_URL answered $status, want 500"
passed=$((passed + 1))

echo "web-image-e2e: $passed checks passed against $image (backends a and b, $other $other_url)"
