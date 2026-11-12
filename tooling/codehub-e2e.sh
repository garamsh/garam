#!/bin/sh
# Runs garam's side of "codehub takes garam as its issuer and decision
# point" against a stack of its own and prints what happened. A stand-in
# plays codehub's token client, adapter and outbox: it signs the
# assertions codehub's service principal would, asks garam's issuer for
# tokens, feeds a repository to the policy service and asks it to
# decide. Each step prints its request and the answer's status and
# body, a token shortened to its header, claims and the first 12
# characters of its signature. A step whose answer differs from the one
# the script names stops the run non-zero.
#
# The stack is deploy/local/docker-compose.yml with
# docker-compose.codehub-e2e.yml, built from this checkout, under a
# project of its own so a stack beside it is not touched. The keys are
# made here, in a directory only this user reads, and removed with the
# project's volumes at every exit.
#
# The platform's feed gives the policy service organizations, their
# members and their users, and no role link (ADR-0146 item 4 is
# unbuilt). The run writes the owner's and the member's role links
# itself, through CreatePolicy as the platform, and says so where it
# does. An allow below rests on those links.
#
# CODEHUB_E2E_PROJECT names the compose project.
set -eu

cd "$(dirname "$0")/.."

project=${CODEHUB_E2E_PROJECT:-garam-codehub-e2e}
[ "$project" != garam-local ] || {
	echo "codehub-e2e: the project name garam-local is the dev stack's" >&2
	exit 1
}
issuer=https://garam.localhost/api
checked=0
started=

tmp=$(mktemp -d)
export CODEHUB_E2E_KEYS_DIR=$tmp CODEHUB_E2E_JWKS_FILE=$tmp/jwks.json

compose() {
	docker compose -p "$project" --profile policy \
		-f deploy/local/docker-compose.yml \
		-f deploy/local/docker-compose.codehub-e2e.yml "$@"
}

