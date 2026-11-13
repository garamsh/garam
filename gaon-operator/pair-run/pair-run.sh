#!/bin/sh
# Runs garam's gaon operator against a real garam and a real control
# service, with a stand-in for gaon's API, and prints what happened. The
# stack is garam's (deploy/local/docker-compose.yml with
# docker-compose.pair-run.yml: the api, the machine listener, the
# authorization engine, postgres) and the control service of
# garamsh/garam-agent-operator at the digest the override pins. The
# operator and the stand-in (tests/gaonstandin, which serves
# tests/gaonfake over mutual TLS) are built from this checkout and run
# on the host. Each step prints its request and the answer's status and
# body, a token shortened to its header, claims and the first 12
# characters of its signature. A step whose answer differs from the one
# the script names stops the run non-zero.
#
# What the run seeds rather than does through a service, and why:
# - the owner's user and session rows, by SQL, as
#   platform-web/e2e/seed.ts and tooling/codehub-e2e.sh seed one: a
#   person signs in through a browser, which a script cannot;
# - the control service's profile, through the image's own
#   publish-profile subcommand, which control.md gives as the way a
#   deployment publishes one;
# - the gaon API's state: the stand-in states a MicroVM's status through
#   its admin path, as a Node Agent would, and the model-key Secret is
#   put through the stand-in's ordinary Secret route, as a person with
#   gaon access would.
# Nothing else is written outside a service's own route.
#
# PAIR_RUN_PROJECT names the compose project. The control image is in a
# private registry: log in first, as the Makefile's comment says.
# Nothing is pushed, tagged or deleted in a registry.
set -eu

cd "$(dirname "$0")/.."
module=$(pwd)
root=$(cd .. && pwd)

project=${PAIR_RUN_PROJECT:-garam-pair-run}
[ "$project" != garam-local ] || {
	echo "pair-run: the project name garam-local is the dev stack's" >&2
	exit 1
}
checked=0
started=
operator_pid=
standin_pid=

tmp=$(mktemp -d)
mkdir "$tmp/pub" "$tmp/bin"
chmod 755 "$tmp/pub"
export PAIR_RUN_KEYS_DIR=$tmp PAIR_RUN_PUBLIC_DIR=$tmp/pub

compose() {
	docker compose -p "$project" --profile profile \
		-f "$root/deploy/local/docker-compose.yml" \
		-f "$root/deploy/local/docker-compose.pair-run.yml" "$@"
}

