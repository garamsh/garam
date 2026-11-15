# ADR 0161: The platform alone writes under the identifiers of a member's role links, and only that member's link

> Status: accepted; built in `policy/crates/grant` and
> `policy/crates/server` (#1882, second half); supersedes
> [ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md)
> in part: its "a writer may write any identifier of its organization"
> is narrowed for the identifiers below; extended by
> [ADR-0163](./0163-the-platform-alone-writes-under-the-identifier-of-the-scoped-link-it-derives-from-a-groups-or-a-projects-roles.md),
> which reserves a fourth prefix for a team's or a project's link and
> changes nothing held here for a member's
> Date: 2026-11-14

Drafted for #1882, which reserves this number. It records the
policy service's half of the issue: what the first half
([ADR-0160](./0160-a-role-link-the-policy-service-refuses-for-good-is-set-aside-and-the-members-links-are-removed.md))
left open, that the platform names a member's link by an identifier
anyone with a writer's right can also write under.
[authz.md](../authz.md) §Role links states the result as built.

## Context

The platform writes a member's link as three policies, one per role,
named `grn:<org>/iam/policy/link-<role>-<hex>`, with `<hex>` the first
16 bytes of the SHA-256 of the member's GRN. It calls `CreatePolicy`
(`ALREADY_EXISTS` is a link written already), `ActivatePolicy` and
`DeletePolicy` (`NOT_FOUND` is a link removed already), naming no
writer. Before this decision nothing in the service tied that name to
what it holds:

- An owner or an administrator naming a writer could create a policy
  under that identifier, a guardrail, a `permit` or another member's
  link. The platform's create then read `ALREADY_EXISTS` as its own
  link written, and its activate made the writer's policy effective as
  the member's, or its delete removed it.
- The identifier of a member is computable by anyone who knows the
  member's GRN.
- ADR-0160 sets a link aside on `PERMISSION_DENIED` or
  `INVALID_ARGUMENT` and removes the member's other links. Any
  `FAILED_PRECONDITION` or other code outside that list stops the
  whole feed, every organization's behind it, until it is mended.

## Decision

1. **An identifier is reserved when it opens with `link-owner-`,
   `link-admin-` or `link-member-`** in an organization's own
   policies (`grn:<org>/iam/policy/<id>`, the id with no `/`). The
   tail is not read to reserve: a near miss, such as a hash cut short,
   is reserved and held by nobody, since the one tail a member has is
   the one item 2 admits. `link-x`, `link-` and `linkadmin-x` are not
   reserved, so an identifier the organization may already hold stays
   writable.
   - Why this width. It is the narrowest prefix set that covers every
     identifier the platform names, and it is the shape an identifier
     is held to, not a guess at the hash's. Narrower, by the exact 32
     hex digits, would leave `link-admin-<anything else>` to writers,
     in the platform's own namespace. Wider, by `link-`, would take
     identifiers that name nothing of the platform's.
2. **Only the caller `platform` with no writer writes under a reserved
   identifier, and only the member's own link.** A `CreatePolicy`
   there is accepted when the body is a role link of the identifier's
   role, in organization scope, whose principal is an `Iam::User`
   whose GRN's hash is the identifier's tail and whose resource is the
   organization (`grant::names_link`). Any other body, a guardrail, a
   `permit`, another role, another member or another scope, is
   refused, as is a tail that is no member's hash.
3. **A write that names a writer under a reserved identifier is
   refused, whatever the writer holds and whatever the write.**
   `CreatePolicy`, `AppendPolicyVersion`, `ActivatePolicy`,
   `RollBackPolicy` and `DeletePolicy` all refuse, even for the owner
   and even for the link the identifier names. A scoped caller
   (`cowork`, `codehub`) was refused every write whole already and
   stays so.
4. **The platform's activation and removal, with no writer, are
   accepted only for a policy every version of which is the member's
   link** (`names_link`, over every version and not the default
   alone). A policy under a reserved identifier that predates this
   decision, or that is anything else, is neither made effective nor
   removed by the platform. The platform's unchanged
   create-then-activate is safe: the create finds it and is told it
   exists, and the activation is refused.
5. **Every refusal above is `PERMISSION_DENIED`.** It is on
   ADR-0160's list of permanent codes, so the platform sets the link
   aside, removes the member's links of all three roles (a removal
   the service refuses for the same reason is tolerated and leaves
   that policy as it was) and goes on with the other organizations;
   the member is denied there until the membership is written again
   or the feed is restarted. The answer of each call:

   | call | what the identifier holds | answer |
   | --- | --- | --- |
   | create, no writer | the member's link as body, nothing stored | created |
   | create, no writer | any body that is not the member's link | `PERMISSION_DENIED` |
   | create, no writer | a policy already there, body the member's link | `ALREADY_EXISTS` |
   | activate or delete, no writer | every version the member's link | done |
   | activate or delete, no writer | any version not the member's link | `PERMISSION_DENIED` |
   | any write naming a writer | anything, or nothing | `PERMISSION_DENIED` |

   `FAILED_PRECONDITION` was rejected for the case of item 4: it is
   not on ADR-0160's list, so the platform would retry it for ever
   and every other organization's links would wait behind one policy
   that no retry clears. `INVALID_ARGUMENT` was rejected as the
   less true: the request is well formed, and what the service
   refuses is the caller's right to the identifier.
6. **A refusal naming a writer is a denial of the step's action on the
   organization, recorded** (ADR-0148 item 1, "a write refused for
   lack of the right to make it"): `iam:policy.attach` for a role
   link's create, append, activation and roll-back, `iam:policy.detach`
   for its removal, and the action a guardrail's or a `permit`'s step
   takes, named by the policy's own kind. **A refusal of the platform
   with no writer is not recorded**: there is no subject to record, as
   for every refusal of a write with no writer (ADR-0148, the record
   of a write with no subject), and it is logged at info level as a
   refused writer is. Nothing is stored, so no write record exists
   for it either.
7. **No migration and no start-time check.** Nothing is deleted or
   rewritten. A policy under a reserved identifier that exists today
   keeps its versions and its state and keeps deciding as it did, an
   active one included. It cannot be changed or removed through the
   service by anyone, and an operator reads it with `ListPolicies` and
   `GetPolicy`. A start-time check would find the
   same set and could only log it, which the refusal of item 4 does
   at the moment it matters, in the platform's set-aside record.

## Consequences

- A writer cannot make the platform's link call act on its policy, nor
  take a member's role by naming a policy as the member's.
- An identifier of the reserved shape that a writer created before this
  decision cannot be removed by a writer. It is removed from the
  database by an operator, which this decision does not do for them.
- A refused platform call costs the member the link, since
  `PERMISSION_DENIED` sets it aside: the member is denied (fail
  closed) until an operator removes the policy that occupies the
  identifier and runs `garam policy-feed start`.
- The service holds the platform's naming, `link_policy`, beside its
  own reading of it; the two are held to one value by a unit test and
  by `platform/internal/policyfeed/grpc/links.go`, whose hash is the
  same function. A change of the platform's naming without this one
  sets every link aside.
- `sha2` becomes a dependency of `grant`, already in the lock file
  through other crates.

## Signs the decision was wrong

- A set-aside link whose reason names a reserved identifier, found
  among members the organization did not squat: the platform's naming
  and this one have drifted.
- An organization that asks to remove or reuse a policy under a
  reserved identifier: the prefix is wider than the identifiers it
  needs, and the remedy is a narrower reserved form with a tail check.
- A caller other than `platform` that must write a link: the closed
  list of item 2 is then the question, and not this one.