cleanup() {
	status=$?
	trap - EXIT
	if [ "$status" -ne 0 ] && [ -n "$started" ]; then
		compose logs --tail 30 api machine policy >&2 2>&1 || true
	fi
	compose down -v --remove-orphans >/dev/null 2>&1 || true
	rm -rf "$tmp"
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

fail() {
	echo "codehub-e2e: FAILED: $1" >&2
	exit 1
}

say() { printf '%s\n' "$*"; }

step() { printf '\n== step %s: %s\n' "$1" "$2"; }

# An assertion: what it checks, what came back, what was named.
expect() {
	if [ "$2" = "$3" ]; then
		checked=$((checked + 1))
		say "  ok: $1: $2"
	else
		fail "$1: got '$2', want '$3'"
	fi
}

b64u() { basenc --base64url -w0 | tr -d =; }

# The bytes of a base64url segment, as text and as hex.
b64pad() {
	pad=$(((4 - ${#1} % 4) % 4))
	printf '%s%s' "$1" "$(printf '====' | head -c "$pad")"
}
b64d() { b64pad "$1" | tr '_-' '/+' | base64 -d 2>/dev/null; }
b64hex() { b64d "$1" | basenc --base16 | tr -d '\n'; }

# Every compact token in the text, shortened to what a reader needs.
shorten() {
	text=$(cat)
	for jwt in $(printf '%s' "$text" | grep -oE 'eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*' | sort -u); do
		rest=${jwt#*.}
		short="<jwt header $(b64d "${jwt%%.*}") claims $(b64d "${rest%%.*}") signature $(printf '%s' "${rest#*.}" | cut -c1-12)...>"
		text=$(printf '%s' "$text" | sed "s|$jwt|$short|g")
	done
	printf '%s\n' "$text"
}

# JSON with every credential that is not a signed token masked.
redact() {
	jq -c 'walk(if type == "object" then
		with_entries(if (.key | IN("password", "invitationToken", "token", "device_code", "user_code", "userCode", "refresh_token", "access_token", "subject_token"))
			and (.value | type == "string") and (.value | startswith("eyJ") | not)
			then .value = "<opaque>" else . end)
		else . end)' 2>/dev/null || cat
}

# call METHOD URL [json BODY | form FIELDS]: one request, printed with
# its answer. The status is left in $status and the body in $tmp/body.
# $bearer, $cookie and $curl_extra, where set, ride on it.
bearer=
cookie=
curl_extra=
call() {
	method=$1
	url=$2
	kind=${3:-}
	data=${4:-}
	say "> $method $url"
	set -- -sS -X "$method" -o "$tmp/body" -w '%{http_code}' -H 'Garam-Contract-Version: operation-authority.v1'
	if [ -n "$bearer" ]; then
		set -- "$@" -H "Authorization: Bearer $bearer"
		say "> authorization: Bearer $(printf '%s' "$bearer" | shorten)"
	fi
	if [ -n "$cookie" ]; then
		set -- "$@" -H "Cookie: garam_session=$cookie"
		say "> cookie: garam_session=<opaque>"
	fi
	case $kind in
	json)
		set -- "$@" -H 'Content-Type: application/json' --data "$data"
		printf '> %s\n' "$(printf '%s' "$data" | redact | shorten)"
		;;
	form)
		for field in $data; do
			set -- "$@" --data-urlencode "$field"
			case ${field%%=*} in
			subject_token | device_code) value=$(printf '%s' "${field#*=}" | sed 's/^[^e].*/<opaque>/') ;;
			*) value=${field#*=} ;;
			esac
			printf '> %s=%s\n' "${field%%=*}" "$(printf '%s' "$value" | shorten)"
		done
		;;
	esac
	# shellcheck disable=SC2086
	status=$(curl $curl_extra "$@" "$url") || fail "curl could not reach $url"
	say "< $status"
	if [ -s "$tmp/body" ]; then
		if jq -e . "$tmp/body" >/dev/null 2>&1; then
			redact <"$tmp/body" | shorten | sed 's/^/< /'
		else
			shorten <"$tmp/body" | sed 's/^/< /'
		fi
	fi
}

jget() { jq -r "$1" "$tmp/body"; }

# A compact JWT: KEY HEADER-JSON CLAIMS-JSON.
jwt() {
	h=$(printf '%s' "$2" | b64u)
	c=$(printf '%s' "$3" | b64u)
	s=$(printf '%s.%s' "$h" "$c" | openssl dgst -sha256 -sign "$1" -binary | b64u)
	printf '%s.%s.%s' "$h" "$c" "$s"
}

jti() { openssl rand -base64 24 | tr '+/' '-_' | tr -d =; }

# The RFC 7638 thumbprint of an RSA key, which is the kid garam reads.
modulus() { openssl rsa -in "$1" -noout -modulus | cut -d= -f2 | basenc --base16 -d | b64u; }
thumbprint() {
	printf '{"e":"AQAB","kty":"RSA","n":"%s"}' "$(modulus "$1")" | openssl dgst -sha256 -binary | b64u
}

# The assertion a service principal presents: KEY KID CLIENT-ID.
assertion() {
	now=$(date +%s)
	jwt "$1" "{\"alg\":\"RS256\",\"kid\":\"$2\",\"typ\":\"client-authentication+jwt\"}" \
		"{\"iss\":\"$3\",\"sub\":\"$3\",\"aud\":\"$issuer\",\"iat\":$now,\"exp\":$((now + 30)),\"jti\":\"$(jti)\"}"
}

assertion_type=urn:ietf:params:oauth:client-assertion-type:jwt-bearer

der_len() {
	if [ "$1" -lt 128 ]; then
		printf '%02X' "$1"
	elif [ "$1" -lt 256 ]; then
		printf '81%02X' "$1"
	else
		printf '82%04X' "$1"
	fi
}
der_int() {
	v=$1
	case $v in [89A-F]*) v=00$v ;; esac
	printf '02%s%s' "$(der_len $((${#v} / 2)))" "$v"
}

# Whether the token's signature verifies against the key the JWKS holds
# under the token's kid: TOKEN JWKS-FILE.
verifies() {
	rest=${1#*.}
	claims=${rest%%.*}
	kid=$(b64d "${1%%.*}" | jq -r .kid)
	n=$(jq -r --arg kid "$kid" '.keys[] | select(.kid == $kid) | .n' "$2")
	e=$(jq -r --arg kid "$kid" '.keys[] | select(.kid == $kid) | .e' "$2")
	[ -n "$n" ] || return 1
	body=$(der_int "$(b64hex "$n")")$(der_int "$(b64hex "$e")")
	printf '30%s%s' "$(der_len $((${#body} / 2)))" "$body" | basenc --base16 -d |
		openssl rsa -RSAPublicKey_in -inform DER -pubout -out "$tmp/verify.pem" 2>/dev/null
	b64d "${rest#*.}" >"$tmp/verify.sig"
	printf '%s.%s' "${1%%.*}" "$claims" |
		openssl dgst -sha256 -verify "$tmp/verify.pem" -signature "$tmp/verify.sig" >/dev/null 2>&1
}

# ---------------------------------------------------------------------
# Step 1: the stack, with the issuer, the service keys and the delivery
# on. Compose reaches no running stack of a name this run did not give.

compose down -v --remove-orphans >/dev/null 2>&1 || true
commit=$(git rev-parse HEAD)
dirty=
git diff --quiet HEAD -- . 2>/dev/null || dirty=" (working tree differs from it)"

step 1 "the stack: issuer on, a service key for codehub, delivery on"
say "commit: $commit$dirty"
say "images: built from this checkout by compose; none pulled from ECR"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$tmp/signing.pem" 2>/dev/null
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$tmp/codehub.pem" 2>/dev/null
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$tmp/other.pem" 2>/dev/null
chmod 600 "$tmp"/*.pem
codehub_kid=$(thumbprint "$tmp/codehub.pem")
printf '{"codehub":{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"%s","n":"%s","e":"AQAB"}]}}\n' \
	"$codehub_kid" "$(modulus "$tmp/codehub.pem")" >"$tmp/service-keys.json"
say "keys: made for this run in a directory only this user reads; the service keys file holds codehub's public key, kid $codehub_kid"
: >"$tmp/jwks.json"

started=1
compose up -d --build --wait api machine >"$tmp/up.log" 2>&1 || {
	cat "$tmp/up.log" >&2
	fail "the api and the machine listener did not come up"
}
api=http://127.0.0.1:$(compose port api 8080 | sed 's/.*://')
machine=https://localhost:$(compose port machine 8443 | sed 's/.*://')
compose cp codehub-keys:/keys/machine.crt "$tmp/machine.crt" >/dev/null 2>&1
machine_curl="--cacert $tmp/machine.crt"

# The policy service trusts the keys the running issuer publishes.
curl -fsS "$api/oauth/jwks" >"$tmp/jwks.json" || fail "the issuer served no JWKS"
issuer_kid=$(jq -r '.keys[0].kid' "$tmp/jwks.json")
compose up -d --build --wait policy >"$tmp/up.log" 2>&1 || {
	cat "$tmp/up.log" >&2
	fail "the policy service did not come up"
}
# policy-tls writes a new certificate each time compose starts it, which
# is each time a service that waits on it comes up, and a server reads
# its certificate once. Bring the api up so it has run for the last
# time, then restart both servers onto what it left.
compose up -d --wait api >/dev/null 2>&1 || fail "the api did not come back"
compose restart api policy >/dev/null 2>&1
compose up -d --wait --no-deps api policy >/dev/null 2>&1 || fail "the api and the policy service did not come back"
api=http://127.0.0.1:$(compose port api 8080 | sed 's/.*://')
policy_port=$(compose port policy 8443 | sed 's/.*://')
compose cp policy-tls:/tls/tls.crt "$tmp/policy.crt" >/dev/null 2>&1
policy=https://policy:$policy_port
policy_curl="--cacert $tmp/policy.crt --resolve policy:$policy_port:127.0.0.1"

compose run --rm -T --no-deps api policy-feed start 2>&1 | sed 's/^/  /'
for service in api machine policy; do
	expect "$service is healthy" "$(compose ps --format '{{.Health}}' "$service")" healthy
done
expect "the delivery is on" "$(compose exec -T postgres psql -U garam -d garam -tAc 'SELECT enabled FROM policy_feed')" t

# ---------------------------------------------------------------------
step 2 "an organization with an owner and a member"
say "sign-in here: a session row for the owner, written as"
say "platform-web/e2e/seed.ts writes one; the member registers through"
say "an invitation, as the console's flow does"
owner_token=$(openssl rand -base64 32 | tr '+/' '-_' | tr -d =)
owner_hash=$(printf '%s' "$owner_token" | openssl dgst -sha256 -hex | awk '{print $NF}')
suffix=$(openssl rand -hex 4)
owner_grn=grn:root/iam/user/codehub-e2e-$suffix
compose exec -T postgres psql -U garam -d garam -v ON_ERROR_STOP=1 -q -c \
	"INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ('$owner_grn', 'owner-$suffix', now(), now());
	 INSERT INTO sessions (token_hash, user_grn, created_at, expires_at) VALUES ('$owner_hash', '$owner_grn', now(), now() + interval '1 hour')"
say "wrote: user $owner_grn and its session"

cookie=$owner_token
call POST "$api/orgs" json "{\"name\":\"Codehub e2e $suffix\"}"
expect "organization created" "$status" 201
org_grn=$(jget .grn)
org=${org_grn##*/}
call POST "$api/orgs/$org/invitations" json '{"role":"member"}'
expect "invitation created" "$status" 201
invitation=$(jget .token)
cookie=
member_password=$(openssl rand -hex 16)
call POST "$api/auth/password/register" json \
	"{\"invitationToken\":\"$invitation\",\"username\":\"member-$suffix\",\"password\":\"$member_password\"}"
expect "member registered" "$status" 201
member_grn=$(jget .userGrn)
expect "member's role" "$(jget .role)" member

say "reading back the memberships:"
members=$(compose exec -T postgres psql -U garam -d garam -tAc "SELECT user_grn || ' ' || role FROM org_members WHERE org_grn = '$org_grn' ORDER BY role")
say "$members" | sed 's/^/  /'
expect "memberships" "$(printf '%s' "$members" | tr '\n' ',')" "$member_grn member,$owner_grn owner"

# The feed delivers on a loop of its own: wait for the outbox to empty.
i=0
until [ "$(compose exec -T postgres psql -U garam -d garam -tAc 'SELECT count(*) FROM policy_outbox')" = 0 ]; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || fail "the outbox still held entities after 60s"
	sleep 1
done
say "the platform's delivery loop emptied its outbox into the policy service"

say "what the platform's feed wrote into the policy service:"
policy_state=$(compose exec -T postgres psql -U garam -d policy -tAc \
	"SELECT 'entities ' || count(*) FROM entities UNION ALL SELECT 'policies ' || count(*) FROM policies")
say "$policy_state" | sed 's/^/  /'
expect "no policy came from the feed" "$(printf '%s' "$policy_state" | sed -n 's/^policies //p')" 0

# ---------------------------------------------------------------------
step 3 "discovery and JWKS from the issuer"
call GET "$api/.well-known/openid-configuration"
expect "discovery issuer" "$(jget .issuer)" "$issuer"
call GET "$api/oauth/jwks"
expect "JWKS holds one RS256 key" "$(jget '[.keys[] | select(.alg == "RS256" and .kty == "RSA")] | length')" 1
expect "the policy service trusts that key" "$(jget '.keys[0].kid')" "$issuer_kid"
cp "$tmp/body" "$tmp/issuer-jwks.json"

# ---------------------------------------------------------------------
step 4 "codehub's principal exchanges an assertion for a token"
principal=grn:$org/iam/service/codehub
exchange() {
	curl_extra=$machine_curl
	call POST "$machine/oauth/token" form "grant_type=client_credentials client_id=$1 client_assertion_type=$assertion_type client_assertion=$2 audience=policy"
	curl_extra=
}
exchange "$principal" "$(assertion "$tmp/codehub.pem" "$codehub_kid" "$principal")"
expect "token endpoint status" "$status" 200
codehub_token=$(jget .access_token)
claims=$(b64d "$(printf '%s' "$codehub_token" | cut -d. -f2)")
expect "token aud" "$(printf '%s' "$claims" | jq -r .aud)" policy
expect "token sub" "$(printf '%s' "$claims" | jq -r .sub)" "$principal"
expect "token iss" "$(printf '%s' "$claims" | jq -r .iss)" "$issuer"
if verifies "$codehub_token" "$tmp/issuer-jwks.json"; then
	verdict=verifies
else
	verdict="does not verify"
fi
expect "signature against the JWKS" "$verdict" verifies

# A control: the same check against another key's modulus must fail, or
# the line above proves nothing.
sed "s/\"n\":\"[^\"]*\"/\"n\":\"$(modulus "$tmp/other.pem")\"/" "$tmp/issuer-jwks.json" >"$tmp/wrong-jwks.json"
if verifies "$codehub_token" "$tmp/wrong-jwks.json"; then
	verdict=verifies
else
	verdict="does not verify"
fi
expect "control: signature against a different key" "$verdict" "does not verify"

# ---------------------------------------------------------------------
step 5 "an assertion that must be refused"
say "a nonexistent organization, signed by codehub's key:"
ghost=grn:0000000000000000/iam/service/codehub
exchange "$ghost" "$(assertion "$tmp/codehub.pem" "$codehub_kid" "$ghost")"
expect "status" "$status" 400
expect "error" "$(jget .error)" unauthorized_client
say "the principal of the real organization, signed by another key:"
exchange "$principal" "$(assertion "$tmp/other.pem" "$codehub_kid" "$principal")"
expect "status" "$status" 400
expect "error" "$(jget .error)" invalid_client

# ---------------------------------------------------------------------
step 6 "a repository is fed over HTTP"
repo=grn:$org/codehub/repo/4821
other_repo=grn:0000000000000000/codehub/repo/1
org_ref="{\"type\":\"grn\",\"id\":\"$org_grn\"}"
bearer=$codehub_token
curl_extra=$policy_curl
call POST "$policy/garam/policy/v1/entity-changes" json \
	"{\"changes\":[{\"entity\":{\"type\":\"grn\",\"id\":\"$repo\"},\"sequence\":\"1\",\"upsert\":{\"org\":$org_ref,\"parents\":[$org_ref]}}]}"
expect "status" "$status" 200
expect "written" "$(jget .written)" 1
expect "refusals" "$(jget '.refusals | length')" 0

# ---------------------------------------------------------------------
step 7 "evaluations over HTTP"
ask() {
	jq -nc --arg who "$1" --arg action "$2" --arg what "$3" \
		'{subject: {type: "grn", id: $who}, action: {name: $action}, resource: {type: "grn", id: $what}}'
}
decide() {
	call POST "$policy/access/v1/evaluation" json "$(ask "$1" "$2" "$3")"
	expect "status" "$status" 200
	expect "$2 for ${1##*/} on ${3##*/}" "$(jget .decision)" "$4"
}

say "control: with no role link the member is denied what a member reads"
decide "$member_grn" codehub:repo.read "$repo" false

# The links are not the feed's, so the run writes them as the platform.
platform_token() {
	now=$(date +%s)
	jwt "$tmp/signing.pem" "{\"alg\":\"RS256\",\"kid\":\"$issuer_kid\",\"typ\":\"JWT\"}" \
		"{\"iss\":\"$issuer\",\"aud\":\"policy\",\"sub\":\"platform\",\"iat\":$now,\"exp\":$((now + 120)),\"jti\":\"$(jti)\"}"
}
hex_varint() {
	n=$1
	out=
	while [ "$n" -ge 128 ]; do
		out=$out$(printf '%02X' $((n % 128 + 128)))
		n=$((n / 128))
	done
	printf '%s%02X' "$out" "$n"
}
hex_msg() { printf '%02X%s%s' $(($1 * 8 + 2)) "$(hex_varint $((${#2} / 2)))" "$2"; }
hex_str() { hex_msg "$1" "$(printf '%s' "$2" | basenc --base16 | tr -d '\n')"; }
hex_int() { printf '%02X%s' $(($1 * 8)) "$(hex_varint "$2")"; }

# One gRPC call, hand-framed: METHOD HEX-MESSAGE. Left in $grpc_status.
grpc() {
	body=$2
	printf '00%08X%s' $((${#body} / 2)) "$body" | basenc --base16 -d >"$tmp/frame"
	# shellcheck disable=SC2086
	curl -sS --http2 $policy_curl -H 'content-type: application/grpc' -H 'te: trailers' \
		-H "authorization: Bearer $(platform_token)" --data-binary "@$tmp/frame" \
		-D "$tmp/trailers" -o /dev/null "$policy/garam.policy.v1.PolicyService/$1" ||
		fail "the policy service refused the $1 call"
	grpc_status=$(tr -d '\r' <"$tmp/trailers" | awk 'tolower($1) == "grpc-status:" { print $2 }')
	grpc_message=$(tr -d '\r' <"$tmp/trailers" | awk 'tolower($1) == "grpc-message:" { $1 = ""; print }')
}

link() {
	policy_id=grn:$org/iam/policy/codehub-e2e-$1
	principal_ref=$(hex_str 1 Iam::User)$(hex_str 2 "$3")
	resource_ref=$(hex_str 1 Iam::Org)$(hex_str 2 "$org_grn")
	role_link=$(hex_int 1 "$2")$(hex_int 2 1)$(hex_msg 3 "$principal_ref")$(hex_msg 4 "$resource_ref")
	say "> gRPC PolicyService/CreatePolicy as the platform: role_link $1 for ${3##*/} on the organization (no writer)"
	grpc CreatePolicy "$(hex_str 2 "$org_grn")$(hex_str 3 "$policy_id")$(hex_str 4 "codehub-e2e-$1")$(hex_msg 5 "$(hex_msg 1 "$role_link")")"
	expect "CreatePolicy grpc-status" "$grpc_status" 0
	say "> gRPC PolicyService/ActivatePolicy as the platform: $policy_id"
	grpc ActivatePolicy "$(hex_str 2 "$org_grn")$(hex_str 3 "$policy_id")"
	expect "ActivatePolicy grpc-status" "$grpc_status" 0
}
say "the run writes these two role links itself; the platform does not:"
link owner 1 "$owner_grn"
link member 3 "$member_grn"

bearer=$codehub_token
decide "$member_grn" codehub:org.use "$org_grn" true
decide "$member_grn" codehub:repo.read "$repo" true
decide "$member_grn" codehub:repo.write "$repo" false
decide "$owner_grn" codehub:repo.write "$repo" true

say "a batch of two paths of one repository, answered in order:"
batch() {
	jq -nc --arg who "$member_grn" --arg what "$repo" --arg first "$1" --arg second "$2" \
		'{subject: {type: "grn", id: $who}, resource: {type: "grn", id: $what}, evaluations: [
			{action: {name: $first}, context: {path: "src/a"}},
			{action: {name: $second}, context: {path: "docs/b"}}]}'
}
call POST "$policy/access/v1/evaluations" json "$(batch codehub:repo.read codehub:repo.write)"
expect "status" "$status" 200
expect "read then write" "$(jget '[.evaluations[].decision] | map(tostring) | join(",")')" true,false
call POST "$policy/access/v1/evaluations" json "$(batch codehub:repo.write codehub:repo.read)"
expect "status" "$status" 200
expect "write then read" "$(jget '[.evaluations[].decision] | map(tostring) | join(",")')" false,true

say "a repository of another organization:"
call POST "$policy/access/v1/evaluation" json "$(ask "$member_grn" codehub:repo.read "$other_repo")"
expect "status" "$status" 403
curl_extra=
bearer=

# ---------------------------------------------------------------------
step 8 "a person's token for codehub, introspected by codehub"
say "a CLI session comes from the device grant the garam CLI uses: the"
say "owner's browser session approves the code a CLI would print"
call POST "$api/auth/device" form "client_id=garam-cli"
expect "device code status" "$status" 200
device_code=$(jget .device_code)
user_code=$(jget .user_code)
interval=$(jget .interval)
cookie=$owner_token
call POST "$api/auth/device/approval" json "{\"userCode\":\"$user_code\",\"approve\":true}"
expect "approval status" "$status" 204
cookie=
sleep "$interval"
call POST "$api/auth/device/token" form "grant_type=urn:ietf:params:oauth:grant-type:device_code client_id=garam-cli device_code=$device_code"
expect "CLI session status" "$status" 200
cli_token=$(jget .access_token)

call POST "$api/oauth/token" form "grant_type=urn:ietf:params:oauth:grant-type:token-exchange subject_token=$cli_token subject_token_type=urn:ietf:params:oauth:token-type:access_token audience=codehub"
expect "exchange status" "$status" 200
person_token=$(jget .access_token)
call POST "$api/oauth/token" form "grant_type=urn:ietf:params:oauth:grant-type:token-exchange subject_token=$cli_token subject_token_type=urn:ietf:params:oauth:token-type:access_token audience=policy"
expect "exchange status for another audience" "$status" 200
policy_audience_token=$(jget .access_token)

introspect() {
	curl_extra=$machine_curl
	call POST "$machine/oauth/introspect" form "token=$1 client_id=$principal client_assertion_type=$assertion_type client_assertion=$(assertion "$tmp/codehub.pem" "$codehub_kid" "$principal")"
	curl_extra=
}
introspect "$person_token"
expect "introspection status" "$status" 200
expect "active" "$(jget .active)" true
expect "sub is the person" "$(jget .sub)" "$owner_grn"
expect "aud" "$(jget .aud)" codehub
say "control: a token of another audience is not codehub's to ask about"
introspect "$policy_audience_token"
expect "introspection status" "$status" 200
expect "active" "$(jget .active)" false

echo
echo "codehub-e2e: $checked checks passed at $commit$dirty"
echo "codehub-e2e: the run wrote itself what the platform's feed does not: the owner's and the member's role links (policies, through CreatePolicy as the platform); the sessions and the user of the owner, by SQL"