cleanup() {
	status=$?
	trap - EXIT
	if [ "$status" -ne 0 ]; then
		for log in operator standin; do
			[ ! -s "$tmp/$log.log" ] || {
				echo "pair-run: last lines of the $log log:" >&2
				tail -n 25 "$tmp/$log.log" >&2
			}
		done
		if [ -n "$started" ]; then
			compose logs --tail 25 api machine control >&2 2>&1 || true
		fi
	fi
	[ -z "$operator_pid" ] || kill "$operator_pid" 2>/dev/null || true
	[ -z "$standin_pid" ] || kill "$standin_pid" 2>/dev/null || true
	compose down -v --remove-orphans --rmi local >/dev/null 2>&1 || true
	rm -rf "$tmp"
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

fail() {
	echo "pair-run: FAILED: $1" >&2
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

b64pad() {
	pad=$(((4 - ${#1} % 4) % 4))
	printf '%s%s' "$1" "$(printf '====' | head -c "$pad")"
}
b64d() { b64pad "$1" | tr '_-' '/+' | base64 -d 2>/dev/null; }

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

# A credential that is not a compact token: its first 12 characters.
mask() {
	case $1 in
	eyJ*.*.*) printf '%s' "$1" | shorten ;;
	*) printf '%s...' "$(printf '%s' "$1" | cut -c1-12)" ;;
	esac
}

# JSON with every credential that is not a signed token masked, and the
# data of a Secret replaced by what it is.
redact() {
	jq -c 'walk(if type == "object" and ((.data? | type) == "object") then .data |= map_values("<redacted>") else . end)
		| walk(if type == "object" then
		with_entries(if (.key | IN("password", "privateKeyPem", "token", "authority", "operationRef", "access_token", "client_assertion"))
			and (.value | type == "string") and (.value | startswith("eyJ") | not)
			then .value = "<opaque>" else . end)
		else . end)' 2>/dev/null || cat
}

# call METHOD URL [json BODY | form FIELDS]: one request, printed with
# its answer. The status is left in $status and the body in $tmp/body.
# $bearer, $authorization, $cookie, $extra_header and $curl_extra, where
# set, ride on it.
bearer=
authorization=
cookie=
extra_header=
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
		say "> authorization: Bearer $(mask "$bearer")"
	fi
	if [ -n "$authorization" ]; then
		set -- "$@" -H "Authorization: Garam-Operation $authorization"
		say "> authorization: Garam-Operation $(mask "$authorization")"
	fi
	if [ -n "$extra_header" ]; then
		set -- "$@" -H "$extra_header"
		say "> $extra_header"
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
			client_assertion) value=$(mask "${field#*=}") ;;
			*) value=${field#*=} ;;
			esac
			printf '> %s=%s\n' "${field%%=*}" "$value"
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

jti() { openssl rand -base64 24 | tr '+/' '-_' | tr -d =; }

# The RFC 7638 thumbprint of an RSA key, which is the kid garam reads.
modulus() { openssl rsa -in "$1" -noout -modulus | cut -d= -f2 | basenc --base16 -d | b64u; }
thumbprint() {
	printf '{"e":"AQAB","kty":"RSA","n":"%s"}' "$(modulus "$1")" | openssl dgst -sha256 -binary | b64u
}

# The assertion an operator presents: KEY KID CLIENT-ID AUDIENCE.
assertion() {
	now=$(date +%s)
	h=$(printf '{"alg":"RS256","kid":"%s","typ":"JWT"}' "$2" | b64u)
	c=$(printf '{"iss":"%s","sub":"%s","aud":"%s","iat":%s,"exp":%s,"jti":"%s"}' "$3" "$3" "$4" "$now" "$((now + 30))" "$(jti)" | b64u)
	s=$(printf '%s.%s' "$h" "$c" | openssl dgst -sha256 -sign "$1" -binary | b64u)
	printf '%s.%s.%s' "$h" "$c" "$s"
}

assertion_type=urn:ietf:params:oauth:client-assertion-type:jwt-bearer

sha256hex() { printf '%s' "$1" | openssl dgst -sha256 -hex | awk '{print $NF}'; }

# wait_for WHAT SECONDS COMMAND...: tries COMMAND once a second.
wait_for() {
	what=$1
	limit=$2
	shift 2
	i=0
	until "$@" >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt "$limit" ] || fail "$what: not within ${limit}s"
		sleep 1
	done
}

# ---------------------------------------------------------------------
# Step 1: the keys, the stack, the owner's session.

compose down -v --remove-orphans >/dev/null 2>&1 || true
commit=$(git rev-parse HEAD)
dirty=
git diff --quiet HEAD -- .. 2>/dev/null || dirty=" (working tree differs from it)"

step 1 "the stack: garam's api, authorization engine and machine listener, the control service, its database"
say "commit: $commit$dirty"
say "images: garam's built from this checkout by compose; the control service's pulled at the digest the override pins"

# The machine listener's certificate names `machine` for the services in
# the stack and `localhost` for the operator on the host, and is its own
# root. The control service's names `control`, `localhost`, 127.0.0.1.
selfsigned() {
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 2 -subj "/CN=$1" \
		-addext "subjectAltName=$2" -addext "basicConstraints=critical,CA:TRUE" \
		-keyout "$tmp/$3.key" -out "$tmp/$3.crt" 2>/dev/null
}
selfsigned machine "DNS:machine,DNS:localhost" machine
selfsigned control "DNS:control,DNS:localhost,IP:127.0.0.1" control-api
openssl rand -hex 32 >"$tmp/kek.hex"

# gaon's CA signs the stand-in's certificate, which names localhost, and
# the operator's client certificate.
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 2 -subj "/CN=pair-run gaon CA" \
	-addext "basicConstraints=critical,CA:TRUE" -keyout "$tmp/gaon-ca.key" -out "$tmp/gaon-ca.crt" 2>/dev/null
issue_gaon() {
	openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$1" \
		-keyout "$tmp/$2.key" -out "$tmp/$2.csr" 2>/dev/null
	printf '%s\n' "$3" >"$tmp/$2.ext"
	openssl x509 -req -in "$tmp/$2.csr" -CA "$tmp/gaon-ca.crt" -CAkey "$tmp/gaon-ca.key" -CAcreateserial \
		-days 2 -extfile "$tmp/$2.ext" -out "$tmp/$2.crt" 2>/dev/null
}
issue_gaon localhost gaon-server "subjectAltName=DNS:localhost,IP:127.0.0.1"
issue_gaon gaon-operator operator-client "extendedKeyUsage=clientAuth"
chmod 600 "$tmp"/*.key
say "keys: made for this run in a directory only this user reads, removed with the run"

started=1
compose up -d --build --wait api machine >"$tmp/up.log" 2>&1 || {
	cat "$tmp/up.log" >&2
	fail "the api and the machine listener did not come up"
}
api=http://127.0.0.1:$(compose port api 8080 | sed 's/.*://')
machine=https://localhost:$(compose port machine 8443 | sed 's/.*://')
machine_curl="--cacert $tmp/machine.crt"
for service in api machine; do
	expect "$service is healthy" "$(compose ps --format '{{.Health}}' "$service")" healthy
done

# ---------------------------------------------------------------------
step 2 "an organization, its manager and control service registered as an owner would"
say "sign-in here: a user row and a session row for the owner, written as"
say "platform-web/e2e/seed.ts writes them; everything after goes through garam's routes"
owner_token=$(openssl rand -base64 32 | tr '+/' '-_' | tr -d =)
owner_hash=$(sha256hex "$owner_token")
suffix=$(openssl rand -hex 4)
owner_grn=grn:root/iam/user/pair-run-$suffix
compose exec -T postgres psql -U garam -d garam -v ON_ERROR_STOP=1 -q -c \
	"INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ('$owner_grn', 'owner-$suffix', now(), now());
	 INSERT INTO sessions (token_hash, user_grn, created_at, expires_at) VALUES ('$owner_hash', '$owner_grn', now(), now() + interval '1 hour')"
say "wrote: user $owner_grn and its session"

cookie=$owner_token
call POST "$api/orgs" json "{\"name\":\"Pair run $suffix\"}"
expect "organization created" "$status" 201
org_grn=$(jget .grn)
org=${org_grn##*/}
call POST "$api/orgs/$org/operators" json '{"identifier":"gaon","name":"gaon operator","controlService":{"identifier":"gaon-control","name":"gaon control service"}}'
expect "manager and control service registered" "$status" 201
manager_grn=$(jget .grn)
control_grn=grn:$org/iam/operator/gaon-control
expect "manager's GRN" "$manager_grn" "grn:$org/iam/operator/gaon"

issue_key() {
	call POST "$api/orgs/$org/operators/$1/keys" json '{"rotation":"immediate"}'
	expect "key issued to $1" "$status" 201
	jget .privateKeyPem >"$tmp/$2.key"
	chmod 600 "$tmp/$2.key"
	jget .clientId >"$tmp/$2-client-id"
	jget .tokenUrl >"$tmp/$2-token-url"
	expect "$1's client id" "$(cat "$tmp/$2-client-id")" "grn:$org/iam/operator/$1"
}
issue_key gaon operator
issue_key gaon-control control-operator
token_url=$(cat "$tmp/operator-token-url")
say "token URL: $token_url"
# The control service reads these as files, and the copy in its volume
# is made at its start without a trailing newline to remove.
printf '%s' "$(cat "$tmp/control-operator-client-id")" >"$tmp/control-client-id"
printf '%s' "$(cat "$tmp/control-operator-token-url")" >"$tmp/control-token-url"
mv "$tmp/control-operator.key" "$tmp/control-operator.key.issued"
cp "$tmp/control-operator.key.issued" "$tmp/control-operator.key"
chmod 600 "$tmp/control-operator.key"

expires=$(date -u -d '+1 day' +%Y-%m-%dT%H:%M:%SZ)
delegation=$(jq -nc --arg id "pair-run-delegation-$suffix" --arg manager "$manager_grn" --arg exp "$expires" \
	'{requestId: $id, controllers: [$manager], operations: ["iam:agent.create", "iam:agent.configure", "iam:agent.activate",
	"iam:agent.recover", "iam:agent-template.read", "iam:agent-template.publish", "iam:execution-profile.read",
	"iam:agent.execution-read", "iam:operator.read"], expiresAt: $exp}')
