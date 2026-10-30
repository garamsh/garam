#!/bin/sh
# Runs cowork's server image and checks it: `migrate` applies the
# migrations the image ships, `serve` starts as the image's own
# account, and `probe` finds /healthz and /readyz answering, the
# second once the database does. The subject is the image
# release publishes to garam/cowork, which cowork's suites never open:
# they build the binary from this tree.
#
# The database is the postgres deploy/local/docker-compose.yml runs,
# read from that file so the two cannot name different servers, and
# pulled from Google's mirror for the reason deploy/local/Dockerfile
# states.
#
# COWORK_IMAGE names the image, which `make cowork-image-e2e` takes from
# `make image-build`, so the same checks run against one pulled from a
# registry by digest.
set -eu

image=${COWORK_IMAGE:?COWORK_IMAGE names the image to check}
net=cowork-image-e2e-$$
db=
server=
passed=0

cleanup() {
	if [ -n "$server" ]; then docker rm -f "$server" >/dev/null 2>&1 || true; fi
	if [ -n "$db" ]; then docker rm -f "$db" >/dev/null 2>&1 || true; fi
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

docker network create "$net" >/dev/null

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
# that carries a credential, and the probes carry none.
server=$net-server
docker run -d --name "$server" --network "$net" \
	-e "COWORK_DATABASE_URL=$url" \
	-e COWORK_PLATFORM_URL=http://platform.image-e2e.invalid \
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
