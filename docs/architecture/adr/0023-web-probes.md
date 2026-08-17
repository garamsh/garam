# ADR 0023: The web app answers the same two probes as the rest

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Decision

- **`/healthz` and `/readyz`, spelled and meaning exactly what
  ADR-0021 gave the other three.** The web app is the fourth thing a
  scheduler runs, and a deployment that configures three surfaces one
  way and the fourth another way is configured wrong in a way nothing
  reports.
- **They are route handlers, not pages.** A probe target that is also
  a page can be renamed by someone changing navigation, and the probe
  starts failing for a reason the person who caused it never sees.
- **The web app's readiness reads nothing.** It has one dependency —
  the API it proxies to — and taking every replica out of rotation
  when the API blips removes the surface that renders the error, so a
  reader who could have been told what is wrong gets a connection
  failure from the ingress instead. Readiness answers whether *this*
  process can serve, and it can.

## Context

ADR-0021 settled two probes, two meanings, one spelling, and scoped
itself to "these three processes" — the three `serve` commands
ADR-0017 makes out of one image. The web app is a separate image with
a separate toolchain (ADR-0009), and the ADR neither covered it nor
excused it.

What filled the gap is what always fills a gap: something plausible,
uncommented on, chosen for what happened to be reachable. The local
manifest probes the web app's liveness on `/icon.svg` and its
readiness on `/login`. Both work, which is the problem — they are page
routes standing in for an interface, and ADR-0021 states that the
probe paths *are* interface, published in the contract of ADR-0020.

The readiness choice is the one with teeth. `/login` is a page, and a
page renders through the app's data path, so readiness answers a
question about the API as well as about the process. That is the
amplification ADR-0021 was written to prevent, one probe over from
where it was prevented: a scheduler pulls every web replica out of
rotation the moment the API is slow, and the console goes dark rather
than degraded.

The liveness choice is milder and already carries a scar. The manifest
comment records that liveness once restarted the web app whenever the
API blipped and was split to stop it — the same lesson, learned here
by measurement, and worth keeping rather than re-learning when someone
tidies the probes.

## Consequences

- Four surfaces, one probe configuration. A deployment repository this
  one does not control writes the same two paths for all of them.
- The web app's readiness cannot fail for a reason outside the
  process, so it says less than the other three's do. That is the
  intended trade: it says less and it says it about the right thing.
- The console stays in rotation during an API outage. It does **not**
  report the outage, and this was measured rather than assumed: with
  the API unreachable, `/login` answers 500 and the console pages
  answer 200 with no content, all three landing on the last-resort
  error boundary that says only that something went wrong. Staying in
  rotation is what this decision buys, and it is half of what a reader
  wants. The other half is a defect in the pages, tracked separately —
  this ADR is what makes fixing it worth anything, because a console
  taken out of rotation cannot report what it knows.
- ADR-0020's published contract gains a fourth image whose probe paths
  are fixed, so changing them supersedes this ADR rather than editing
  a handler.

## Rejected alternatives

- **Leave the page routes.** They work today. They work by accident:
  nothing tells the person renaming `/login` that a scheduler reads
  it, and the failure surfaces as replicas going unready during a
  deploy that touched only navigation.
- **Readiness that checks the API.** It is the honest reading of "the
  dependencies a request needs", and it is the reading that takes the
  console down exactly when the console is what someone needs. The
  console's job during an API outage is to be reachable and say so.
- **No probes on the web app at all.** The scheduler then restarts
  nothing when the process wedges, and a wedged Node process holds its
  port open, so a TCP check would call it healthy — the same
  well-formed answer from a path that exercised nothing that this
  repository keeps finding.
- **Extending ADR-0021 in place.** ADRs are append-only
  (`docs/architecture/README.md` §Rules 2), and the scope line there
  is a fact about what was decided that day. This one names it rather
  than overwriting it.
