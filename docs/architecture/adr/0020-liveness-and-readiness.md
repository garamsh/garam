# ADR 0020: Liveness and readiness answer on a port that carries no credential

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

ADR-0017 makes three processes out of one image, and nothing deploys
them until a scheduler can ask each two questions: whether to restart it
and whether to send it traffic. They are different questions with
different consequences, and answering them with one check is what makes
an outage worse — a liveness probe that reads a dependency restarts
every replica at once the moment that dependency blips, so the outage
amplifies where it would otherwise drain.

The processes do not offer the same surface to ask on. `serve api`
already serves HTTP without a session on `/metrics`, and `serve authz`
speaks gRPC only and already runs an HTTP listener beside it for the
same reason. `serve machine` terminates TLS itself and requires a client
certificate (ADR-0014), and a scheduler holds none — so on that process
there is nothing to ask on at all.

## Decision

- **Two probes, two meanings.** `/healthz` answers for the process
  alone and reads nothing outside it. `/readyz` answers for the
  dependencies a request needs, and fails while one of them is not
  there.
- **The answer is served where a caller needs no credential**: the
  api's own HTTP port, the authz engine's existing metrics listener,
  and — for `serve machine` — a plaintext listener of its own on
  `GARAM_MACHINE_HEALTH_ADDR`. The mTLS listener gains no route.
- **The meaning of the two lives in `internal/health`**, which the
  three composition roots mount. A process names its own dependencies;
  what the two words mean is written once.
- **Readiness is what a dependency reaches.** The database for each of
  the three, and the engine channel for `serve api`, which decides
  every org-scoped request.

## Consequences

- A database outage takes replicas out of rotation and leaves them
  running, so they serve again when it returns without a restart having
  been ordered.
- `serve machine` exposes a second port, which a deployment can forget.
  It fails open rather than closed — forgetting it costs the probes and
  not the surface — which is the opposite direction to ADR-0014's
  second port and is the reason it is worth stating here.
- The local stack's healthchecks are these probes rather than TCP
  connects, so `service_healthy` means the process can serve a request.
- A process whose readiness answer cannot fail is indistinguishable
  from one that works, so each check ships with the failing case
  asserted beside the passing one.
- Startup probes are not decided here. Nothing in these three processes
  has a start slow enough to need one; a surface that acquires one
  states so.

## Rejected alternatives

- **One probe for both questions.** Half the configuration, and it
  restarts a healthy process because something it talks to is down.
  Restarting is the expensive answer and the one a dependency outage
  least deserves.
- **Probing the machine listener with a client certificate.** No new
  port, and it would test the real surface. It puts a certificate and
  its rotation into every deployment's probe configuration, and the
  certificate a scheduler would hold names an identity garam issues to
  agents (ADR-0014) — a probe would be one.
- **A gRPC health service on `serve authz`, probed with grpc-health-probe.**
  The ecosystem-standard answer for a gRPC server, and the engine gains
  a second surface stating what an HTTP one already states on a
  listener it runs regardless.
- **Readiness that reports the policy-change listener.** A stopped
  listener is a real degradation — the engine serves from a cache
  nothing invalidates — but it never restarts, so one database blip
  would drain every replica permanently. Restarting the listener is
  what has to land first.
