#!/bin/sh
# Runs cowork-web's image and checks it: the standalone server starts
# and answers both probes (ADR-0023), a visitor with no session is sent
# to sign in, the sign-in names the site address the container is given
# rather than the placeholder the build held
# (deploy/local/Dockerfile.web) and holds a state on this host
# (ADR-0135), and the app's static assets are served, which the
# standalone output leaves out unless the image copies them in. The
# subject is the image a release publishes to garam/cowork-web, which
# cowork-web's e2e suite never opens.
#
# A visitor with no session asks nothing of cowork or the platform, so
# every address below is one no request reaches.
#
# COWORK_WEB_IMAGE names the image, which `make cowork-web-image-e2e`
# takes from `make image-build`, so the same checks run against one
# pulled from a registry by digest.
set -eu

image=${COWORK_WEB_IMAGE:?COWORK_WEB_IMAGE names the image to check}
site=http://site.image-e2e.invalid
web=
passed=0

cleanup() {
	if [ -n "$web" ]; then docker rm -f "$web" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT INT TERM

fail() {
	echo "cowork-web-image-e2e: $1" >&2
	if [ -n "$web" ]; then docker logs "$web" >&2 2>&1 || true; fi
	exit 1
}

# The host's kernel picks the port, as platform/tests/image/web.sh
# does it and for the reason it states (#1436).
port=$(python3 -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])') || fail "could not reserve a host port"
echo "cowork-web-image-e2e: publishing on 127.0.0.1:$port"

web=cowork-web-image-e2e-$$
docker run -d --name "$web" -p "127.0.0.1:$port:3000" \
	-e COWORK_API_URL=http://cowork.image-e2e.invalid \
	-e COWORK_SYNC_URL=ws://cowork.image-e2e.invalid \
	-e GARAM_API_URL=http://api.image-e2e.invalid \
	-e "GARAM_SITE_URL=$site" \
	"$image" >/dev/null

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

location=$(curl -s -o /dev/null -w '%{redirect_url}' "http://127.0.0.1:$port/acme")
case $location in
*/auth/sign-in?next=%2Facme) ;;
*) fail "GET /acme without a session redirected to '$location', want /auth/sign-in?next=%2Facme" ;;
esac
passed=$((passed + 1))

headers=$(curl -s -o /dev/null -D - "http://127.0.0.1:$port/auth/sign-in?next=%2Facme")
case $headers in
*"location: $site/handoff?app=cowork&next=%2Facme&state="*) ;;
*) fail "GET /auth/sign-in did not redirect to $site/handoff naming cowork, from GARAM_SITE_URL: $headers" ;;
esac
case $headers in
*"set-cookie: garam_handoff_state="*) ;;
*) fail "GET /auth/sign-in held no state on this host: $headers" ;;
esac
passed=$((passed + 1))

# The app renders no page without a session, so the asset's name comes
# from the image's own static folder.
asset=$(docker exec "$web" sh -c "cd /app/cowork-web/.next/static && find . -name '*.js' | head -n 1") ||
	fail "could not list the image's static assets"
[ -n "$asset" ] || fail "the image holds no script under .next/static"
asset=/_next/static/${asset#./}
status=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port$asset")
[ "$status" = "200" ] || fail "GET $asset answered $status, want 200"
passed=$((passed + 1))

echo "cowork-web-image-e2e: $passed checks passed against $image on 127.0.0.1:$port (GARAM_SITE_URL $site, asset $asset)"