call PUT "$api/orgs/$org/operators/gaon-control/delegation" json "$delegation"
expect "delegation written" "$status" 201
cookie=

# ---------------------------------------------------------------------
step 3 "the control service, its profile, and the console's view of an owner's authority"
compose up -d --wait control >"$tmp/up.log" 2>&1 || {
	cat "$tmp/up.log" >&2
	fail "the control service did not come up"
}
control=https://localhost:$(compose port control 8080 | sed 's/.*://')
control_health=http://127.0.0.1:$(compose port control 8081 | sed 's/.*://')
control_curl="--cacert $tmp/control-api.crt"
control_ready() { curl -fsS "$control_health/readyz"; }
wait_for "the control service's readiness" 60 control_ready
say "the control service is ready: it migrated its schema and reached garam"

cat >"$tmp/pub/profile.yaml" <<EOF
organization: $org
name: standard
version: 1
resources:
  requests: {cpu: 500m, memory: 512Mi}
  limits: {cpu: "1", memory: 1Gi}
storageSize: 1Gi
workspaceStorageSize: 1Gi
EOF
chmod 644 "$tmp/pub/profile.yaml"
say "> control publish-profile --file profile.yaml (the image's subcommand, run once)"
compose run --rm -T --no-deps control-profile >"$tmp/profile.log" 2>&1 || {
	cat "$tmp/profile.log" >&2
	fail "publish-profile was refused"
}
sed 's/^/< /' "$tmp/profile.log"
checked=$((checked + 1))
say "  ok: the profile standard v1 is published"

