# ADR 0036: A secret in garam is a reference, not a value

> Status: accepted
> Date: 2026-08-19

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

The intended structure names secrets as a first-class resource — usable
by agents, projects, and clusters, at organization and project level —
and nothing in this repository stores, serves, or protects one. #304
asked the questions in their dependency order, and the owner has
answered the first, which constrains the rest: **garam holds a reference
to a secret, not the value.**

The existing secret-adjacent machinery shows what holding a value costs.
A certificate authority's key is sealed under a key-encryption key
([ADR-0016](./0016-ca-key-custody.md)), travels as an owner-only file
([ADR-0015](./0015-operator-identity.md)), and is never stored for an
issued certificate at all. That machinery exists because a CA key cannot
leave. A general secret store is the opposite business: values meant to
leave, to arbitrary consumers, with rotation, audit, and a blast radius
a database backup would silently widen.

## Decision

- **garam stores the reference, never the value.** A secret resource
  names where the value lives — an external store the organization
  operates — plus the metadata to identify and rotate the reference.
  garam is a directory over someone else's store.
- **No surface of garam's serves a secret's value.** Not the console, not
  the machine listener, not an API. A consumer reads the value from the
  referenced store, under that store's own authentication.
- The reference is a resource with a GRN, so policy can decide who may
  read *that a secret exists and where it lives* — metadata, not
  material. What org-versus-project level means for it follows the
  existing resource model.

## Consequences

- The dangerous half of the feature — sealing, rotation of values,
  breach blast radius — never exists here. What exists is naming and
  authorization over references, which the policy model already covers.
- An organization adopting garam needs a secret store of its own. That
  is the ADR-0012 shape (bring your own operator) extended: bring your
  own vault.
- The value never crosses garam, so a garam database backup is not a
  secret breach — the property the store-shaped alternative could never
  have given.
- What the reference's exact payload is (store kind, addressing,
  versioning) is the next decision, taken when the first consumer needs
  it — this ADR fixes the boundary, not the schema.

## Rejected alternatives

- **Hold the value, sealed.** garam becomes a secret store: rotation,
  audit, and a backup that is a breach arrive with it, and ADR-0016's
  bring-your-own-operator reasoning is contradicted at the exact point
  it was meant to protect.
- **Hold nothing.** Then agents have no way to name the secrets they
  use, and every deployment invents its own channel for what is
  configuration-shaped — the console cannot show what an agent is meant
  to reach.
