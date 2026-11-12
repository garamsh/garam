# ADR 0149: codehub is a service of the policy service, its repositories are entities and its paths are context

> Status: proposed; not built
> Date: 2026-11-13

Drafted for #1812, under #1760, which reserves this number. Nothing
below is built: no `Codehub` manifest, no `Caller` variant for codehub
and no HTTP binding exist in the policy service, and codehub's adapter
sends the requests described under "What codehub sends today". It
supersedes in part
[ADR-0141](./0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md)
(its worked example's answers 4 to 6, whose GRN forms
[ADR-0142](./0142-a-grn-names-its-organization-its-service-and-a-path-of-type-and-id-pairs-with-a-locator-the-service-reads.md)
had already replaced, and which this ADR decides in full). garam is
read at `931768b6183a39ea11f170e92323115df8c2c903`. codehub is read
through the GitHub API at
`76660678f22722c791083986a3b82208c5561ff8` (its `main`), except its
ADR 0042, which is not on `main` and is read at
`947291090b9549413553162ff5d4b59d7841f8b0`, the head of codehub's
open pull request 138. A claim below about codehub names its file and
holds for that commit; one about garam names no revision and holds for
the first.

## Context

- **What codehub sends today.** Read in
  `crates/adapter-authzen/src/authorisation_decision.rs` at the commit
  above:
  - the subject is `{type: "principal", id: <token sub>, properties:
    {issuer}}`;
  - the action is one of six snake-case names:
    `create_organisation`, `create_repository`, `administer_members`,
    `administer_runners`, `read` and `write`;
  - the resource is `{type: "organisation", id: <org>}` for an
    organisation, and `{type: "repository", id: "<org>/<name>",
    properties: {organisation, repository}}` for a repository, with
    `path` added to the properties where a path is asked about;
  - one bearer token serves every request, obtained by client
    credentials with a client secret.
- **codehub's decided shape.** ADR 0027 gives it one kind of principal
  and no kind in the identity port. ADR 0028 shares the organisation
  name with the outside layer with no mapping table. ADR 0030 has the
  external mode keep no members or grants in codehub, refuse its member
  endpoints, create an organisation lazily when `CreateOrganisation` is
  allowed, and report directory changes through a port whose delivery
  is at most once. ADR 0033 makes a failing decision point a server
  error. ADR 0034 makes a runner an ordinary principal registered in
  codehub's own registry and issues it no credential. ADR 0038 and ADR
  0041 batch the paths of a landing only where the operator turns it
  on, because Cerbos 0.57.0 collapsed evaluations that share a resource
  type and id. ADR 0042, in the pull request above, has the adapter
  authenticate by `private_key_jwt` with one token per organisation.
  All of these are in `docs/architecture/adr/` of codehub.
- **What codehub names.** An organisation and a repository name are 1
  to 63 bytes of lowercase ASCII letters, digits and `-`
  (`crates/app/src/domain/name.rs`), which is inside the 64-character
  slug of a GRN segment (`lib/grn/grn.go`, `isSlug`). A path is
  components that are bytes, none empty, `.`, `..` or holding `/`, and
  a prefix covers whole components, so `src` covers `src` and `src/a`
  and not `srcx/a` (`crates/app/src/domain/path.rs`). Its `Display`
  renders a component with `String::from_utf8_lossy`.
