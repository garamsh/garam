# ADR 0037: The machine listener presents a per-organization certificate

> Status: superseded by [ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
> Date: 2026-08-19

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

The machine listener presents a server certificate read from
`GARAM_MACHINE_TLS_FILE` material, and #218 asked what should be in
that file, who issues it, and what a client verifies it against.
Nothing decided it: ADR-0013 has the per-organization CA issue the
agent's certificates, and [ADR-0014](./0014-agent-authentication.md)
fixes what a *client* presents — but the certificate garam itself
presents was left to whatever a deployment put there.

The fork in the question is real. One certificate from a public
authority would make garam's identity to a machine rest on the public
web PKI — a different trust model from the one ADR-0014 chose for the
other direction, where an organization's agents trust exactly one CA
and a compromise elsewhere reaches nothing. A deployment is also free
to terminate TLS anywhere it likes in between, which the listener
cannot accept anyway — it terminates TLS itself because the client
certificate is the caller.

## Decision

**The listener presents a per-organization certificate, selected by
SNI, issued by the organization's own certificate authority.** An agent
verifies garam against the same trust root that signed the agent — the
root it already holds, because it travels with the certificate.

- The listener reads the client certificate's SAN to learn the
  organization, then serves that organization's certificate. A client
  offering no usable SNI gets no answer rather than a wrong one.
- The issuing machinery already exists: the organization's CA signs
  the listener's certificate the way it signs any other, and rotation
  is ADR-0014's reissue and restart, with no reload.
- `ca.Certificate.IssuerPEM`'s contract — a holder needs the issuing
  CA to verify the other side — is what makes the agent side free: the
  trust root is already in its hands.

## Consequences

- The trust model is symmetric: agent verifies garam and garam verifies
  agent against the same per-organization root. A public-CA compromise
  or a neighbour organization's CA compromise reaches nothing.
- The listener holds one key pair per organization it serves, under
  ADR-0015's arrival rule (the key file through `internal/keyfile`).
  The deployment contract's key-material section names the new files.
- A deployment serving one organization mounts one pair; the SNI
  selection is the shape that keeps that true when it serves two.

## Rejected alternatives

- **One certificate for the listener from a public authority.**
  Simpler to mount, and it puts garam's identity to a machine on the
  web PKI — a strictly weaker root than the one ADR-0014 established,
  adopted at the exact surface where machine identities live.
- **The per-organization CA's key signs the listener certificate
  directly, long-lived.** The CA's key stays sealed (ADR-0016); the
  listener's certificate is an ordinary issue with an ordinary
  lifetime, reissued on rotation like any other.
