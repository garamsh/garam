# ADR 0025: The engine's organization scope is the caller's obligation

> Status: accepted — the engine-membership half this decision deferred is decided by [ADR-0038](./0038-the-engine-learns-a-subjects-memberships-and-enforces-its-own-scope.md);
> the caller's obligation is superseded by [ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
> Date: 2026-08-17

## Contents
- Decision
- Context
- Consequences
- Rejected alternatives
- Errata

## Decision

- **A caller may only ask the engine about a subject that belongs to
  the organization owning the object.** The engine cannot check this
  and does not try. A caller that cannot establish it must refuse
  before asking, and refusing is not an authorization decision — it is
  the precondition of asking for one.
- **`authz.md` states the obligation where it states the scope**, so
  the promise and the thing that keeps it are read together.
- **A new call path into the engine is reviewed against this
  specifically.** It is the one property of an authorization call that
  neither the type system nor the test suite reports.

## Context

`authz.md` says an unattached active policy applies to "every subject
of the policy's org". `internal/authz/cache.go`'s `appliesTo` returns
true for an empty subject set with no scope check of any kind. The
documented scope and the implemented scope differ, and the difference
is the whole of an organization boundary.

**The engine cannot close the gap with what it is given.** A user GRN
is root-owned — `grn:root:default:user:...` — so its organization
segment is never the policy's organization, and comparing the two
would refuse every user check ever made. Membership lives in the
organization tables, which ADR-0007 deliberately keeps on the api
server's side of the split. The engine has the subject's string and
nothing that says where the subject belongs.

So the scope has always been enforced by the caller. `internal/api`'s
`authorizeOn` enforces it, but as a **side effect**: it fetches the
member to obtain a role, and a non-member has no role, so a foreign
subject never reaches the engine. Nothing in that code says it is
holding an organization boundary, and nothing outside it says a caller
must.

That is not a hypothetical. A second caller was written — the machine
listener asking whether an agent may send a message (ADR-0024) — by an
author who reasoned carefully about statement collection, documented
the reasoning, and had no way to learn the rule. It evaluated a
subject from one organization against an object in another, and any
organization holding an unattached active policy became reachable by
every agent everywhere. The reasoning that would have caught it was in
the author's own pull request, aimed at the half of the union the
caller supplies rather than the half the database holds.

**Neither review nor the test suite would have caught it, and neither
would catch the next one.** The engine's tests exercise subjects that
are members, which is precisely the assumption under test.

## Consequences

- The condition survives this decision. Stating a contract makes the
  rule learnable; it does not make it enforceable, and a caller that
  forgets is refused by nothing.
- The review standard gains a question that cannot be answered by
  reading the diff alone: *can the subject and the object belong to
  different organizations here?* Fifteen call sites on the browser API
  answer no, each because it builds its object from the same
  organization slug it checks membership against.
- The two callers now hold the boundary two different ways — one
  through a membership fetch it needs anyway, one through an explicit
  comparison. A reader will not find a shared mechanism because there
  is not one.
- `authz.md`'s attachment table becomes a claim with a stated
  precondition rather than a promise the engine appears to make.

## Rejected alternatives

- **Give the engine what it needs to enforce its own scope.** This is
  the durable fix and the only one that survives a caller that
  forgets. It is rejected *for now* rather than on the merits: it moves
  membership across the boundary ADR-0007 drew, which is a larger
  decision than the one this ADR takes, and taking it here would
  settle it as a side effect of closing a bug. It is deferred to the
  project owner, and it stays open in the issue this ADR came from.
- **Compare organization segments inside the engine.** Refuses every
  user check, because a user GRN is root-owned. It is the obvious fix
  and it does not work, which is worth recording so it is not proposed
  again.
- **Make the scope narrower than documented — an unattached policy
  applies to nothing until attached.** It would close the gap by
  removing the feature, and it contradicts ADR-0008, where an
  unattached active policy applying org-wide is a deliberate decision
  with its own reasoning. A bug in how a rule is enforced is not a
  reason to delete the rule.
- **Leave it undocumented and fix each caller as it appears.** This is
  the status quo, and it has produced one breach in two callers. The
  next author will be as careful as the last and as uninformed.

## Errata

- 2026-08-18 — §Context's "Neither review nor the test suite would have caught it, and neither would catch the next one. The engine's tests exercise subjects that are members, which is precisely the assumption under test." is false. `TestOrgIsolation_GrantsNeverCrossOrgs` had no membership rows and deliberately asked about a foreign subject; the real omission was that every policy in its fixture was attached, and an attached policy does not match a foreign subject. One fixture line closed the gap — the test is now `TestOrgIsolation_OnlyAttachmentKeepsGrantsInsideAnOrg` (#296). The decision stands; what changes is the remedy's footing: the boundary is observable by the engine's own tests, not only by reviewer vigilance.
- 2026-08-31 — The 2026-08-18 entry above ends "the test is now
  `TestOrgIsolation_OnlyAttachmentKeepsGrantsInsideAnOrg` (#296)",
  which was true when written and is false since `1d67d5c` (#411):
  that commit deleted the function from `tests/e2e/postgres_test.go`
  and added `TestGRPCServer_RefusesForeignSubjectWhenSubjectOrgUnset`
  to `internal/authz/server_test.go`, where the boundary is tested
  now. The predecessor name that entry also cites,
  `TestOrgIsolation_GrantsNeverCrossOrgs`, went at `324adea` (#296) —
  the rename the entry itself records. What the entry concluded still
  holds: the boundary is observable by the engine's own tests, not
  only by reviewer vigilance. The decision is untouched.
