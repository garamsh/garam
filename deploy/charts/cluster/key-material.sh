#!/bin/sh
# Produces the key material a garam cluster deployment names, as files
# in a new directory and nowhere else: nothing is printed but file
# names, modes and fingerprints. It is the cluster counterpart of
# deploy/local/minikube/machine-key-material.sh and
# policy-key-material.sh, which write throwaway material straight into a
# Secret; this writes material meant to be kept, and puts it nowhere
# itself.
#
# usage: key-material.sh <new-directory> [options]
#   --fullname <name>      the chart's fullname (default garam); the
#                          release name when it equals the chart name,
#                          else <release>-garam
#   --namespace <name>     the namespace the release is installed in
#                          (default garam)
#   --machine-host <host>  the name callers reach the machine listener
#                          by from outside the cluster
#                          (default machine.garam.garam.sh)
#   --days <n>             validity of the certificates (default 365)
#
# The directory must not exist. A run never writes into a directory that
# holds a key-encryption key, so it cannot replace one in place: every
# message signing key sealed under it opens under no other
# (docs/architecture/deployment-contract.md §The key-encryption key
# cannot be regenerated).
set -eu

fullname=garam
namespace=garam
machine_host=machine.garam.garam.sh
days=365

usage() {
  sed -n '/^# usage:/,/^# *(default 365)/p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

out=${1:-}
[ -n "$out" ] || usage
shift
while [ $# -gt 0 ]; do
  [ $# -ge 2 ] || usage
  case $1 in
  --fullname) fullname=$2 ;;
  --namespace) namespace=$2 ;;
  --machine-host) machine_host=$2 ;;
  --days) days=$2 ;;
  *) usage ;;
  esac
  shift 2
done
case $days in
'' | *[!0-9]*) usage ;;
esac

if [ -e "$out" ]; then
  echo "key-material: $out exists, and a key-encryption key already there must not be replaced in place; name a new directory" >&2
  exit 1
fi

umask 077
mkdir -p "$out"
cd "$out"
mkdir garam-machine-key-material garam-policy-tls garam-policy-ca garam-policy-jwks garam-policy-signing-key offline

# A key-encryption key is 32 bytes, hex-encoded
# (platform/internal/ca).
openssl rand -hex 32 >garam-machine-key-material/ca-kek.hex

# One server root signs the machine listener's certificate and nothing
# else (ADR-0040). Its key is written under offline/ and belongs where
# garam cannot reach it: a leaf is renewed by signing a new one with it,
# and nothing in the cluster reads it.
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out offline/server-root.key 2>/dev/null
openssl req -x509 -new -key offline/server-root.key -days "$days" \
  -subj "/CN=garam server root ($namespace)" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
  -addext "keyUsage=critical,keyCertSign" \
  -out garam-machine-key-material/server-root.pem 2>/dev/null

# The names are the one a caller outside the cluster reaches the
# listener by, and the Service's names inside it.
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out garam-machine-key-material/tls.key 2>/dev/null
openssl req -new -key garam-machine-key-material/tls.key \
  -subj "/CN=$machine_host" -out offline/machine.csr 2>/dev/null
openssl x509 -req -in offline/machine.csr -days "$days" \
  -CA garam-machine-key-material/server-root.pem -CAkey offline/server-root.key \
  -set_serial 1 -extfile /dev/stdin \
  -out garam-machine-key-material/tls.crt 2>/dev/null <<EOT
subjectAltName=DNS:$machine_host,DNS:$fullname-machine,DNS:$fullname-machine.$namespace.svc,DNS:$fullname-machine.$namespace.svc.cluster.local
extendedKeyUsage=serverAuth
EOT
rm offline/machine.csr

# The policy service's certificate names the Service host the platform
# dials, and signs itself: it is its own root, so the callers' CA is the
# same file (chart README §The policy service).
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -days "$days" -subj "/CN=$fullname-policy" \
  -addext "subjectAltName=DNS:$fullname-policy,DNS:$fullname-policy.$namespace.svc,DNS:$fullname-policy.$namespace.svc.cluster.local" \
  -addext "basicConstraints=critical,CA:FALSE" \
  -keyout garam-policy-tls/tls.key -out garam-policy-tls/tls.crt 2>/dev/null
cp garam-policy-tls/tls.crt garam-policy-ca/ca.crt

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
  -out garam-policy-signing-key/signing.pem 2>/dev/null

# The kid is the key's RFC 7638 thumbprint. The modulus is the 256 bytes
# before the 3-byte public exponent that close a 2048-bit key's DER
# public key.
b64u() { openssl base64 -A | tr '+/' '-_' | tr -d =; }
n=$(openssl pkey -in garam-policy-signing-key/signing.pem -pubout -outform DER 2>/dev/null |
  tail -c 261 | head -c 256 | b64u)
[ -n "$n" ] || {
  echo "key-material: empty modulus" >&2
  exit 1
}
kid=$(printf '{"e":"AQAB","kty":"RSA","n":"%s"}' "$n" |
  openssl dgst -sha256 -binary | b64u)
printf '{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"%s","n":"%s","e":"AQAB"}]}\n' \
  "$kid" "$n" >garam-policy-jwks/jwks.json

# What a run reports: each file's mode and a fingerprint that is not a
# value. A certificate is fingerprinted as a certificate, a private key
# by the SHA-256 of its public key, the JWKS by its own SHA-256, and the
# key-encryption key by the first 16 hex digits of its SHA-256, enough
# to tell a restored one from another and too few to help recover it.
mode() { stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1"; }
cert() { openssl x509 -in "$1" -noout -fingerprint -sha256 | sed 's/.*=//'; }
pub() { openssl pkey -in "$1" -pubout -outform DER 2>/dev/null | openssl dgst -sha256 | sed 's/.*= //'; }
sha() { openssl dgst -sha256 "$1" | sed 's/.*= //'; }
count=0
report() {
  printf '%s %s %s\n' "$(mode "$1")" "$2" "$1"
  count=$((count + 1))
}
report garam-machine-key-material/ca-kek.hex "kek-sha256-16:$(sha garam-machine-key-material/ca-kek.hex | cut -c1-16)"
report garam-machine-key-material/tls.crt "cert-sha256:$(cert garam-machine-key-material/tls.crt)"
report garam-machine-key-material/tls.key "spki-sha256:$(pub garam-machine-key-material/tls.key)"
report garam-machine-key-material/server-root.pem "cert-sha256:$(cert garam-machine-key-material/server-root.pem)"
report garam-policy-tls/tls.crt "cert-sha256:$(cert garam-policy-tls/tls.crt)"
report garam-policy-tls/tls.key "spki-sha256:$(pub garam-policy-tls/tls.key)"
report garam-policy-ca/ca.crt "cert-sha256:$(cert garam-policy-ca/ca.crt)"
report garam-policy-jwks/jwks.json "file-sha256:$(sha garam-policy-jwks/jwks.json)"
report garam-policy-signing-key/signing.pem "spki-sha256:$(pub garam-policy-signing-key/signing.pem)"
report offline/server-root.key "spki-sha256:$(pub offline/server-root.key)"
echo "key-material: $count files written under $out, none printed"
