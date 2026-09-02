#!/bin/sh
# Runs the web image and checks it: the standalone server starts,
# /healthz and /readyz answer (ADR-0023), and `/api/*` reaches the URL
# baked at build time (ADR-0009). The subject is the image a release
# publishes, which `web/e2e` never opens — that suite builds this tree's
# app and serves it.
#
# 3000 below is the PORT deploy/local/Dockerfile.web sets. The chart
# probes 3000 as well and nothing ties the two, so nothing here
# establishes anything about the chart's probes (#870).
#
# GARAM_WEB_IMAGE names an image to check instead of building one, so the
# same checks run against an image pulled from a registry by digest.
set -eu

image=${GARAM_WEB_IMAGE:-}
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
	image=garam-web:image-e2e
	docker build -t "$image" \
		--build-arg GARAM_API_URL=http://garam-api-stub:8080 \
		-f deploy/local/Dockerfile.web web
fi

# The rewrite target is baked, so what the image itself carries is the
# only thing the stub can be expected to answer on.
dest=$(docker run --rm --entrypoint node "$image" -e '
const r = require("/app/.next/routes-manifest.json").rewrites;
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

# --add-host rather than a network alias: the baked host is a DNS name
# with dots, which an alias does not accept.
web=$net-web
docker run -d --name "$web" --network "$net" \
	--add-host "$api_host:$stub_ip" -p 127.0.0.1:0:3000 "$image" >/dev/null

port=$(docker port "$web" 3000/tcp | head -1 | sed 's/.*://')
[ -n "$port" ] || fail "the web container published no port"

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

echo "web-image-e2e: $passed checks passed against $image (rewrite target $dest)"
