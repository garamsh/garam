#!/bin/sh
# Generates the key material `serve machine` needs and puts it in a
# Secret. All of it is throwaway local material — see the comment on the
# volume in garam.yaml for what the real ones cost.
#
# Nothing generated here is written into the repository. The files exist
# in a private temporary directory for as long as `kubectl create` takes
# to read them, and the trap removes them on every exit path.
set -eu

namespace=garam-local
secret=machine-throwaway-key-material
kubectl="kubectl --context minikube -n $namespace"

# A Secret holding everything is kept rather than replaced. Every
# message signing key in the database is sealed under the
# key-encryption key that was there when it was created, so a second
# bring-up that generated a new one would leave them all unopenable
# (ADR-0016, ADR-0104).
if $kubectl get secret "$secret" -o jsonpath='{.data.server-root\.pem}' 2>/dev/null | grep -q .; then
  echo "$secret: present, kept — a new key-encryption key would orphan every sealed key"
  exit 0
fi

dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT INT TERM

# A Secret predating ADR-0040 carries no server root, and the pod cannot
# start without one. Its key-encryption key is carried across unchanged
# — it is the one value here that cannot be regenerated — and the TLS
# material beside it is replaced, which costs nothing here: it seals
# nothing.
migrating=false
if $kubectl get secret "$secret" >/dev/null 2>&1; then
  migrating=true
  $kubectl get secret "$secret" \
    -o go-template='{{ index .data "ca-kek.hex" | base64decode }}' >"$dir/ca-kek.hex"
else
  # A key-encryption key is 32 bytes, hex-encoded (internal/ca).
  openssl rand -hex 32 >"$dir/ca-kek.hex"
fi

# The machine listener presents a certificate signed by a garam server
# root, and every agent pins that root (ADR-0040). There is no
# cert-manager here, so the root is generated beside the certificate,
# expires in a year, and is trusted by nothing off this machine. A real
# deployment holds that root's key where garam cannot reach it; here
# the key is thrown away with the temporary directory below, which is
# why nothing renews.
#
# Each key is generated on its own because genpkey writes it unencrypted
# without a flag, where `req -newkey` needs one whose spelling differs
# between OpenSSL 3 and what a Mac ships.
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$dir/root.key" 2>/dev/null
openssl req -x509 -new -key "$dir/root.key" -days 365 \
  -subj "/CN=garam server root (throwaway)" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
  -addext "keyUsage=critical,keyCertSign" \
  -out "$dir/server-root.pem" 2>/dev/null

# The names are the ones the Service answers to inside the cluster.
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$dir/tls.key" 2>/dev/null
openssl req -new -key "$dir/tls.key" \
  -subj "/CN=machine.garam-local.svc.cluster.local" \
  -out "$dir/tls.csr" 2>/dev/null
openssl x509 -req -in "$dir/tls.csr" -days 30 \
  -CA "$dir/server-root.pem" -CAkey "$dir/root.key" -set_serial 1 \
  -extfile /dev/stdin \
  -out "$dir/tls.crt" 2>/dev/null <<EOF
subjectAltName=DNS:machine,DNS:machine.garam-local.svc,DNS:machine.garam-local.svc.cluster.local,DNS:localhost,IP:127.0.0.1
extendedKeyUsage=serverAuth
EOF

# Applied rather than created-after-delete: a delete that succeeded
# followed by a create that failed would take the key-encryption key
# with it, and no window exists in which the Secret is absent.
$kubectl create secret generic "$secret" \
  --from-file="$dir/ca-kek.hex" \
  --from-file="$dir/tls.crt" \
  --from-file="$dir/tls.key" \
  --from-file="$dir/server-root.pem" \
  --dry-run=client -o yaml | $kubectl apply -f -
if [ "$migrating" = true ]; then
  echo "$secret: kept its key-encryption key, generated 1 server root and 1 certificate signed by it"
else
  echo "$secret: generated 1 key-encryption key, 1 server root and 1 certificate signed by it"
fi
