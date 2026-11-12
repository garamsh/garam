#!/bin/sh
# Fills /keys for tooling/codehub-e2e.sh's stack, owned by the user its
# first argument gives. /in holds what the run made on the host, the
# issuer's signing key and the service keys file, and both are copied
# in, the private one readable by that user alone. It adds a
# key-encryption key and the machine listener's certificate, which names
# `machine` and `localhost` and is its own root. Everything here is
# throwaway and removed with the volume. The alpine image carries no
# openssl, and the stack runs this on the one policy-tls names.
set -eu

owner=${1:?the user that owns the keys}

# Compose runs this again whenever a service that waits on it is brought
# up, and a listener already serving holds the certificate it started
# with: what is there stays.
[ ! -e /keys/machine.crt ] || exit 0

apk add --no-cache --quiet openssl

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
	-days 2 -subj "/CN=machine" \
	-addext "subjectAltName=DNS:machine,DNS:localhost" \
	-addext "basicConstraints=critical,CA:TRUE" \
	-keyout /keys/machine.key -out /keys/machine.crt 2>/dev/null
openssl rand -hex 32 >/keys/kek.hex
cp /in/signing.pem /keys/signing.pem
cp /in/service-keys.json /keys/service-keys.json

chown "$owner" /keys/machine.key /keys/kek.hex /keys/signing.pem
chmod 600 /keys/machine.key /keys/kek.hex /keys/signing.pem
chmod 644 /keys/machine.crt /keys/service-keys.json
