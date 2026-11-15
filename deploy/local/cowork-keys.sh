#!/bin/sh
# Writes into /keys what cowork needs to ask the machine listener for a
# caller token, owned by the user the first argument gives: its
# deployment key, readable by that user alone; the service keys file the
# listener checks that key against, which holds its public half under
# the name `cowork`; and the CA file cowork trusts, which holds the
# policy service's certificate and the machine listener's. Neither the
# key nor its public half is printed. They belong to this stack and are
# removed with its volume. /policy.crt and /machine.crt are the two
# certificates. The stack runs this on the alpine image policy-tls
# names, which carries no openssl of its own.
set -eu

owner=${1:?the user that owns the deployment key}

# The CA file follows the certificates it holds, so it is written every
# time, through a rename that a reader never sees half of.
bundle() {
	cat /policy.crt /machine.crt >/keys/ca.crt.new
	chmod 644 /keys/ca.crt.new
	mv /keys/ca.crt.new /keys/ca.crt
}

# Compose runs this again whenever a service that waits on it is brought
# up, and the listener reads the service keys file against the key that
# is there: the key stays. service-keys.json is written last, so its
# presence says the key and the file are whole.
if [ -e /keys/service-keys.json ]; then
	bundle
	exit 0
fi

apk add --no-cache --quiet openssl

rm -f /keys/deployment.pem
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
	-out /keys/deployment.pem 2>/dev/null
chown "$owner" /keys/deployment.pem
chmod 600 /keys/deployment.pem

# The kid is the key's RFC 7638 thumbprint, which the assertion's header
# names.
b64u() { openssl base64 -A | tr '+/' '-_' | tr -d =; }
n=$(openssl rsa -in /keys/deployment.pem -noout -modulus 2>/dev/null |
	cut -d= -f2 | xxd -r -p | b64u)
kid=$(printf '{"e":"AQAB","kty":"RSA","n":"%s"}' "$n" |
	openssl dgst -sha256 -binary | b64u)

bundle
printf '{"cowork":{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"%s","n":"%s","e":"AQAB"}]}}\n' \
	"$kid" "$n" >/keys/service-keys.json
chmod 644 /keys/service-keys.json
