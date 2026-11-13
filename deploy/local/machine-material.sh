#!/bin/sh
# Writes into /material what the machine listener reads: its TLS pair,
# which names `machine` and is its own root, and its key-encryption key,
# both owned by the user the first argument gives and readable by that
# user alone, as the process refuses anything wider (ADR-0017). Neither
# is printed. They belong to this stack and are removed with its volume.
# The stack runs this on the alpine image policy-tls names, which
# carries no openssl of its own.
set -eu

owner=${1:?the user that owns the key files}

# Compose runs this again whenever a service that waits on it is brought
# up, and a listener holds what it started with: what is there stays.
# kek.hex is written last, so its presence says the set is whole.
[ ! -e /material/kek.hex ] || exit 0

apk add --no-cache --quiet openssl

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
	-days 30 -subj "/CN=machine" \
	-addext "subjectAltName=DNS:machine" \
	-addext "basicConstraints=critical,CA:FALSE" \
	-keyout /material/machine.key -out /material/machine.crt 2>/dev/null
chown "$owner" /material/machine.key
chmod 600 /material/machine.key
chmod 644 /material/machine.crt

openssl rand -hex 32 >/material/kek.hex
chown "$owner" /material/kek.hex
chmod 600 /material/kek.hex
