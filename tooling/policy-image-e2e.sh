#!/bin/sh
# Runs the policy service's image and checks it: `migrate` reaches a
# database that refuses anything but TLS and applies the migrations
# compiled into the binary, `serve` refuses to start without its
# certificate, then starts with one as the image's own account, `probe`
# finds /healthz and /readyz answering, and the contract's port
# completes a verified TLS handshake offering HTTP/2. The subject is
# the image itself, which policy's suites never open: they build the
# binary from this tree.
#
# The postgres and the alpine this runs are the ones
# deploy/local/docker-compose.yml names, read from that file so the two
# cannot differ, and pulled from Google's mirror for the reason
# deploy/local/Dockerfile states.
#
# POLICY_IMAGE names the image, which `make policy-image-e2e` builds
# unless it is given one.
set -eu

image=${POLICY_IMAGE:?POLICY_IMAGE names the image to check}
net=policy-image-e2e-$$
db=$net-db
server=$net-server
db_tls=$net-db-tls
server_tls=$net-server-tls
started=
passed=0

cleanup() {
	for name in $started; do docker rm -f "$name" >/dev/null 2>&1 || true; done
	docker volume rm "$db_tls" "$server_tls" >/dev/null 2>&1 || true
	docker network rm "$net" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

fail() {
	echo "policy-image-e2e: $1" >&2
	for name in $started; do docker logs "$name" >&2 2>&1 || true; done
	exit 1
}

pinned() {
	ref=$(awk -v service="$1:" '$1 == service { s = 1 } s && $1 == "image:" { print $2; exit }' deploy/local/docker-compose.yml)
	case $ref in
	"$2":*@sha256:*) echo "mirror.gcr.io/library/$ref" ;;
	*) fail "deploy/local/docker-compose.yml names no $2 image by digest for $1; read '$ref'" ;;
	esac
}

postgres=$(pinned postgres postgres)
alpine=$(pinned policy-tls alpine)

docker network create "$net" >/dev/null

# A pair for each server, named as the server's container is. 70 is the
# postgres user of the alpine image, and 10001 the image's own.
pair() {
	docker run --rm -v "$PWD/deploy/local/policy-tls.sh:/policy-tls.sh:ro" -v "$1:/tls" \
		"$alpine" sh /policy-tls.sh "$2" "$3"
}
pair "$db_tls" "$db" 70
pair "$server_tls" "$server" 10001

started=$db
docker run -d --name "$db" --network "$net" -v "$db_tls:/tls:ro" \
	-e POSTGRES_USER=policy -e POSTGRES_PASSWORD=policy -e POSTGRES_DB=policy \
	"$postgres" -c ssl=on -c ssl_cert_file=/tls/tls.crt -c ssl_key_file=/tls/tls.key >/dev/null

# Over TCP, as the compose healthcheck asks: the image's init server
# listens on the socket alone, so a socket probe answers too early.
i=0
until docker exec "$db" pg_isready -q -h 127.0.0.1 -U policy 2>/dev/null; do
	i=$((i + 1))
	[ "$i" -lt 120 ] || fail "postgres did not accept connections within 60s"
	sleep 0.5
done

# From here the server refuses a connection over the network that is
# not TLS.
docker exec -i "$db" sh -c 'cat > "$PGDATA/pg_hba.conf"' <<'EOF'
local all all trust
hostssl all all all scram-sha-256
EOF
docker exec "$db" psql -q -U policy -d policy -c 'SELECT pg_reload_conf()' >/dev/null

policy_run() {
	docker run --rm --network "$net" -v "$db_tls:/db-tls:ro" "$@"
}

plaintext="postgres://policy:policy@$db:5432/policy?sslmode=disable"
if out=$(policy_run -e "POLICY_DATABASE_URL=$plaintext" "$image" migrate 2>&1); then
	fail "migrate reached the database without TLS, so the server is not refusing it: $out"
fi
passed=$((passed + 1))

url="postgres://policy:policy@$db:5432/policy?sslmode=verify-full&sslrootcert=/db-tls/tls.crt"
out=$(policy_run -e "POLICY_DATABASE_URL=$url" "$image" migrate 2>&1) ||
	fail "migrate failed over TLS: $out"
tables=$(docker exec "$db" psql -U policy -d policy -tAc \
	"SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename <> '_sqlx_migrations'")
[ "${tables:-0}" -gt 0 ] || fail "migrate reported '$out' and created no table"
passed=$((passed + 1))

user=$(docker image inspect -f '{{.Config.User}}' "$image")
[ "$user" = policy ] || fail "the image runs as '$user', want policy"
passed=$((passed + 1))

serve_settings="-e POLICY_ADDR=0.0.0.0:8443 -e POLICY_HEALTH_ADDR=0.0.0.0:8091 -e POLICY_TOKEN_ISSUER=https://image-e2e.invalid -e POLICY_JWKS_FILE=/etc/policy/jwks.json"
jwks="$PWD/deploy/local/policy-jwks.json:/etc/policy/jwks.json:ro"

# shellcheck disable=SC2086
if out=$(policy_run -v "$jwks" $serve_settings -e "POLICY_DATABASE_URL=$url" "$image" serve 2>&1); then
	fail "serve started with no certificate: $out"
fi
case $out in
*POLICY_TLS_CERT_FILE*) ;;
*) fail "serve stopped without naming the certificate it lacks: $out" ;;
esac
passed=$((passed + 1))

started="$db $server"
# shellcheck disable=SC2086
docker run -d --name "$server" --network "$net" -v "$db_tls:/db-tls:ro" -v "$jwks" \
	-v "$server_tls:/etc/policy/tls:ro" $serve_settings \
	-e "POLICY_DATABASE_URL=$url" \
	-e POLICY_TLS_CERT_FILE=/etc/policy/tls/tls.crt \
	-e POLICY_TLS_KEY_FILE=/etc/policy/tls/tls.key \
	"$image" serve >/dev/null

# Wait on the condition the checks depend on, and stop early when the
# container is gone, which is how a server refusing its configuration
# presents.
i=0
until docker exec "$server" policy probe http://127.0.0.1:8091/healthz 2>/dev/null; do
	[ "$(docker inspect -f '{{.State.Running}}' "$server")" = "true" ] ||
		fail "the container exited before it served /healthz"
	i=$((i + 1))
	[ "$i" -lt 60 ] || fail "/healthz did not answer within 30s"
	sleep 0.5
done
passed=$((passed + 1))

docker exec "$server" policy probe http://127.0.0.1:8091/readyz ||
	fail "/readyz did not answer ok with the database up"
passed=$((passed + 1))

handshake=$(docker run --rm --network "$net" -v "$server_tls:/tls:ro" -e "SERVER=$server" "$alpine" sh -ec '
	apk add --no-cache --quiet openssl
	openssl s_client -connect "$SERVER:8443" -CAfile /tls/tls.crt -verify_hostname "$SERVER" \
		-verify_return_error -alpn h2 </dev/null 2>&1
') || fail "the contract's port refused a verified handshake: $handshake"
case $handshake in
*"Verification: OK"*) ;;
*) fail "the handshake did not verify: $handshake" ;;
esac
case $handshake in
*"ALPN protocol: h2"*) ;;
*) fail "the handshake did not settle on HTTP/2: $handshake" ;;
esac
passed=$((passed + 1))

echo "policy-image-e2e: $passed checks passed against $image ($tables tables migrated over TLS on $postgres)"
