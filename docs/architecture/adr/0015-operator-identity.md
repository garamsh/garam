# ADR 0015: Operators authenticate as agents do, and key material travels as files

> Status: accepted
> Date: 2026-08-16

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

ADR-0014 settled how an agent proves which agent it is. It did not
settle the identity standing next to it: ADR-0012 has an operator read
garam to learn which agents it should run, which makes the operator a
second machine caller. Designing it after the first would leave two
mechanisms to reconcile, and the operator's own PM raised exactly that
before either was built.

Three further questions were left open by ADR-0014 and are answered
here: how a certificate reaches the process that presents it, whether
an expired certificate is observable, and what a fleet does when its
certificates expire together.

Facts established outside this repository, checked rather than assumed:

- **A GRN in a SAN URI is an opaque URI.** `url.Parse` on
  `grn:acme:default:agent:bot-1` returns the identity in `Opaque` and
  leaves `Path` and `Host` as **empty strings, not errors**, because
  there is no `//` to make it hierarchical. Verification that reads
  `Path` recovers an empty identity that then compares equal to
  another empty identity.
- **An environment variable is not a place for a key.** The agent
  platform execs child processes, so the value is inherited, readable
  at `/proc/<pid>/environ`, and captured in a crash dump.
- **A key readable by group or other is rejected by the platform**, and
  the symptom is a crash loop rather than a permission error.

## Decision

- **An operator is a machine identity of the same kind as an agent and
  authenticates the same way**: mTLS on the same listener, a SAN URI
  holding its GRN, signed by the organization's CA. One mechanism to
  secure, rotate and reason about rather than two.
- **An operator obtains an agent's certificate by asking garam, and
  garam checks the assignment.** No issuing credential exists to be
  stolen: what an operator may obtain is exactly the agents assigned
  to it at the current epoch. ADR-0012's relation row does this second
  job without a new table.
- **The operator's own first certificate is installed out of band,
  once.** That single bounded manual step is what removes the per-agent
  one, and it is the only bootstrap in the system.
- **An assignment stays inside one organization**, enforced the way
  ADR-0008 enforces attachment scope. An operator authenticated by one
  organization's CA cannot be assigned an agent of another.
- **Key material reaches a process as a file, never an environment
  variable**, readable only by its owner, with the reading process
  running as that owner.
- **Lifetimes carry jitter at issuing, and an agent's garam-facing and
  loopback certificates share an expiry.** Rotation is a restart on
  both sides, so agents provisioned together would otherwise restart
  together, and independent expiries would restart one agent twice.
- **garam distinguishes an agent that has never authenticated from one
  that authenticated and stopped.** These are different failures —
  a bootstrap that never worked against a credential that lapsed — and
  they are facts about garam's own surface rather than inferences
  about the agent's health.
- **The operator reports adapter reachability as part of provisioning
  state.** It holds its own connection to garam, so it can report the
  adapter's failure to reach garam over a path that is not the failing
  one. This is what provisioning state already is in ADR-0012 — what
  the operator observes about the pod — and it gives the gap two
  independent observers instead of none.

## Consequences

- Creating an agent depends on the CA being available, because the
  certificate is the identity and must exist before the agent runs.
  Accepted: the alternative is an agent that exists and cannot speak.
- An operator's blast radius is the agents assigned to it. The
  assignment check is therefore not an optimisation and may not be
  skipped for a fast path.
- Revoking an operator means revoking one certificate, but the agent
  certificates it already fetched stay valid until they expire, which
  is ADR-0014's revocation model reaching one level further.
- The runtime state vocabulary of ADR-0012 — `connected` and
  `disconnected` — predates ADR-0013's polling model, where no
  connection is held. The terms are revisited when an endpoint first
  writes them; nothing writes them today.
- Nothing here is implemented, and it rests on the certificate
  authority of ADR-0014, which is not implemented either.

## Rejected alternatives

- **Issuing every agent certificate out of band.** A person or a
  pipeline fetches from the CA and installs the material. It needs no
  operator credential at all, and it defeats the automation the
  operator exists for — an operator that reads garam to decide what to
  run, then waits for a human to make each agent able to speak, has
  automated nothing. Acceptable as a first milestone, not as the shape.
- **The adapter fetching its own certificate at startup**, bootstrapped
  by a projected ServiceAccount token federated to garam. No issuing
  credential rests anywhere and no key material sits in cluster
  storage, which is stronger than what is chosen here. It is rejected
  for now because it requires per-cluster issuer federation configured
  for every deployment, and it stays the escalation path if an
  operator's issuing scope proves too wide.
- **A distinct credential type for operators** — a token, an API key.
  Two mechanisms to secure, rotate, revoke and test for one property
  that mTLS already provides.
- **Passing key material through the environment.** Convenient, and
  each of the three leak paths above is a plain fact about the process
  model rather than a hypothetical.
