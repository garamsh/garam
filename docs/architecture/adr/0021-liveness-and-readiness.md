# ADR 0021: Liveness and readiness answer on a port that carries no credential

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives
- Errata

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

- **Two probes, two meanings, one spelling on every process.**
  `/healthz` answers for the process alone and reads nothing outside
  it. `/readyz` answers for the dependencies a request needs, and fails
  while one of them is not there. Both paths are the same on all three,
  so three deployments are configured the same way.
- **The answer is served where a caller needs no credential**: for
  `serve api`, the HTTP port it already serves on (`GARAM_HTTP_ADDR`,
  default `:8080`); for `serve authz`, the listener its metrics already
  use (`GARAM_METRICS_ADDR`, default `:9091`), because the engine
  speaks gRPC only; for `serve machine`, a plaintext listener of its
  own (`GARAM_MACHINE_HEALTH_ADDR`, default `:8444`), because its own
  listener admits no caller without a client certificate. The mTLS
  listener gains no route.
- **Those paths and ports are interface, not implementation.** They are
  the "paths a probe may reach" of the contract ADR-0020 publishes, so
  a repository this one does not control is written against them:
  changing either is superseding this ADR, not editing a handler.
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
  They reach them with the base image's `wget`, so removing the shell
  from that image is a change that has to bring its own healthcheck. A
  kubelet needs neither.
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

## Errata

- 2026-08-27 — §Consequences' "They reach them with the base image's
  `wget`, so removing the shell from that image is a change that has to
  bring its own healthcheck" was true when written and is false since
  #369, which on 2026-08-18 moved the Go image's final stage to
  `FROM scratch`. At `da50c58`, where #231 merged this ADR, that stage
  was `FROM alpine:3.24.1` and both Go healthchecks read
  `CMD-SHELL wget -q -O /dev/null …/readyz`; today
  `deploy/local/docker-compose.yml` runs the shipped binary instead —
  `garam probe` against `/readyz` on `:8080` and `:9091`, a subcommand
  `internal/cli/probe.go` answers. The constraint this sentence states
  was met rather than broken: the change that removed the shell brought
  its own healthcheck, which is what the clause asked of it. The only
  `wget` left is the `web` service's, on a different image
  (`node:24.18.1-alpine3.24`) that carried no healthcheck at all when
  this ADR merged, so it was never in this sentence's scope. The rest of
  the bullet stands — the local stack's healthchecks are these probes
  rather than TCP connects, so `service_healthy` still means the process
  can serve a request — and the decision is untouched: both probes still
  answer on a port that carries no credential.
- 2026-08-27 — §Decision's "Readiness is what a dependency reaches"
  gives the engine channel to `serve api`, and `serve machine` reports
  it too. The sentence was true when written and is false since #253,
  which added `authzChannel` to that process's health listener eight
  hours after #231 merged this ADR: at `da50c58`,
  `internal/cli/machine.go:99` registered `health.Database(pool)` and
  nothing else. The gap is what makes this an event rather than an
  oversight — the bullet described the tree it was written against, and
  that tree moved the same day. What holds today is that all three
  commands report the database, and two of them report the engine
  channel as well: `serve api` and `serve machine`, in
  `internal/cli/serve.go` and `internal/cli/machine.go`. `serve authz`
  reports the database alone. The decision is untouched and so is the
  bullet's own rule — readiness is still what a dependency reaches, and
  what moved is how many processes reach the engine.
- 2026-09-05 — §Decision's "because its own listener admits no caller
  without a client certificate" was true when written and is false since
  `b16a896` (#1005), which on 2026-09-04 set the machine listener's
  `ClientAuth` to `tls.RequestClientCert` and made the verification it
  runs in the handshake return no error when the chain does not verify
  (`internal/machine/listener.go`). A connection presenting no
  certificate completes the handshake and carries no identity instead,
  which is what leaves the enrollment route reachable to an operator
  that holds no certificate yet
  ([ADR-0064](./0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md)).
  The decision is untouched and so is the clause beside this one: the
  mTLS listener still gains no route, `api/machine.yaml` declaring none
  for either probe, and `serve machine` still answers both on a
  plaintext listener of its own. What moved is the reason — the refusal
  a probe would meet is now each handler's rather than the handshake's.
