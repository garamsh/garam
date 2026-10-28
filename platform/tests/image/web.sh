#!/bin/sh
# Runs a web image and checks it: the standalone server starts,
# /healthz and /readyz answer (ADR-0023), `/api/*` reaches the URL
# baked at build time (ADR-0009), and the other app's address is the
# one the container is given rather than one the build held (ADR-0131).
# The subject is the image a release publishes, which neither app's e2e
# suite opens — each builds this tree's app and serves it.
#
# APP names the app: `console`, the default, or `site`.
#
# 3000 below is the PORT deploy/local/Dockerfile.web sets. The chart
# probes 3000 as well and nothing ties the two, so nothing here
# establishes anything about the chart's probes (#870).
#
# GARAM_WEB_IMAGE names an image to check instead of building one, so the
# same checks run against an image pulled from a registry by digest.
set -eu

image=${GARAM_WEB_IMAGE:-}
app=${APP:-console}
case $app in
console)
	other=GARAM_SITE_URL
	other_url=http://site.image-e2e.invalid
	sent=/login
	;;
site)
	other=GARAM_CONSOLE_URL
	other_url=http://console.image-e2e.invalid
	sent=/console/orgs
	;;
*)
	echo "web-image-e2e: APP is '$app', and the apps are console and site" >&2
	exit 1
	;;
esac
net=garam-web-image-$$
stub=
web=
passed=0

cleanup() {
	if [ -n "$web" ]; then docker rm -f "$web" >/dev/null 2>&1 || true; fi
	if [ -n "$stub" ]; then docker rm -f "$stub" >/dev/null 2>&1 || true; fi
	docker network rm "$net" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

fail() {
	echo "web-image-e2e: $1" >&2
	if [ -n "$web" ]; then docker logs "$web" >&2 2>&1 || true; fi
	exit 1
}

if [ -z "$image" ]; then
	image=garam-$app:image-e2e
	docker build -t "$image" \
		--build-arg GARAM_API_URL=http://garam-api-stub:8080 \
		--build-arg APP="$app" \
		-f deploy/local/Dockerfile.web .
fi

# The rewrite target is baked, so what the image itself carries is the
# only thing the stub can be expected to answer on.
dest=$(docker run --rm --entrypoint node "$image" -e '
const r = require("/app/'"$app"'/.next/routes-manifest.json").rewrites;
const all = [...r.beforeFiles, ...r.afterFiles, ...r.fallback];
const api = all.find((x) => x.source === "/api/:path*");
if (!api) { console.error("no /api/:path* rewrite"); process.exit(1); }
process.stdout.write(api.destination);
') || fail "could not read the /api/* rewrite from $image"

api_host=$(printf %s "$dest" | sed -n 's|^http://\([^:/]*\).*|\1|p')
api_port=$(printf %s "$dest" | sed -n 's|^http://[^:/]*:\([0-9][0-9]*\).*|\1|p')
[ -n "$api_host" ] || fail "the baked rewrite target is not a plain http URL: $dest"
[ -n "$api_port" ] || api_port=80

docker network create "$net" >/dev/null

# The stub stands in for the backend and echoes the path it was reached
# on, which is what shows the `/api` segment was stripped.
stub=$net-stub
docker run -d --name "$stub" --network "$net" --entrypoint node "$image" -e '
require("http")
  .createServer((q, s) => {
    s.writeHead(200, { "content-type": "text/plain" });
    s.end(q.url);
  })
  .listen('"$api_port"', "0.0.0.0");
' >/dev/null

stub_ip=$(docker inspect -f "{{(index .NetworkSettings.Networks \"$net\").IPAddress}}" "$stub")
[ -n "$stub_ip" ] || fail "the stub backend took no address on $net"

# The host's kernel picks the port, not the daemon: a rootless daemon
# allocates from 32768 up knowing nothing of the other daemons on the
# host, so two of them pick the same one (#1436). The port is free
# again between this bind's close and the daemon's own, a window an
# unrelated process can still take it in.
port=$(python3 -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])') || fail "could not reserve a host port"
echo "web-image-e2e: publishing on 127.0.0.1:$port"

# --add-host rather than a network alias: the baked host is a DNS name
# with dots, which an alias does not accept. GARAM_API_URL is given as
# the chart gives it: each app's proxy reads its env, which requires it
# in production. It names the baked target, so the rewrite check below
# still says nothing the runtime value could have answered for.
web=$net-web
docker run -d --name "$web" --network "$net" -e "$other=$other_url" \
	-e "GARAM_API_URL=$dest" \
	--add-host "$api_host:$stub_ip" -p "127.0.0.1:$port:3000" "$image" >/dev/null

# Wait on the condition the checks depend on — the server answering —
# and stop early when the container is gone, which is how a standalone
# server missing a module at startup presents.
i=0
until curl -fsS -o /dev/null "http://127.0.0.1:$port/healthz" 2>/dev/null; do
	[ "$(docker inspect -f '{{.State.Running}}' "$web")" = "true" ] ||
		fail "the container exited before it served /healthz"
	i=$((i + 1))
	[ "$i" -lt 60 ] || fail "/healthz did not answer within 30s"
	sleep 0.5
done
passed=$((passed + 1))

status=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/readyz")
[ "$status" = "200" ] || fail "GET /readyz answered $status, want 200"
passed=$((passed + 1))

body=$(curl -fsS "http://127.0.0.1:$port/api/healthz")
[ "$body" = "/healthz" ] ||
	fail "GET /api/healthz reached the backend as '$body', want '/healthz' at $dest"
passed=$((passed + 1))

# The build held a placeholder for the other app's address
# (deploy/local/Dockerfile.web), so a redirect naming the one this
# container was given shows the address is read when a request arrives.
location=$(curl -s -o /dev/null -w '%{redirect_url}' "http://127.0.0.1:$port$sent")
[ "$location" = "$other_url$sent" ] ||
	fail "GET $sent redirected to '$location', want '$other_url$sent' from $other"
passed=$((passed + 1))

echo "web-image-e2e: $passed checks passed against $image on 127.0.0.1:$port (rewrite target $dest, $other $other_url)"