# mint OPERATION TARGET REQUEST-ID BODY [REQUEST-TARGET]: the owner has
# garam mint an authority over exactly BODY for the control service.
mint() {
	req=$(jq -nc --arg a "$control_grn" --arg o "$1" --arg t "$2" --arg r "$3" --arg d "$(sha256hex "$4")" --arg rt "${5:-}" \
		'{audience: $a, operation: $o, target: $t, requestId: $r, bodySha256: $d} + (if $rt == "" then {} else {requestTarget: $rt} end)')
	cookie=$owner_token
	call POST "$api/orgs/$org/operation-authorities" json "$req"
	cookie=
	expect "authority for $1" "$status" 201
	authority=$(jget .authority)
}

# ---------------------------------------------------------------------
step 4 "gaon's stand-in, and a model key for the organization"
( cd "$module" && go build -o "$tmp/bin/gaonstandin" ./tests/gaonstandin && go build -o "$tmp/bin/gaon-operator" ./cmd/gaon-operator ) ||
	fail "the stand-in and the operator did not build"
"$tmp/bin/gaonstandin" -addr 127.0.0.1:0 -addr-file "$tmp/gaon.addr" -cert-file "$tmp/gaon-server.crt" \
	-key-file "$tmp/gaon-server.key" -client-ca-file "$tmp/gaon-ca.crt" >"$tmp/standin.log" 2>&1 &