- **What garam already decided and this ADR does not reopen.**
  - A caller that is not the platform is a scoped caller: its `sub` is
    `grn:<org>/iam/service/<name>`, it asks only the actions of its own
    service on resources of its token's organization, and it writes
    only the types whose `@caller` is its own, into that organization
    (`policy/README.md` §Callers, `policy/crates/server/src/caller.rs`).
  - `Iam::Org` is `grn:root/iam/org/<slug>` and the platform's alone to
    write.
  - A locator, the part after `#`, is not an entity and reaches policy
    only as a context value (ADR-0142 item 5).
  - Each service ADR-0141 admits is a pull request with a manifest, an
    audience, a `Caller` variant and feeding (ADR-0143 item 7).
  - The policy service's HTTP AuthZEN binding is #1809 and answers each
    evaluation of a batch separately. An HTTP form of
    `ApplyEntityChanges` comes with it.
  - codehub holds one principal per organization,
    `grn:<org>/iam/service/codehub`, with one deployment key and a
    `private_key_jwt` assertion (#1811). A person's token is the
    session exchange's JWT with `aud` `codehub` and `sub` the person's
    GRN (ADR-0141 item 6).
- **The wire contract refuses what codehub sends.**
  `policy/proto/garam/policy/v1/policy.proto` refuses a resource's
  `properties` and a `context` holding a field, because the schema
  declares none, and a subject carries no properties. The subject's and
  the resource's `type` are Cedar entity type names, `Iam::User`,
  `Cowork::Channel`, and an action's name is `<service>:<type>.<verb>`
  (ADR-0145).
- **A `permit` must name an entity.** ADR-0144 item 2 refuses an
  organization's `permit` whose resource scope names no entity inside
  the organization. A path is no entity, so a grant of one directory of
  a repository cannot be a resource scope.

## Decision

**codehub is a service of the policy service: it declares a `Codehub`
manifest, asks `codehub:` actions with its organization's token, and
feeds its repositories as entities. A path is a context value, and an
organization is garam's.**

1. **The `Codehub` manifest.** A new
   `policy/schema/codehub.cedarschema`, under the shape of
   `cowork.cedarschema`, with `@service("codehub")`:
   - one entity type, `Codehub::Repo`, `@grn("repo")`,
     `@caller("codehub")`, in `[Iam::Org]` with `{ org: Iam::Org }`. Its
     GRN is `grn:<org>/codehub/repo/<name>`. A project as a second
     parent, and the `project/<id>/repo/<id>` name, are a later
     manifest change and not made here;
   - these actions, each `in` the role groups shown, with principals
     `[Iam::User, Iam::Agent]`:

     | Action | Resource types | Groups | Replaces |
     |---|---|---|---|
     | `repo.read` | `Iam::Org`, `Codehub::Repo` | owner, admin, member | `read` |
     | `repo.write` | `Iam::Org`, `Codehub::Repo` | owner, admin, member | `write` |
     | `repo.create` | `Iam::Org` | owner, admin | `create_repository` |
     | `runner.administer` | `Iam::Org` | owner, admin | `administer_runners` |

   - `repo.read` and `repo.write` declare a context of one optional
     string, `path`; the other two declare none;
   - `create_organisation` and `administer_members` have no action.
     The first is item 4. The second is never asked, because ADR 0030
     refuses codehub's member endpoints in the external mode and
     members are garam's (`Iam::OrgMembers` and the role links);
   - no `repo.*` group is declared: cowork declares none either, and
     the manifest check holds a group to its type's actions only where
     one exists;
   - `runner.administer` is of the type `runner`, for which no entity
     type exists. Whether the manifest check accepts an action whose
     type has no entity is not verified (see the questions).
2. **The mapping of codehub's request.** codehub changes its adapter
   as the table says, and sends it to the HTTP binding of #1809:

   | AuthZEN field | Value | Notes |
   |---|---|---|
   | `subject.type`, a person | `Iam::User` | read from the `sub`'s service and type segments, `iam` and `user` |
   | `subject.type`, an agent | `Iam::Agent` | the segments `iam` and `agent` |
   | `subject.id` | the token's `sub`, unchanged | `grn:<org>/iam/user/<id>` or `grn:<org>/iam/agent/<id>` (ADR-0141 item 6) |
   | `subject.properties` | not sent | the contract carries none |
   | `resource.type`, a repository | `Codehub::Repo` | |
   | `resource.id`, a repository | `grn:<org>/codehub/repo/<name>` | |
   | `resource.type`, an organization | `Iam::Org` | for `repo.create`, `runner.administer`, and a read or write asked of the organization |
   | `resource.id`, an organization | `grn:root/iam/org/<org>` | |
   | `resource.properties` | not sent | the contract refuses it |
   | `action.name` | `codehub:<type>.<verb>` | `codehub:repo.read`, `codehub:repo.write`, `codehub:repo.create`, `codehub:runner.administer` |
   | `context.path` | the path, `/`-separated | only for `repo.read` and `repo.write` of a path; absent for a question of the whole repository |

   - A runner is the subject of none of these: it is authorised by
     codehub's registry (item 6) and asks garam nothing.
   - codehub does not tell kinds apart in its core (ADR 0027). The
     adapter reads the Cedar type from the `sub`, which is a string the
     issuer wrote. A `sub` of another type, an operator for one, is
     asked as it is and denied, since no `Codehub` action applies to
     it.
   - The token an evaluation presents is the token of the organization
     of its resource (ADR 0042, #1811). A call that spans organizations
     is split by organization, as ADR 0042 decides.
3. **A path decides as a context value.** The path is the context key
   `path` of the question and nothing in an entity.
   - **A guardrail** is `forbid ... when { context has path && ... }`.
     The one string operator Cedar has for a prefix is `like`, with `*`
     matching any run of characters, `/` included. A prefix of
     codehub's whole-component kind is therefore two terms,
     `context.path == "src"` and `context.path like "src/*"`: the
     pattern alone does not match `src`, and `src*` would match
     `srcx/a`. A guardrail that names `context.path` without the
     `has` guard errors on a question of a whole repository, and an
     error is a deny (ADR-0137 item 2), so the guard is the author's.
   - **A `permit`** says the same in a `when` clause: its resource
     scope is the `Codehub::Repo` entity
     (`resource in Codehub::Repo::"grn:<org>/codehub/repo/<name>"`)
     and the directory is the condition. `docs/architecture/authz.md`
     says a `permit`'s `when` is bounded as a guardrail's is, and
     `policy/crates/grant/src/body.rs` bounds both by length and
     nesting. That the write accepts a condition on a context key
     the schema declares is not verified.
   - **The manifest declares the key.** Until `context` is declared on
     `repo.read` and `repo.write`, garam refuses `context.path`
     (`policy.proto`). The declaration is a manifest change in the
     policy service, as item 1 states.
   - **What is not decided in policy.** A ref is not carried: the
     adapter sends the organisation, the repository and the path and
     no ref (the same file), so ADR-0142's locator form
     `#refs/heads/main:cmd/main.go` is not what codehub sends and the
     key is `path` alone.
   - **Batching.** The path no longer shares a resource type and id
     with its siblings in the resource but sits in each evaluation's
     own context. Whether that makes codehub's
     `CODEHUB_AUTHZEN_EVALUATIONS` safe to turn on against garam
     rests on #1809 answering each evaluation separately, which is a
     fact of that issue and not read here; the setting stays codehub's
     operator's decision (ADR 0041).
4. **`CreateOrganisation` is not asked.** An organization is garam's,
   `grn:root/iam/org/<slug>`, created and named by garam; codehub
   creates none and reports none.
   - **What codehub asks.** The action of the request it is serving,
     with that organization's token, as item 2 states. It asks no
     action named for creating an organization, and no `codehub:`
     action can be asked of `grn:root/iam/org/<slug>` as a creation:
     a scoped caller asks only `codehub:` actions of resources of its
     token's organization.
   - **Which token.** The organization named in the request, which is
     the name codehub shares with garam (ADR 0028).
   - **When garam has no such organization.** codehub's principal
     `grn:<org>/iam/service/codehub` does not exist, so the token
     endpoint refuses the assertion with RFC 6749 §5.2
     `invalid_client`. That answer is definite, and the adapter reads
     it as a deny of the request, not as an outage: the caller is
     refused as for any organization it may not act in. A token
     endpoint that cannot be reached or answers 5xx remains a server
     error (ADR 0033).
   - **codehub's own organization row** may still be created lazily,
     after an allowed decision in an organization, as a record of what
     garam has already said exists. An allow is proof of the
     organization, so the mistyped-name hazard of ADR 0030's
     consequences is gone: a name garam does not hold gets no token.
5. **The entity feed.** codehub writes `Codehub::Repo` entities only,
   through the HTTP form of `ApplyEntityChanges`, with the token of
   the entity's organization.
   - **An upsert** is `entity` `Codehub::Repo` with id
     `grn:<org>/codehub/repo/<name>`, `org` `Iam::Org` with id
     `grn:root/iam/org/<org>`, and `parents` that one `Iam::Org`. It
     carries no `members`, no `consents` and no `operations`, which are
     the platform's.
   - **A delete** is `EntityDelete` of the same entity.
   - **The sequence is per entity** and rises with every change
     codehub makes to it, from a transactional outbox (ADR-0138). A
     delete keeps its sequence as a tombstone, so a name created after
     its deletion is an upsert at a higher sequence. codehub therefore
     keeps the counter in a table keyed by the repository's name and
     not in the repository's row, which a delete removes.
   - **What the port needs.** ADR 0030's at-most-once port cannot
     repair a lost change, since nothing remembers it. It is replaced
     for the external mode by an outbox written in the transaction of
     the repository's creation or deletion, and a relay that delivers
     in sequence order per entity and retries until garam answers
     applied or dropped. A replayed or reordered delivery changes
     nothing (`policy.proto`, `ApplyEntityChanges`), so at-least-once
     delivery is safe. The in-process and no-op adapters keep their
     part in the other mode.
   - **Organization changes are not reported.** `OrganisationCreated`
     and `OrganisationDeleted` are the platform's to write and cease
     to be codehub's.
   - **A repository garam does not yet hold** is a resource with no
     parent, outside every role's `resource in Iam::Org`, and is
     denied. The window is the delivery delay, and the order of
     codehub's create is its own: it delivers once after the commit,
     and the outbox repairs a failure.
6. **A runner is a garam agent that codehub registers.** A runner is
   an `Iam::Agent` of the organization, `grn:<org>/iam/agent/<id>`,
   which garam already issues credentials to. It exchanges its agent
   token for a JWT with `aud` `codehub` and presents that to
   `/v1/runner/` (ADR-0141 items 3 and 8, codehub ADR 0034).
   - **Registration stays codehub's.** The runner registry holds the
     registrations and is kept in both modes (ADR 0034). The registered
     principal is the issuer and the `sub` of that token.
   - **A runner asks garam nothing.** Its calls are authorised by the
     registry, so no entity, action or subject mapping exists for it,
     and the source of a job is by presigned URLs (ADR 0034).
   - **Registering it is `codehub:runner.administer`** on the
     organization, asked of the administrator.

## Consequences

- **A change is needed in four places before any of this works.** The
  policy service needs the manifest, a `Caller` variant for codehub, a
  row in `policy/README.md`'s caller table, the declared context and
  the removal of the audience check's exemption for `codehub`
  (ADR-0143 item 7). The platform needs codehub's service principal in
  each organization (#1811). The HTTP binding of #1809 must accept a
  context key and the `Iam::` and `Codehub::` type names, and must
  carry the `ApplyEntityChanges` form. codehub needs the adapter change
  of item 2, the outbox of item 5 and the `invalid_client` reading of
  item 4. None is built.
- **A guardrail on a path is two terms and a guard.** The pattern of
  Cedar is not whole-component prefix, so every author writes the pair.
  A lossy rendering of a non-UTF-8 component can make two paths
  one string.
- **Policy never sees the path of a stored fact.** Nothing about a
  path is in an entity, so the access report
  (`policy/crates/access-report/`) lists the policies that name a
  repository and cannot list those that name a directory.
- **A freshly created repository can be denied** until its entity
  arrives, and a repository deleted in codehub stays readable in garam
  until its delete does.
- **A runner is a member of its organization.** `Iam::Agent` is in
  `Iam::OrgMembers`, so the default roles give it `repo.read` and
  `repo.write` on the same API a person uses. The registry authorises
  `/v1/runner/` and does not stop it using the rest. An organization
  narrows it with a guardrail, or codehub refuses a registered runner
  elsewhere; the second is codehub's choice.
- **A repository name is garam's identifier and is not reused**
  (`grn-model.md` §identifier), whose enforcement is undecided there.
  A name codehub recreates after a deletion is the same GRN at a higher
  sequence.

## Signs the decision was wrong

- A decision needs the ref, or a second per-request fact, and the
  context holds more than `path`, which would show the key was the
  first of several and wants a declared shape.
- Organizations write path conditions in a large share of their
  guardrails and the access report cannot answer who may write a
  directory, which would show a path wants to be an entity after all.
- A runner agent is found writing to a repository it was never
  granted, which would show an agent is the wrong principal for a
  runner.
- A mistyped organization in codehub is read as an outage often enough
  to alarm, which would show `invalid_client` is not a definite answer
  of garam's token endpoint.
- The outbox's backlog is long enough that a created repository is
  denied for longer than a person waits.

## Alternatives rejected

- **The path as a resource attribute.** The contract refuses a
  resource's `properties`, and an entity's attributes are what its
  owner writes, so a per-request path would need an entity per path.
  A path is not an entity (ADR-0142).
- **A path in the resource id,** `grn:<org>/codehub/repo/<name>#<path>`.
  The locator is no entity and the engine reads the id before `#`, so
  the path would still reach policy only as context, by a parse of the
  id that ADR-0142 refuses.
- **Keeping `create_organisation` as `codehub:org.create`.** Garam
  creates organizations, so the action would ask garam whether garam
  may do what it already did. It would also be an action of a type
  codehub owns no entity of.
- **A codehub-named `Codehub::Runner` entity.** The runner asks garam
  nothing, so the entity would be written for no decision, and
  codehub's registry already records it. A credential for it would be
  one codehub issues, which ADR 0034 refuses.
- **A new `iam/runner` principal kind.** It costs an ADR, a type and a
  caller entry for what an agent's credentials already do. It is the
  answer if the signs above show the agent wrong.
- **A service principal `grn:<org>/iam/service/codehub-runner`.** A
  service principal is one per organization and service with one
  deployment key. A fleet of runners would share it, and revoking one
  runner would revoke all.
- **Keeping the at-most-once port.** A lost change would leave garam
  without a repository codehub holds, and nothing repairs it. ADR 0030
  rejected a durable queue for needing a store before any adapter
  needed it; the external mode against garam is the adapter that does,
  and codehub has a database.

## Open questions

For codehub's PM. Neither repository settles them.

1. Does codehub accept reading the subject's Cedar type from the
   `sub`, or would it rather the HTTP binding derive the type from the
   GRN and ignore `subject.type`?
2. Does codehub send `path` as the `Display` of its path, with a
   lossy rendering of non-UTF-8 bytes, or refuse a path that is not
   UTF-8 where it asks garam?
3. Should `role-member` hold `repo.write`? The recommendation follows
   cowork, which gives its members write, and an organization narrows
   it by a guardrail; the alternative is that only owners and
   administrators write until an owner writes a `permit`.
4. May the `invalid_client` of item 4 be read as a deny, which
   supersedes ADR 0033's clause for that one answer, and is the lazy
   organization row kept?
5. Is a created repository's window of denial acceptable, or must the
   create wait on delivery?
6. Should codehub refuse a registered runner's token on the rest of
   its API?
7. Is `runner.administer`, an action whose type has no entity,
   accepted by the manifest check, and do the write of a guardrail and
   of a `permit` accept a `when` clause naming `context.path`, and
   does the analysis suite (`policy/README.md` §Analysis) hold with a
   declared context? These are the policy service's to answer and none
   is verified here.
