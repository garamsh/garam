#!/bin/sh
# Runs cowork's server image and checks it: `migrate` applies the
# migrations the image ships, `serve` starts as the image's own
# account with the five policy settings a deployment gives it, and
# `probe` finds /healthz and /readyz answering, the second once the
# database does. The subject is the image
# release publishes to garam/cowork, which cowork's suites never open:
# they build the binary from this tree.
#
# The database is the postgres deploy/local/docker-compose.yml runs,
# read from that file so the two cannot name different servers, and
# pulled from Google's mirror for the reason deploy/local/Dockerfile
# states.
#
# The settings name a policy service and a token endpoint that do not
# exist here: serve reads the CA file and dials lazily, and neither the
# probes nor the database ask for a decision. The deployment key and the
# CA file are made for the run, in a volume the server reads alone and
# the end of the run removes. The key is written by the stack's own
# key one-shot's image (deploy/local/cowork-keys.sh) and never printed.
#
# COWORK_IMAGE names the image, which `make cowork-image-e2e` takes from
# `make image-build`, so the same checks run against one pulled from a
# registry by digest.
set -eu

image=${COWORK_IMAGE:?COWORK_IMAGE names the image to check}
net=cowork-image-e2e-$$
db=
server=
keys=
passed=0

cleanup() {
	if [ -n "$server" ]; then docker rm -f "$server" >/dev/null 2>&1 || true; fi
	if [ -n "$db" ]; then docker rm -f "$db" >/dev/null 2>&1 || true; fi
	if [ -n "$keys" ]; then docker volume rm -f "$keys" >/dev/null 2>&1 || true; fi
	docker network rm "$net" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

fail() {
	echo "cowork-image-e2e: $1" >&2
	if [ -n "$server" ]; then docker logs "$server" >&2 2>&1 || true; fi
	exit 1
}

postgres=$(awk '$1 == "postgres:" { s = 1 } s && $1 == "image:" { print $2; exit }' deploy/local/docker-compose.yml)
case $postgres in
postgres:*@sha256:*) postgres=mirror.gcr.io/library/$postgres ;;
*) fail "deploy/local/docker-compose.yml names no postgres image by digest; read '$postgres'" ;;
esac

alpine=$(awk '/^  cowork-keys:$/ { s = 1 } s && $1 == "image:" { print $2; exit }' deploy/local/docker-compose.yml)
case $alpine in
alpine:*@sha256:*) ;;
*) fail "deploy/local/docker-compose.yml names no cowork-keys image by digest; read '$alpine'" ;;
esac

docker network create "$net" >/dev/null

# The key belongs to the image's account at mode 600, which serve
# requires of it. The CA file holds one throwaway certificate.
keys=$net-keys
docker volume create "$keys" >/dev/null
out=$(docker run --rm -v "$keys:/keys" "$alpine" sh -c '
	set -e
	apk add --no-cache --quiet openssl
	openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out /keys/deployment.pem 2>/dev/null
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
		-keyout /dev/null -subj /CN=policy.image-e2e.invalid -days 1 -out /keys/ca.crt 2>/dev/null
	chown 10001 /keys/deployment.pem
	chmod 600 /keys/deployment.pem
	chmod 644 /keys/ca.crt
' 2>&1) || fail "making the key material failed: $out"

db=$net-db
docker run -d --name "$db" --network "$net" \
	-e POSTGRES_USER=cowork -e POSTGRES_PASSWORD=cowork -e POSTGRES_DB=cowork \
	"$postgres" >/dev/null

# Over TCP, as the compose healthcheck asks: the image's init server
# listens on the socket alone, so a socket probe answers too early.
i=0
until docker exec "$db" pg_isready -q -h 127.0.0.1 -U cowork 2>/dev/null; do
	i=$((i + 1))
	[ "$i" -lt 120 ] || fail "postgres did not accept connections within 60s"
	sleep 0.5
done

url="postgres://cowork:cowork@$db:5432/cowork?sslmode=disable"

out=$(docker run --rm --name "$net-migrate" --network "$net" -e "COWORK_DATABASE_URL=$url" "$image" migrate 2>&1) ||
	fail "migrate failed: $out"
tables=$(docker exec "$db" psql -U cowork -d cowork -tAc \
	"SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'schema_migrations'")
[ "${tables:-0}" -gt 0 ] || fail "migrate reported '$out' and created no table"
passed=$((passed + 1))

user=$(docker image inspect -f '{{.Config.User}}' "$image")
[ "$user" = cowork ] || fail "the image runs as '$user', want cowork"
passed=$((passed + 1))

# The platform is never reached: serve dials it only for a request
# that carries a credential, and the probes carry none. Nor is the
# policy service, which is dialled for a decision.
server=$net-server
docker run -d --name "$server" --network "$net" \
	-e "COWORK_DATABASE_URL=$url" \
	-e COWORK_PLATFORM_URL=http://platform.image-e2e.invalid \
	-e COWORK_POLICY_ADDR=policy.image-e2e.invalid:8443 \
	-e COWORK_POLICY_CA_FILE=/keys/ca.crt \
	-e COWORK_MACHINE_TOKEN_URL=https://machine.image-e2e.invalid:8443/oauth/token \
	-e COWORK_IDENTITY_ISSUER=https://garam.image-e2e.invalid/api \
	-e COWORK_DEPLOYMENT_KEY_FILE=/keys/deployment.pem \
	-v "$keys:/keys:ro" \
	"$image" serve >/dev/null

# Wait on the condition the checks depend on, and stop early when the
# container is gone, which is how a server refusing its configuration
# presents.
i=0
until docker exec "$server" cowork probe http://127.0.0.1:8090/healthz 2>/dev/null; do
	[ "$(docker inspect -f '{{.State.Running}}' "$server")" = "true" ] ||
		fail "the container exited before it served /healthz"
	i=$((i + 1))
	[ "$i" -lt 60 ] || fail "/healthz did not answer within 30s"
	sleep 0.5
done
passed=$((passed + 1))

docker exec "$server" cowork probe http://127.0.0.1:8090/readyz ||
	fail "/readyz did not answer ok with the database up"
passed=$((passed + 1))

echo "cowork-image-e2e: $passed checks passed against $image ($tables tables migrated on $postgres)"