standin_pid=$!
addr_ready() { [ -s "$tmp/gaon.addr" ]; }
wait_for "the stand-in's address" 20 addr_ready
gaon=https://localhost:$(sed 's/.*://' "$tmp/gaon.addr")
gaon_curl="--cacert $tmp/gaon-ca.crt --cert $tmp/operator-client.crt --key $tmp/operator-client.key"
say "stand-in: tests/gaonfake over mutual TLS at $gaon"

org_label=$(sha256hex "$org_grn" | cut -c1-32)
model_key=sk-pair-run-$(openssl rand -hex 12)
secret=$(jq -nc --arg org "$org_label" --arg key "$(printf '%s' "$model_key" | base64 -w0)" \
	'{apiVersion: "compute.gaon.garam.sh/v1alpha1", kind: "Secret",
	metadata: {name: "model-keys", labels: {"garam.sh/role": "model-key", "garam.sh/organization": $org}},
	data: {web: $key}}')
curl_extra=$gaon_curl
call PUT "$gaon/secrets/model-keys" json "$secret"
expect "model-key Secret stored" "$status" 200
curl_extra=

# ---------------------------------------------------------------------
step 5 "an agent is created through the control service's console route"
template_body=$(jq -nc --arg id "pair-run-publish-$suffix" \
	'{requestId: $id, profile: {name: "standard", version: 1}, configuration: {
	model: {provider: "anthropic", baseUrl: "https://api.anthropic.com", name: "claude-opus-5-5", apiKeyRef: "model-keys/web",
	embedding: {baseUrl: "https://embeddings.example/v1", name: "bge-base-en-v1.5"}},
	ego: "Answers the questions of the pair run.", tools: {}}}')
publish_path=/v1/orgs/$org/templates/pair-run/versions
mint iam:agent-template.publish "$org_grn" "pair-run-publish-$suffix" "$template_body" "$publish_path"
authorization=$authority
curl_extra=$control_curl
call POST "$control$publish_path" json "$template_body"
authorization=
curl_extra=
expect "template published" "$status" 201

create_body=$(jq -nc --arg id "pair-run-create-$suffix" --arg manager "$manager_grn" \
	'{requestId: $id, controller: $manager, template: {name: "pair-run", version: 1}, profile: {name: "standard", version: 1}}')
