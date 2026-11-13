#!/bin/sh
# Writes into /keys the key the api signs the policy service's tokens
# with and the JWKS the policy service trusts for it, owned by the user
# its first argument gives. signing.pem is readable by that user alone;
# jwks.json is public. Neither is printed. The key and the JWKS belong
# to this stack and are removed with its volume. The stack runs this on
# the alpine image policy-tls names, which carries no openssl of its
# own.
set -eu

owner=${1:?the user that owns the signing key}

# Compose runs this again whenever a service that waits on it is brought
# up, and a server holds the key and the JWKS it started with: what is
# there stays. jwks.json is written last, so its presence says both
# files are whole.
[ ! -e /keys/jwks.json ] || exit 0

apk add --no-cache --quiet openssl

rm -f /keys/signing.pem
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
	-out /keys/signing.pem 2>/dev/null
chown "$owner" /keys/signing.pem
chmod 600 /keys/signing.pem

# The kid is the key's RFC 7638 thumbprint, which is the name the api
# gives its tokens' key.
b64u() { openssl base64 -A | tr '+/' '-_' | tr -d =; }
n=$(openssl rsa -in /keys/signing.pem -noout -modulus 2>/dev/null |
	cut -d= -f2 | xxd -r -p | b64u)
kid=$(printf '{"e":"AQAB","kty":"RSA","n":"%s"}' "$n" |
	openssl dgst -sha256 -binary | b64u)

printf '{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"%s","n":"%s","e":"AQAB"}]}\n' \
	"$kid" "$n" >/keys/jwks.json
chmod 644 /keys/jwks.json
