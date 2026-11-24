# ADR 0186: The cluster-connection routes are served from the interface's own specification, vendored at a pinned commit

> Status: accepted
> Date: 2026-11-24

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2046, for which the Garam PM reserved this number. The
fork it settles was put to the PM with a recommendation and agreed on
2026-11-24 in that issue.

## Context

[ADR-0184](./0184-a-connected-cluster-is-a-machine-identity-that-registers-once-with-a-one-time-token-and-a-key-it-generates.md)
has a connected cluster's connector speak the wire of
`garamsh/garam-bot-operator`'s `interfaces/cluster-connection/v1alpha1/`
to garam. That interface is garam-bot-operator's, not garam's: its
`README.md` is normative for behaviour, its `openapi.yaml` is normative
for shapes, and its `conformance.md` is the suite both a server and a
client are held to. garam is one of the two production servers it must
admit, and the reference server beside it is another.

[ADR-0019](./0019-machine-openapi-spec.md) decided three things for the
machine listener:

- `api/machine.yaml` is the source of truth for what `garam serve
  machine` serves;
- one specification per listener;
- the whole listener is in it.

Restating the interface's paths in `api/machine.yaml` honours all
three and makes two copies of one contract, garam's and the
interface's. The copy garam serves would drift from the one the suite
tests, and the drift is the failure ADR-0004 and ADR-0019 exist to
prevent, met between repositories rather than between a handler and a
document.

The interface's paths also carry what `api/machine.yaml` does not. Its
error body is `{error, message}` with codes the interface lists (T-5),
where the machine listener's is `{kind, message, reason}`. Its
registration takes no token at all. A connector is built against the
interface and never reads `api/machine.yaml`.

## Decision

1. **The paths under `/cluster-connection/v1alpha1/` are served from
   the interface's own specification.** garam-bot-operator's
   `interfaces/cluster-connection/v1alpha1/openapi.yaml` is vendored
   byte for byte at
   `platform/api/cluster-connection/v1alpha1/openapi.yaml`.
   oapi-codegen generates its server beside it, and
   `internal/machineapi` implements it. garam does not restate any of
   it.
2. **The vendored copy is pinned to a full commit SHA, and the PM moves
   the pin.** `make -C platform check-vendor`, part of the module's
   `ci`, fails when the copy is not that commit's file. The first pin
   is garam-bot-operator#447's merge commit,
   `552a69f28e7fb254c443c669b18ce27f54ac9eb1`.
3. **The routes are mounted on the machine listener,** on the same mux
   as `api/machine.yaml`'s routes, behind the same server TLS and
   token resolution (ADR-0100). Their refusals carry the interface's
   error body (T-5), not the listener's.
4. **The cluster's `private_key_jwt` exchange stays on `/oauth/token`**,
   which `api/machine.yaml` specifies. The exchange chooses the
   cluster's profile by the GRN type of `client_id`, and a registration
   answers that URL as `tokenUrl`.
5. **This supersedes ADR-0019 in part, for paths under
   `/cluster-connection/v1alpha1/` and no others.** For those paths,
   two of its decisions no longer hold: `api/machine.yaml` is not the
   source of truth for them, and they are not in it. Its second
   decision gives way on the same ground: the machine listener now
   carries two specifications. ADR-0019 stands as written for every
   other path the listener serves.

## Consequences

- A change to the wire is a change to garam-bot-operator's interface,
  reviewed there. garam takes it by a pin move, which regenerates the
  server, and a handler change is owed wherever the generated interface
  moved.
- A reader asking what the machine listener serves reads two
  specifications, and `api-surface.md` names both.
- A route the interface defines and garam has not built still has a
  generated method, and it answers 501 with the code `internal`, which
  T-6 has the connector back off from. `watchDeclarations` (#2048) and
  `report` (#2050) answer that way until they land.
- `check-vendor` reads the pinned file over HTTPS from
  `raw.githubusercontent.com`. The module's `ci` now needs network
  access to that host, and fails where it has none.
- A future version of the interface is a new path (its §Compatibility).
  It is vendored beside this one at a pin of its own, under a decision
  this one already covers.

## Signs the decision was wrong

- A pin move that regenerates the server breaks handlers often enough
  that garam would rather hold a copy of its own, and garam's copy and
  the interface's would then differ on purpose.
- The interface comes to need behaviour on the machine listener's other
  routes. The bot-token mint (AUTH-4, AUTH-5) is the first candidate.
  In that case the two-specification split cuts through one concern
  rather than along a boundary.
- `check-vendor`'s network read fails often enough in CI that it is
  skipped or retried by hand.

## Rejected alternatives

- **Restating the paths in `api/machine.yaml`.** It keeps ADR-0019
  whole and gives one generated server. It also makes two copies of
  one contract, with nothing to hold them together except a reviewer
  reading both, and the listener's error body would have to bend to
  T-5 on some routes and not on others.
- **A listener of its own for the cluster connection.** It would honour
  "one specification per listener" by adding a port, a certificate and
  a token resolution identical to the machine listener's, and a
  deployment would have one more thing to expose. Nothing a cluster
  authenticates as differs from a machine caller's, which is the split
  ADR-0019 follows.
- **Importing the interface as a Go module or a submodule.** The
  interface publishes a specification, not a Go package garam can
  generate into. A submodule pins by SHA just as well, but makes every
  checkout fetch garam-bot-operator, where only the one file is needed.