mint iam:agent.create "$manager_grn" "pair-run-create-$suffix" "$create_body"
authorization=$authority
curl_extra=$control_curl
call POST "$control/v1/orgs/$org/agents" json "$create_body"
authorization=
curl_extra=
expect "agent created" "$status" 201
agent_grn=$(jget .agent)
epoch=$(jget .epoch)
expect "revision" "$(jget .revision)" 1
agent_id=${agent_grn##*/}
name=agent-$(sha256hex "$agent_grn" | cut -c1-16)
say "agent: $agent_grn at epoch $epoch; its objects in gaon are named $name"

cookie=$owner_token
call GET "$api/orgs/$org/agents/$agent_id"
cookie=
expect "garam reads the agent" "$status" 200
expect "garam's view before any report" "$(jget .provisioningState)" pending

# ---------------------------------------------------------------------
step 6 "the manager reads the feed as the operator does: the feed holds the agent"
now_assertion=$(assertion "$tmp/operator.key" "$(thumbprint "$tmp/operator.key")" "$manager_grn" "$token_url")
curl_extra=$machine_curl
call POST "$machine/oauth/token" form "grant_type=client_credentials client_id=$manager_grn client_assertion_type=$assertion_type client_assertion=$now_assertion audience=$control_grn"
curl_extra=
expect "token exchange for the control service" "$status" 200
manager_token=$(jget .access_token)
bearer=$manager_token
extra_header="Controller-Grn: $manager_grn"
curl_extra=$control_curl
call GET "$control/v1/operators/self/desired?waitSeconds=0"
bearer=
extra_header=
curl_extra=
expect "feed status" "$status" 200
expect "the feed holds the agent" "$(jq -r --arg g "$agent_grn" '[.agents[].agent] | index($g) != null' "$tmp/body")" true
expect "the feed's epoch" "$(jq -r --arg g "$agent_grn" '.agents[] | select(.agent == $g) | .epoch' "$tmp/body")" "$epoch"

# ---------------------------------------------------------------------
step 7 "gaon-operator, built from this checkout, runs as the registered manager"
env GAON_API_URL="$gaon" GAON_CA_FILE="$tmp/gaon-ca.crt" GAON_CLIENT_CERT_FILE="$tmp/operator-client.crt" \
	GAON_CLIENT_KEY_FILE="$tmp/operator-client.key" GARAM_MACHINE_URL="$machine" GARAM_ROOT_FILE="$tmp/machine.crt" \
	GARAM_TOKEN_URL="$token_url" CONTROL_URL="$control" CONTROL_ROOT_FILE="$tmp/control-api.crt" \
	CONTROL_GRN="$control_grn" OPERATOR_KEY_FILE="$tmp/operator.key" OPERATOR_CLIENT_ID="$manager_grn" \
	AGENT_IMAGE=gaon-guest:pair-run HEALTH_ADDR=127.0.0.1:0 \
	"$tmp/bin/gaon-operator" >"$tmp/operator.log" 2>&1 &
operator_pid=$!
say "gaon-operator started as $manager_grn"

registered() { grep -qF "registered the agent's placement" "$tmp/operator.log"; }
wait_for "the placement's registration" 120 registered
say "the operator's log:"
sed 's/^/  /' "$tmp/operator.log"
checked=$((checked + 1))
say "  ok: the control service answered 201 to the placement (the operator logs 'registered the agent's placement' for a 201, and 'already registered' for a 200)"

owner_state() {
	curl -fsS -H "Cookie: garam_session=$owner_token" "$api/orgs/$org/agents/$agent_id" | jq -r .provisioningState
}
state_is() { [ "$(owner_state)" = "$1" ]; }
wait_for "garam's view of the agent to read provisioned" 60 state_is provisioned
cookie=$owner_token
call GET "$api/orgs/$org/agents/$agent_id"
cookie=
expect "garam's view after the operator's report" "$(jget .provisioningState)" provisioned
say "(the operator's report to garam is what moved that state; the operator does not print the 204)"

# ---------------------------------------------------------------------
step 8 "what the operator wrote into gaon's API, read from the stand-in"
curl_extra=$gaon_curl
call GET "$gaon/secrets/$name"
expect "secret read" "$status" 200
expect "secret's role label" "$(jget '.metadata.labels["garam.sh/role"]')" agent
expect "secret's organization label" "$(jget '.metadata.labels["garam.sh/organization"]')" "$org_label"
expect "secret's keys" "$(jget '.data | keys | join(",")')" "agent-token,control-root.pem,control-token,garam-root.pem,placement-token"
credential_values=$(jq -r '.data | to_entries[] | select(.key | IN("agent-token", "control-token", "placement-token")) | .value' "$tmp/body" |
	while read -r v; do printf '%s\n' "$(printf '%s' "$v" | base64 -d)"; done)
expect "three credentials were read from the secret" "$(printf '%s\n' "$credential_values" | grep -c .)" 3

call GET "$gaon/volumes/$name-memory"
expect "memory volume read" "$status" 200
call GET "$gaon/volumes/$name-workspace"
expect "workspace volume read" "$status" 200

call GET "$gaon/microvms/$name"
expect "microvm read" "$status" 200
curl_extra=
expect "microvm's user" "$(jget .spec.user)" 0:0
expect "microvm's run strategy" "$(jget .spec.runStrategy)" Always
expect "microvm's volumes" "$(jget '[.spec.volumes[].name] | sort | join(",")')" "$name-memory,$name-workspace"
expect "microvm's files" "$(jget '[.spec.files[].path] | sort | join(",")')" \
	"/run/garam/agent-token,/run/garam/control-root.pem,/run/garam/control-token,/run/garam/garam-root.pem,/run/garam/placement-token,/run/garam/runtime-config.yaml,/run/garam/runtime-ego.md"
expect "microvm's credential files come from the secret, mode 0400, owned by 65532" \
	"$(jget '[.spec.files[] | select(.valueFrom.secretKeyRef.name == "'"$name"'") | "\(.mode)/\(.owner.uid)"] | unique | join(",")')" 256/65532
expect "microvm's model key is the Secret's, by reference" \
	"$(jget '.spec.env[] | select(.name == "MODEL_API_KEY") | "\(.valueFrom.secretKeyRef.name)/\(.valueFrom.secretKeyRef.key) value=\(.value // "none")"')" "model-keys/web value=none"
leaks=0
for value in $credential_values "$model_key"; do
	if grep -qF -- "$value" "$tmp/body"; then
		leaks=$((leaks + 1))
	fi
done
expect "no credential, and not the model key, is inline in the microvm" "$leaks" 0

puts() { grep -c "method=PUT path=$1" "$tmp/standin.log" || true; }
expect "PUTs of the agent's volumes" "$(puts "/volumes/$name-")" 2
expect "PUTs of the agent's secret" "$(puts "/secrets/$name")" 1
expect "PUTs of the agent's microvm" "$(puts "/microvms/$name")" 1
say "the stand-in's request log:"
sed 's/^/  /' "$tmp/standin.log"

# ---------------------------------------------------------------------
step 9 "the stand-in reports the MicroVM Running and Ready, as a Node Agent would"
curl_extra=$gaon_curl
call PUT "$gaon/_admin/microvms/$name/status" json '{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}'
curl_extra=
expect "status stored" "$status" 204
wait_for "garam's view of the agent to read ready" 120 state_is ready
cookie=$owner_token
call GET "$api/orgs/$org/agents/$agent_id"
cookie=
expect "garam's view after the operator's second report" "$(jget .provisioningState)" ready

# ---------------------------------------------------------------------
step 10 "the agent is stopped through the control service; the operator halts the MicroVM and deletes nothing"
stop_body=$(jq -nc --arg id "pair-run-stop-$suffix" '{requestId: $id}')
mint iam:agent.configure "$agent_grn" "pair-run-stop-$suffix" "$stop_body"
authorization=$authority
curl_extra=$control_curl
call POST "$control/v1/orgs/$org/agents/$(printf '%s' "$agent_grn" | sed 's|/|%2F|g')/stop" json "$stop_body"
authorization=
curl_extra=
expect "stop status" "$status" 200
expect "stopped" "$(jget .stopped)" true

curl_extra=$gaon_curl
halted() {
	curl -fsS $gaon_curl "$gaon/microvms/$name" | jq -e '.spec.runStrategy == "Halted"'
}
wait_for "the microvm to read Halted" 120 halted
call GET "$gaon/microvms/$name"
expect "microvm's run strategy" "$(jget .spec.runStrategy)" Halted
for object in "secrets/$name" "volumes/$name-memory" "volumes/$name-workspace"; do
	call GET "$gaon/$object"
	expect "$object is still there" "$status" 200
done
curl_extra=
expect "DELETEs the stand-in served" "$(grep -c 'method=DELETE' "$tmp/standin.log" || true)" 0

echo
echo "pair-run: $checked checks passed at $commit$dirty"
echo "pair-run: seeded rather than done through a service: the owner's user and session rows (SQL); the control service's profile (its own publish-profile subcommand); gaon's API state (the stand-in: the model-key Secret through its Secret route, the MicroVM's status through its admin path)"
