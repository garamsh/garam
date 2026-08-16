# ADR 0017: Machine identities are served by a process of their own

> Status: accepted
> Date: 2026-08-16

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

ADR-0014 decided that a machine identity authenticates on a listener of
its own, and offered metrics and gRPC as the shape to follow. Both of
those are listeners inside an existing process. ADR-0003 decided one
cobra CLI, one image, and two processes, and rests part of its
reasoning on there being two deployables.

So the machine listener could have been a goroutine inside `serve api`,
and ADR-0014 does not say otherwise. It is not, and the count in
ADR-0003 is stated plainly enough that changing it belongs in the
record rather than in a pull request's reasoning.

## Decision

- **A third serve command, `garam serve machine`, is the process a
  machine identity reaches.** It terminates TLS itself and requires a
  client certificate; the browser-facing process keeps its proxy and
  its session cookie and gains nothing.
- **The reason is key custody, and it is prospective rather than
  present.** A listener that only verifies never opens a CA private
  key, so today this process could hold no key-encryption key at all.
  What decides it is what lands here next: the surface that issues
  certificates must hold every organization's key-encryption key, and
  the process serving browsers must not be the process that holds it.
  Splitting later would mean moving a running surface; splitting now
  costs a deployable.
- **Everything else ADR-0003 decided stands**: one CLI at `cmd/garam`,
  one image, package independence between `internal/api` and
  `internal/authz`, gRPC between them, and actions as plain strings
  owned by their domains. This changes the enumeration and the count,
  and nothing else.
- **Local development does not start it.** `make backend-up` and the
  web end-to-end job bring up the browser-facing stack, which needs no
  machine key material. A contributor who never touches an agent never
  generates a certificate.

## Consequences

- Three deployables from one image rather than two: one more thing to
  expose, configure and watch, and one more place a deployment can
  forget a port.
- The failure of forgetting it is closed rather than silent — an agent
  cannot connect — which is the direction worth having.
- ADR-0003's "two deployables keeps local dev simple" is weakened in
  letter and kept in spirit: the two a contributor runs are unchanged,
  and the third is only run by someone working on the machine surface.
- Key material stays out of the browser-facing process, which is the
  property being bought and which cannot be recovered later without
  moving a live surface.
- The machine listener's HTTP surface has no specification. ADR-0004
  makes `api/openapi.yaml` the source of truth for what `serve api`
  serves, and this is a different listener; whether a second surface
  gets a spec of its own is undecided and the message endpoints will
  force it.

## Rejected alternatives

- **The listener as a goroutine inside `serve api`.** One deployable
  fewer, and the shape ADR-0014's own analogy suggests. Rejected
  because it puts every organization's key-encryption key in the
  process that serves browsers, as soon as issuing lands — and because
  local development would then need key material for a stack that does
  not use it, on a file a container user cannot own when it is bind
  mounted, which is the crash loop ADR-0015 describes.
- **A second binary.** Ends the one-CLI, one-image property ADR-0003
  bought, for a separation a subcommand already provides.
- **Terminating machine TLS at the existing proxy.** ADR-0014 rejected
  the forwarded client-certificate header, and nothing here changes
  that argument.
