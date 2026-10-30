# ADR 0130: The policy engine is a Rust service deciding Cedar policies behind an AuthZEN-shaped gRPC contract

> Status: accepted; not yet built; tenant isolation's form, the role
> templates' scopes and the GRN examples are superseded by
> [ADR-0137](./0137-tenant-isolation-is-membership-and-the-policy-engine-fails-closed-on-any-evaluation-error.md)
> Date: 2026-10-28

Drafted for #1689, which reserves this number. The direction is the
owner's of 2026-10-28, recorded on #1689 with the alternatives
rejected. garam is read at
`06f086bdc05b8b560cc6aeb978af388991a1dff3`; a claim below that names
no revision holds for it.

## Context

- **The engine is a process of its own, but not a service of its
  own.** `garam serve authz` answers gRPC (`proto/garam/authz/v1`) and
  the api server never imports it (ADR-0003). It is built from the
  platform's module, stores policies in the platform's database, and
  ships in the platform's image.
- **It decides on what its caller sends.** The api server collects the
  statements a subject's roles grant (ADR-0007), and each caller sends
  `subject_groups` and `subject_org` (ADR-0025, ADR-0038). The engine
  checks that `subject_org` names the object's organization; it cannot
  check that the subject belongs to it. So only a caller that resolves
  membership correctly can ask it safely, and cowork (ADR-0127) or any
  later service would each have to.
- **Its contract is its document format.** `CheckRequest` carries IAM
  statements, so the evaluator cannot change without every caller
  changing.
- **Its language stops at matching.** `internal/authz/evaluator.go` and
  `statement.go` hold 132 lines that match action and resource strings
  under Deny-overrides (ADR-0002). `Condition` is reserved and read by
  nothing (`authz.md` §Policy document). The guardrails the owner wants
  — a team's agents kept out of external channels, delegated authority
  refused outside working hours — need conditions, and an
  organization's tenant isolation is today a caller obligation rather
  than a rule anything can prove.
- **The state has to be readable.** The owner's requirement is that a
  person or an agent can read who may do what as an artifact, not
  only ask an engine. What makes Kubernetes RBAC and AWS IAM readable
  is that each grant is one document.

## Decision

**The policy engine becomes `policy/`, a Rust service of its own. It
serves an AuthZEN-shaped gRPC contract, decides with the reference
Cedar implementation, and holds every grant as a readable Cedar
policy. It reads who belongs where itself, from an entity store its
owners feed, and no caller supplies the grounds of its own decision.**

1. **The service.** `policy/` is a top-level folder (ADR-0125) holding
   one Cargo workspace, its own binary and image, and its own
   database. It is named `policy` everywhere: `garam.policy.v1`,
   `PolicyService`, the `garam-policy` image, `POLICY_ADDR`.
