#!/bin/sh
# Generates the key material the policy service and its callers need and
# puts it in a Secret: the certificate the service serves under, which
# the callers trust as their CA, the key the api and the machine
# listener sign their calls with, and the JWKS the service trusts for
# it. All of it is throwaway local material, the counterpart of
# machine-key-material.sh and of policy-tls.sh and policy-keys.sh in
# deploy/local, which make the same pair for the compose stack.
#
# Nothing generated here is written into the repository. The files exist
# in a private temporary directory for as long as `kubectl create` takes
# to read them, and the trap removes them on every exit path.
set -eu

namespace=garam-local
secret=policy-throwaway-key-material
kubectl="kubectl --context minikube -n $namespace"

# A Secret that holds the pair is kept whole: the service holds the JWKS
# it started with, and a new signing key beside it would leave every
# call refused until the service restarted.
if $kubectl get secret "$secret" -o jsonpath='{.data.jwks\.json}' 2>/dev/null | grep -q .; then
  echo "$secret: present, kept"
  exit 0
fi

dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT INT TERM

# The names are the ones the Service answers to inside the cluster. The
# certificate signs itself and is its own root, as in compose.
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -days 30 -subj "/CN=policy" \
  -addext "subjectAltName=DNS:policy,DNS:policy.garam-local.svc,DNS:policy.garam-local.svc.cluster.local" \
  -addext "basicConstraints=critical,CA:FALSE" \
  -keyout "$dir/tls.key" -out "$dir/tls.crt" 2>/dev/null

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
  -out "$dir/signing.pem" 2>/dev/null

# The kid is the key's RFC 7638 thumbprint, which is the name the api
# gives its tokens' key. The modulus is the 256 bytes before the
# 3-byte public exponent (the INTEGER's 5 bytes with its tag and
# length) that close a 2048-bit key's DER public key.
b64u() { openssl base64 -A | tr '+/' '-_' | tr -d =; }
n=$(openssl pkey -in "$dir/signing.pem" -pubout -outform DER 2>/dev/null |
  tail -c 261 | head -c 256 | b64u)
[ -n "$n" ] || { echo "policy-key-material: empty modulus" >&2; exit 1; }
kid=$(printf '{"e":"AQAB","kty":"RSA","n":"%s"}' "$n" |
  openssl dgst -sha256 -binary | b64u)
printf '{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"%s","n":"%s","e":"AQAB"}]}\n' \
  "$kid" "$n" >"$dir/jwks.json"

$kubectl create secret generic "$secret" \
  --from-file="$dir/tls.crt" \
  --from-file="$dir/tls.key" \
  --from-file="$dir/signing.pem" \
  --from-file="$dir/jwks.json" \
  --dry-run=client -o yaml | $kubectl apply -f -
echo "$secret: generated 1 certificate, 1 signing key and its JWKS"
