#!/bin/sh
# Writes a throwaway self-signed certificate and key into /tls, for the
# DNS name its first argument gives and owned by the user its second
# gives, the key readable by that user alone. The policy service serves
# no plaintext, and this is the pair it serves on this machine. The
# stack and tooling/policy-image-e2e.sh run it on the alpine image the
# policy-tls service names, which carries no openssl of its own.
set -eu

name=${1:?the DNS name the certificate holds}
owner=${2:?the user that owns the key}

apk add --no-cache --quiet openssl

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
	-days 30 -subj "/CN=$name" \
	-addext "subjectAltName=DNS:$name" \
	-addext "basicConstraints=critical,CA:FALSE" \
	-keyout /tls/tls.key -out /tls/tls.crt 2>/dev/null

chown "$owner" /tls/tls.key
chmod 600 /tls/tls.key
chmod 644 /tls/tls.crt