2. **The contract.** gRPC stays as the transport. The messages take
   the shape of OpenID AuthZEN Authorization API 1.0 (final, January
   2026): `Evaluate(subject, action, resource, context)` answers a
   decision with its reasons, and `Evaluations` is the batch
   (ADR-0115's list filtering asks it). An option on the request reads
   policies and entities from storage instead of the cache, which is
   what `CheckCurrent` is today (ADR-0078). An HTTP AuthZEN gateway is
   added only when a caller that is not a Go service needs one.
3. **The language.** Policies are Cedar, evaluated by the
   `cedar-policy` crate, the reference implementation. The schema
   (`policy/schema/garam.cedarschema`) is the vocabulary of entity
   types, actions and attributes, and every write is validated against
   it. A caller can name only what the schema names.
4. **Every grant is a policy.**
   - A role is a Cedar template the system tier ships:
     `permit (principal == ?principal, action in Action::"project-editor", resource in ?resource);`.
   - A role assignment links that template to one subject and one
     resource, so each grant is one readable policy.
   - A guardrail is a `forbid … when { … }` an organization writes.
   - Tenant isolation is a system `forbid … unless { principal.org ==
     resource.org }` that no policy can override.
   - An organization only narrows the system tier: it links
     templates and writes guardrails, and writes no `permit` of its
     own.
5. **Two tiers.** System policies live in the repository
   (`policy/system-policies/`), tested in CI and checked by Cedar's
   analysis. Organization policies are data: versioned append-only as
   `policy_versions` is today (ADR-0002), audited, and deleted as
   ADR-0117 deletes a policy.
6. **Entities.** A GRN is an entity's identifier
   (`User::"grn:acme:user/kim"`), and what contains it is given by its
   parents, never parsed from the string: `resource in
   Org::"grn:acme"` replaces `grn:acme:*`. The hierarchy is fixed and
   shallow: organization, then team and project, then channel. There
   are no groups of groups.
7. **Who owns what.** Every grant is the policy service's: roles,
   their assignments, guardrails. Containment and membership remain
   their owners' — the platform's organizations, teams and projects,
   cowork's channels. Each owner writes the part a decision reads into
   the policy service's entity store through a transactional outbox,
   in the transaction that changes it, so the store is rebuildable
   from its owners.
8. **Callers are authenticated, and send no grounds.** A caller is a
   service principal with a platform-issued token (ADR-0125's service
   rule). It sends the subject, the action, the resource and request
   context, and only entities of a type it owns. The service reads the
   subject's memberships and the resource's ancestors from its own
   store.
9. **What a person or an agent reads.** An access report lists the
   policies naming a subject (with its teams) or a resource (with its
   ancestors). It is a lookup over grants, not a traversal.
10. **`policy/` sits below the platform.** The platform and cowork
    call it, so a dependency points from `platform/` to `policy/`.
    `policy/` calls no service. It verifies the tokens the platform
    issues against keys it is configured with, which it reads as
    configuration and not through a call, and the outbox feeds reach
    it as writes its owners make. Dependencies point toward
    `platform/` or `policy/`, and none points from `policy/` to
    another service.
11. **Layout.** The workspace follows `docs/convention/stack-rust.md`,
    with one crate per responsibility under `policy/crates/`:
    `decision`, `grant`, `entity`, `access-report` and `server`.

This supersedes ADR-0002 on the document format and the evaluator,
ADR-0003 on the engine's binary, ADR-0007 on callers collecting role
statements, ADR-0008 on attachments, which become template links and
principal scopes, ADR-0025 and ADR-0038 on the caller's obligation to
resolve membership, and ADR-0125 on the direction of dependencies, which
now point toward `policy/` as well as `platform/`. Each keeps its status
line pointing here. ADR-0024's decision stands: an agent's sending
inside its organization becomes a system policy, and an organization
narrows it with a `forbid`.

## Alternatives rejected

- **Keeping the IAM-style evaluator and adding conditions to it.**
  That is designing a policy language, its validation and its
  analysis, which Cedar has.
- **A Zanzibar-family engine (SpiceDB, OpenFGA).** It would answer
  lists natively, but who may do what would exist only as a traversal
  of relation tuples through schema rewrites. An agent managing
  permissions could only query that state, never read it.
- **Amazon Verified Permissions.** It ties decisions to an AWS
  account and keeps the policies outside garam's database.
- **`cedar-go` in a Go service.** It would share the platform's
  language. The Rust crates are the reference implementation and carry
  the validator, partial evaluation and the symbolic analysis the
  system tier's proofs need.
- **A `policy-engine` name.** It spells three ways (folder, crate
  import, proto package) and names only the deciding half of a service
  that also holds grants.

## Consequences

- The repository gains a second backend language, under
  `stack-rust.md` (#1690). CI, Dependabot (`cargo`), the toolchain
  check and `images.yml` each need a Rust entry.
- Stored IAM documents are converted to Cedar, each as a new version,
  so history stays readable. A deployment with no organization
  policies converts nothing.
- The api server and the machine listener stop collecting role
  statements and resolving groups for the engine. They ask, and
  publish their changes.
- The platform and cowork each gain an outbox of the containment and
  membership the policy service reads. That is the cost of the service
  reading for itself.
- The console's policy screens (ADR-0073) change to role assignment
  and guardrail forms that compile to Cedar.
- Until `policy/` replaces it, `garam serve authz` keeps serving, and
  `authz.md` describes it.

## Signs the decision was wrong

- Organizations ask to write `permit` policies of their own often
  enough that the two tiers keep being bypassed.
- The outbox lags so far behind its owners that decisions are made on
  stale membership, and the request option meant for current
  authority becomes the default.
- The access report needs entity traversal to answer, because the
  hierarchy grew deeper than the four levels fixed here.
- Rust's cost — a second toolchain, a second set of conventions —
  outweighs what the reference implementation gives, shown by the
  validator and analysis going unused.
