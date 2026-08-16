#!/bin/sh
# Generates the two pieces of key material `serve machine` needs and
# puts them in a Secret. Both are throwaway local material — see the
# comment on the volume in garam.yaml for what the real ones cost.
#
# Nothing generated here is written into the repository. The files exist
# in a private temporary directory for as long as `kubectl create` takes
# to read them, and the trap removes them on every exit path.
set -eu

namespace=garam-local
secret=machine-throwaway-key-material

# An existing Secret is kept rather than replaced. Every certificate
# authority in the database is sealed under the key-encryption key that
# was there when it was created, so a second bring-up that generated a
# new one would leave them all unopenable (ADR-0016).
if kubectl --context minikube -n "$namespace" get secret "$secret" >/dev/null 2>&1; then
  echo "$secret: present, kept — a new key-encryption key would orphan every sealed authority"
  exit 0
fi

dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT INT TERM

# A key-encryption key is 32 bytes, hex-encoded (internal/ca).
openssl rand -hex 32 >"$dir/ca-kek.hex"

# The machine listener terminates TLS itself, and there is no
# cert-manager here: this pair is self-signed, trusted by nothing, and
# expires in a month. The names are the ones the Service answers to
# inside the cluster.
#
# The key is generated on its own because genpkey writes it unencrypted
# without a flag, where `req -newkey` needs one whose spelling differs
# between OpenSSL 3 and what a Mac ships.
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$dir/tls.key" 2>/dev/null
openssl req -x509 -new -key "$dir/tls.key" -days 30 \
  -subj "/CN=machine.garam-local.svc.cluster.local" \
  -addext "subjectAltName=DNS:machine,DNS:machine.garam-local.svc,DNS:machine.garam-local.svc.cluster.local,DNS:localhost,IP:127.0.0.1" \
  -out "$dir/tls.crt" 2>/dev/null

kubectl --context minikube -n "$namespace" create secret generic "$secret" \
  --from-file="$dir/ca-kek.hex" \
  --from-file="$dir/tls.crt" \
  --from-file="$dir/tls.key"
echo "$secret: generated 1 key-encryption key and 1 self-signed certificate"
