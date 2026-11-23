#!/bin/sh
# Tests deploy/cluster/key-material.sh against a throwaway directory:
# what it writes, with which modes, that the certificates chain and name
# what the chart's Services answer to, that nothing secret is printed,
# and that a second run into the same directory is refused and leaves
# the key-encryption key as it was. Needs only openssl.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
fail() {
  echo "key-material-test: $*" >&2
  exit 1
}

out="$tmp/m"
sh "$here/key-material.sh" "$out" >"$tmp/stdout" 2>"$tmp/stderr" ||
  fail "the run failed: $(cat "$tmp/stderr")"

want="garam-machine-key-material/ca-kek.hex garam-machine-key-material/server-root.pem garam-machine-key-material/tls.crt garam-machine-key-material/tls.key garam-policy-ca/ca.crt garam-policy-jwks/jwks.json garam-policy-signing-key/signing.pem garam-policy-tls/tls.crt garam-policy-tls/tls.key offline/server-root.key"
got=$(cd "$out" && find . -type f | sed 's|^\./||' | sort | tr '\n' ' ' | sed 's/ $//')
[ "$got" = "$want" ] || fail "files written: $got; want $want"

for f in $want; do
  m=$(stat -c %a "$out/$f" 2>/dev/null || stat -f %Lp "$out/$f")
  [ "$m" = 600 ] || fail "$f has mode $m, want 600"
done
m=$(stat -c %a "$out" 2>/dev/null || stat -f %Lp "$out")
[ "$m" = 700 ] || fail "the directory has mode $m, want 700"

kek=$(cat "$out/garam-machine-key-material/ca-kek.hex")
case $kek in
*[!0-9a-f]* | '') fail "the key-encryption key is not hex" ;;
esac
[ "${#kek}" -eq 64 ] || fail "the key-encryption key is ${#kek} digits, want 64"

# Nothing a file holds reaches the terminal: not the key-encryption key,
# not a private key's body, not the modulus the JWKS carries.
for f in garam-machine-key-material/tls.key garam-policy-tls/tls.key garam-policy-signing-key/signing.pem offline/server-root.key; do
  body=$(sed -n '2p' "$out/$f")
  ! grep -qF "$body" "$tmp/stdout" "$tmp/stderr" || fail "the output holds a line of $f"
done
! grep -qF "$kek" "$tmp/stdout" "$tmp/stderr" || fail "the output holds the key-encryption key"
n=$(sed 's/.*"n":"\([^"]*\)".*/\1/' "$out/garam-policy-jwks/jwks.json")
! grep -qF "$n" "$tmp/stdout" "$tmp/stderr" || fail "the output holds the JWKS modulus"
[ "$(wc -l <"$tmp/stdout")" -eq 11 ] || fail "the report is not one line per file and a count"

# The machine certificate chains to the server root and names what the
# listener is reached by, inside the cluster and outside it.
openssl verify -CAfile "$out/garam-machine-key-material/server-root.pem" "$out/garam-machine-key-material/tls.crt" >/dev/null ||
  fail "the machine certificate does not chain to the server root"
san=$(openssl x509 -in "$out/garam-machine-key-material/tls.crt" -noout -ext subjectAltName)
for name in machine.garam.garam.sh garam-machine garam-machine.garam.svc; do
  echo "$san" | grep -qF "DNS:$name" || fail "the machine certificate does not name $name"
done
san=$(openssl x509 -in "$out/garam-policy-tls/tls.crt" -noout -ext subjectAltName)
echo "$san" | grep -qF "DNS:garam-policy" || fail "the policy certificate does not name garam-policy"
cmp -s "$out/garam-policy-tls/tls.crt" "$out/garam-policy-ca/ca.crt" || fail "the policy CA is not the policy certificate"

# The JWKS is the public half of the signing key: the kid is the key's
# thumbprint and the modulus is the key's.
want_n=$(openssl pkey -in "$out/garam-policy-signing-key/signing.pem" -noout -text | sed -n '/^modulus:/,/^publicExponent:/p' | grep '^ ' | tr -d ' :\n' | sed 's/^00//')
got_n=$(printf '%s' "$n" | tr '_-' '/+' | base64 -d 2>/dev/null | od -An -tx1 | tr -d ' \n')
[ "$want_n" = "$got_n" ] || fail "the JWKS modulus is not the signing key's"

# A second run into the same directory is refused and replaces nothing.
if sh "$here/key-material.sh" "$out" >/dev/null 2>"$tmp/stderr2"; then
  fail "a run into an existing directory was accepted"
fi
grep -q 'must not be replaced' "$tmp/stderr2" || fail "the refusal does not say why: $(cat "$tmp/stderr2")"
[ "$(cat "$out/garam-machine-key-material/ca-kek.hex")" = "$kek" ] || fail "the key-encryption key changed"

# A name given to the run is the name the certificates carry.
sh "$here/key-material.sh" "$tmp/n" --fullname prod-garam --namespace prod --machine-host m.example.test >/dev/null
openssl x509 -in "$tmp/n/garam-machine-key-material/tls.crt" -noout -ext subjectAltName | grep -qF "DNS:prod-garam-machine.prod.svc" ||
  fail "--fullname and --namespace did not reach the certificate"

echo "key-material-test: 10 files, modes, chain, names, JWKS, refusal and options checked"
