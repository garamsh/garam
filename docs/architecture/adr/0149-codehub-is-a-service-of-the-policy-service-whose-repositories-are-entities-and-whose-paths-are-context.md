# ADR 0149: codehub is a service of the policy service, its repositories are entities and its paths are context

> Status: accepted; built in the policy service (#1815), not in
> codehub, its outbox or the runner
> Date: 2026-11-13

Drafted for #1812, under #1760, which reserves this number, and
decided with the PM and with codehub's answers on #1760
([comment 6830559368](https://github.com/garamsh/garam/issues/1760#issuecomment-6830559368)),
cited below as "answer N". Nothing below is built: no `Codehub`
manifest, no `Caller` variant for codehub and no HTTP binding exist in
the policy service, and codehub's adapter sends the requests described
under "What codehub sends today". It supersedes in part
[ADR-0141](./0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md)
(its worked example's answers 4 and 5, whose GRN forms
[ADR-0142](./0142-a-grn-names-its-organization-its-service-and-a-path-of-type-and-id-pairs-with-a-locator-the-service-reads.md)
had already replaced, and which this ADR decides in full; answers 1 to
3 and 6 stand). garam is read at
`931768b6183a39ea11f170e92323115df8c2c903`. codehub is read through
the GitHub API at `76660678f22722c791083986a3b82208c5561ff8`, except
that its ADR 0042, which was not on `main` there, is read at
`947291090b9549413553162ff5d4b59d7841f8b0`, and that codehub's answers
read facts at `171fb60967154bd45d285cb3f79e5985e2f7449b`. A claim
below about codehub names its file and holds for the commit its
paragraph names; one about garam names no revision and holds for the
first.

## Context

- **What codehub sends today.** Read in
  `crates/adapter-authzen/src/authorisation_decision.rs`:
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
  type and id. ADR 0042 has the adapter authenticate by
  `private_key_jwt` with one token per organisation. All of these are
  in `docs/architecture/adr/` of codehub.
- **What codehub names.** An organisation and a repository name are 1
  to 63 bytes of lowercase ASCII letters, digits and `-`
  (`crates/app/src/domain/name.rs`), which is inside the 64-character
  slug of a GRN segment (`lib/grn/grn.go`, `isSlug`). A path is
  components that are bytes, none empty, `.`, `..` or holding `/`, and
  a prefix covers whole components, so `src` covers `src` and `src/a`
  and not `srcx/a` (`crates/app/src/domain/path.rs`, where a component
  is `Component(Vec<u8>)`).
  A repository also has an identity column, `bigint generated always as
  identity`, which is never reused (`crates/adapter-postgres/migrations/`,
  `0001_*.sql`; answer 7). Nothing renames or deletes a repository
  today, and the id is not in codehub's public API yet.
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
  - A manifest's `@grn` names a type's GRN segment and is unique within
    a service (ADR-0143 item 2).
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
  - An identifier is not reused (`grn-model.md` §identifier).
- **The wire contract refuses what codehub sends.**
  `policy/proto/garam/policy/v1/policy.proto` refuses a resource's
  `properties` and a `context` holding a field, because the schema
  declares none, and a subject carries no properties. The one place
  that refuses a context is `policy/crates/server/src/policy_service.rs`
  lines 266 to 268, and line 280 passes an empty context whatever was
  sent. The subject's and the resource's `type` are Cedar entity type
  names, `Iam::User`, `Cowork::Channel`, and an action's name is
  `<service>:<type>.<verb>` (ADR-0145).
- **A `permit` must name an entity.** ADR-0144 item 2 refuses an
  organization's `permit` whose resource scope names no entity inside
  the organization. A path is no entity, so a grant of one directory of
  a repository cannot be a resource scope.
- **What was probed.** On a scratch worktree at the garam commit above,
  with the manifest of item 1 added and nothing committed:
  - `make -C policy gen` accepts `runner.administer` with no `runner`
    entity type, because `Manifests::problems()` holds an action id to
    its form and a `<type>.*` group to its type's actions and asks no
    entity of the type;
  - a declared `context: { path?: String }` loads, and the engine
    admits `{"path": "docs/a"}` on `repo.write` and refuses a
    non-string, an unknown key, and a `path` on `repo.create`;
  - `Body::guardrail` accepts a `forbid` with
    `context has path && (context.path == "secrets" || context.path like
    "secrets/*")`, scoped to `repo.write` or with an open action, and
    refuses the same without the `has` guard ("unable to guarantee
    safety of access to optional attribute `path`"); `Body::permit`
    accepts the guarded form on a `Codehub::Repo` resource;
  - the analysis suite (`policy/README.md` §Analysis) passes 8 of 8
    with the manifest added.

## Decision

**codehub is a service of the policy service: it declares a `Codehub`
manifest, asks `codehub:` actions with its organization's token, and
feeds its repositories as entities. A repository is named by codehub's
immutable id, a path is a context value, and an organization is
garam's.**

1. **The `Codehub` manifest.** A new
   `policy/schema/codehub.cedarschema`, under the shape of
   `cowork.cedarschema`, with `@service("codehub")`:
   - one entity type, `Codehub::Repo`, `@grn("repo")`,
     `@caller("codehub")`, in `[Iam::Org]` with `{ org: Iam::Org }`. Its
     GRN is `grn:<org>/codehub/repo/<id>`, item 2. A project as a
     second parent is a later manifest change and not made here;
   - these actions, each `in` the role groups shown, with principals
     `[Iam::User, Iam::Agent]`:

     | Action | Resource types | Groups | Replaces |
     |---|---|---|---|
     | `repo.read` | `Iam::Org`, `Codehub::Repo` | owner, admin, member | `read` |
     | `repo.write` | `Iam::Org`, `Codehub::Repo` | owner, admin | `write` |
     | `repo.create` | `Iam::Org` | owner, admin | `create_repository` |
     | `runner.administer` | `Iam::Org` | owner, admin | `administer_runners` |
     | `org.use` | `Iam::Org` | owner, admin, member | `create_organisation` |

   - **`role-member` holds `repo.read` and not `repo.write`.** A member
     or an agent writes by an owner's `permit` on a repository. Every
     agent is in `Iam::OrgMembers`, so a member's write would give
     every agent every repository, and a default is harder to take
     back than to give. codehub holds no fact against it (answer 3);
     cowork's grant of write to its members is no reason, since its
     writes are of messages and not of source;
   - `repo.read` and `repo.write` declare a context of one optional
     string, `path`; the others declare none;
   - `administer_members` has no action. It is never asked, because
     ADR 0030 refuses codehub's member endpoints in the external mode
     and members are garam's (`Iam::OrgMembers` and the role links);
   - no `repo.*` group is declared: cowork declares none either, and
     the manifest check holds a group to its type's actions only where
     one exists;
   - `runner.administer` is of the type `runner`, for which no entity
     type exists, and the manifest check accepts it (see "What was
     probed").
2. **A repository is named by codehub's id, and the binding resolves
   the type from the GRN.**
   - **The GRN is `grn:<org>/codehub/repo/<id>`**, the id being the
     decimal form of the repository's identity column. It is not the
     name. An identifier is not reused, so a GRN on the name would be
     one GRN for a repository deleted and recreated, and a `permit` for
     the first would hold for the second. codehub's id is never reused
     (answer 7). codehub puts the id in the GRN it sends as
     `resource.id` and as the entity id of every directory change, and
     sends no property: answer 7 offered a property, and garam's
     contract takes the GRN instead.
   - **A policy names a number.** A reader needs codehub to learn which
     repository `grn:acme/codehub/repo/4821` is. The id is unique
     within one codehub deployment, so one garam organization is served
     by one codehub deployment.
   - **codehub sends the type `grn`.** It does not tell a person from
     an agent (its ADR 0027) and will not parse a GRN (answer 1), so
     the HTTP binding accepts the type `grn` for a subject and for a
     resource and resolves the Cedar entity type from the id's service
     and last type segment through the manifests' `@grn`, which is
     unique within a service (ADR-0143 item 2). A Cedar type name stays
     accepted. The gRPC contract is unchanged. This is garam's rule and
     not codehub's: the manifest maps `group` to `Iam::Team`, and no
     caller should copy that table.
   - **The mapping of codehub's request** to the HTTP binding of #1809:

     | AuthZEN field | Value | Notes |
     |---|---|---|
     | `subject.type` | `grn` | codehub configures this as its one subject type (answer 1) |
     | `subject.id` | the token's `sub`, unchanged | `grn:<org>/iam/user/<id>` or `grn:<org>/iam/agent/<id>` (ADR-0141 item 6); resolved to `Iam::User` or `Iam::Agent` |
     | `subject.properties` | not sent | the contract carries none |
     | `resource.type` | `grn` | for a repository and for an organization |
     | `resource.id`, a repository | `grn:<org>/codehub/repo/<id>` | resolved to `Codehub::Repo` |
     | `resource.id`, an organization | `grn:root/iam/org/<org>` | resolved to `Iam::Org`; for `repo.create`, `runner.administer`, `org.use` and a read or write asked of the organization |
     | `resource.properties` | not sent | the contract refuses it |
     | `action.name` | `codehub:<type>.<verb>` | `codehub:repo.read`, `codehub:repo.write`, `codehub:repo.create`, `codehub:runner.administer`, `codehub:org.use` |
     | `context.path` | the path, `/`-separated | only for `repo.read` and `repo.write` of a path; absent for a question of the whole repository |

   - A runner is the subject of none of these: it is authorised by
     codehub's registry (item 6) and asks garam nothing.
   - A `sub` of another type, an operator for one, is asked as it is
     and denied, since no `Codehub` action applies to it.
   - The token an evaluation presents is the token of the organization
     of its resource (ADR 0042, #1811). A call that spans organizations
     is split by organization, as ADR 0042 decides.
3. **A path decides as a context value, and it is exact.** The path is
   the context key `path` of the question and nothing in an entity.
   - **It is exact.** `context.path` is the path's components joined by
     `/`, and is valid UTF-8. codehub denies a path that is not UTF-8
     without asking (answer 2), so no lossy string reaches policy and
     two paths are never one string.
   - **A guardrail** is `forbid ... when { context has path && ... }`.
     The one string operator Cedar has for a prefix is `like`, with `*`
     matching any run of characters, `/` included. A prefix of
     codehub's whole-component kind is therefore two terms,
     `context.path == "src"` and `context.path like "src/*"`: the
     pattern alone does not match `src`, and `src*` would match
     `srcx/a`. The `has` guard is enforced by the write: a guardrail
     that names `context.path` without it is refused ("What was
     probed").
   - **A `permit`** says the same in a `when` clause: its resource
     scope is the `Codehub::Repo` entity
     (`resource in Codehub::Repo::"grn:<org>/codehub/repo/<id>"`)
     and the directory is the condition. `docs/architecture/authz.md`
     says a `permit`'s `when` is bounded as a guardrail's is, and
     `policy/crates/grant/src/body.rs` bounds both by length and
     nesting. `Body::permit` accepts the guarded form.
   - **The manifest declares the key, and the service lifts one
     refusal.** The declaration is the manifest's, as item 1 states.
     `policy_service.rs` lines 266 to 280 refuse a context and pass an
     empty one, and a context declared in the manifest is read there
     instead.
   - **A question that carries no `path` is of the whole repository,
     and a guardrail on a path does not deny it.** What a path rule
     protects is what codehub asks by path. A rule on `secrets` holds
     for a read or write codehub asks of `secrets/a`, and not for one
     it asks of the repository.
   - **A ref is not carried.** The adapter sends the organisation, the
     repository and the path and no ref, so ADR-0142's locator form
     `#refs/heads/main:cmd/main.go` is not what codehub sends and the
     key is `path` alone.
   - **Batching.** The path no longer shares a resource type and id
     with its siblings in the resource but sits in each evaluation's
     own context. Whether that makes codehub's
     `CODEHUB_AUTHZEN_EVALUATIONS` safe to turn on against garam
     rests on #1809 answering each evaluation separately, which is a
     fact of that issue and not read here; the setting stays codehub's
     operator's decision (ADR 0041).
4. **An organization gains one action, `codehub:org.use`, and garam
   creates nothing.** codehub's `create_organisation` becomes
   `codehub:org.use`, on `Iam::Org`, held by all three role groups.
   - **What codehub asks.** It asks the action once when it first meets
     an organization, and records the organization on allow (answer 4),
     so its code does not change.
   - **What it means.** An organization gains the one action a guardrail
     forbids to keep a principal out of codehub. It creates nothing in
     garam: an organization is garam's, `grn:root/iam/org/<slug>`,
     created and named by garam.
   - **Which token.** The organization named in the request, which is
     the name codehub shares with garam (ADR 0028).
   - **When garam holds no such organization,** the token endpoint
     answers a request for it `unauthorized_client` (HTTP 400, RFC 6749
     §5.2) once the assertion's signature has verified. Every failure
     to authenticate stays `invalid_client` (#1811,
     [comment 6830580429](https://github.com/garamsh/garam/issues/1811#issuecomment-6830580429)).
     codehub reads `unauthorized_client` as a deny (403) and
     `invalid_client` as a server error, so ADR 0033 stands for every
     answer but the one that is a definite refusal. A token endpoint
     that cannot be reached or answers 5xx remains a server error.
5. **The entity feed.** codehub writes `Codehub::Repo` entities only,
   through the HTTP form of `ApplyEntityChanges`, with the token of
   the entity's organization.
   - **An upsert** is `entity` `Codehub::Repo` with id
     `grn:<org>/codehub/repo/<id>`, `org` `Iam::Org` with id
     `grn:root/iam/org/<org>`, and `parents` that one `Iam::Org`. It
     carries no `members`, no `consents` and no `operations`, which are
     the platform's.
   - **A delete** is `EntityDelete` of the same entity.
   - **The sequence is per entity** and rises with every change
     codehub makes to it, from a transactional outbox (ADR-0138). A
     delete keeps its sequence as a tombstone. The id is never reused,
     so a recreated repository is a new entity with its own sequence.
   - **What the port needs.** ADR 0030's at-most-once port cannot
     repair a lost change, since nothing remembers it. It is replaced
     for the external mode by an outbox written in the transaction of
     the repository's creation or deletion, and a relay that delivers
     in sequence order per entity and retries until garam answers
     applied or dropped. A replayed or reordered delivery changes
     nothing (`policy.proto`, `ApplyEntityChanges`), so at-least-once
     delivery is safe. The replacement is a new codehub ADR that
     supersedes that part of its ADR 0030 (answer 5). The in-process
     and no-op adapters keep their part in the other mode.
   - **codehub's create** tries delivery for a bounded time and answers
     created (201) either way; the outbox retries (answer 5).
   - **Organization changes are not reported.** `OrganisationCreated`
     and `OrganisationDeleted` are the platform's to write and cease
     to be codehub's.
   - **A repository garam does not yet hold** is a resource with no
     parent, outside every role's `resource in Iam::Org`, and is
     denied. The window is the delivery delay.
6. **A runner is a garam agent that codehub registers.** A runner is
   an `Iam::Agent` of the organization, `grn:<org>/iam/agent/<id>`,
   which garam already issues credentials to. It exchanges its agent
   token for a JWT with `aud` `codehub` and presents that to
   `/v1/runner/` (ADR-0141 items 3 and 8, codehub ADR 0034).
   - **Registration stays codehub's.** The runner registry holds the
     registrations and is kept in both modes (ADR 0034). The registered
     principal is the issuer and the `sub` of that token.
   - **A runner asks garam nothing.** Its calls are authorised by the
     registry, and a job's files reach it by presigned URL
     (`crates/codehub-runner/src/http_source.rs`), so a runner's agent
     needs no `codehub:` action.
   - **What an agent holds.** An agent is in `Iam::OrgMembers`
     (`policy/schema/iam.cedarschema`, `Agent in [OrgMembers, Org,
     Team, Project]`) and holds no role by default:
     `docs/architecture/authz.md` §Identity-derived statements says a
     machine identity holds no roles, and
     `platform/internal/org/postgres/postgres_membership.go` stores a
     role against a user's GRN. A role link for an agent is ADR-0146
     item 4's, which is not built, and whether its feed writes one for
     an agent is not verified here. A runner's agent therefore holds
     no `codehub:` action unless a link or an owner's `permit` gives
     it one.
   - **codehub does not refuse a registered runner's token on the rest
     of its API** (answer 6), and with the member default of item 1 it
     need not.
   - **Registering it is `codehub:runner.administer`** on the
     organization, asked of the administrator.

## Consequences

- **A change is needed in five places before any of this works.** The
  policy service needs the manifest, a `Caller` variant for codehub, a
  row in `policy/README.md`'s caller table, the declared context, the
  lifting of `policy_service.rs` lines 266 to 280, the removal of the
  audience check's exemption for `codehub` (ADR-0143 item 7), and the
  manifest test's hardcoded service list changing with the manifest.
  The platform needs codehub's service principal in each organization
  and the `unauthorized_client` answer (#1811). The HTTP binding of
  #1809 must accept a context key, the type `grn` and the type
  resolution of item 2, and must carry the `ApplyEntityChanges` form.
  codehub needs the adapter change of item 2, the id in the GRN it
  sends, the outbox of item 5 and the `unauthorized_client`
  reading of item 4. None is built.
- **A policy on a repository names a number.** It cannot be read
  without codehub, and one garam organization has one codehub
  deployment.
- **A guardrail on a path is two terms and a guard,** and it does not
  deny a question of the whole repository.
- **Policy never sees the path of a stored fact.** Nothing about a
  path is in an entity, so the access report
  (`policy/crates/access-report/`) lists the policies that name a
  repository and cannot list those that name a directory.
- **A freshly created repository can be denied** until its entity
  arrives, because codehub answers created whether or not delivery
  finished, and a repository deleted in codehub stays readable in garam
  until its delete does.
- **A member reads and does not write** until an owner's `permit` says
  so, which is one `permit` per repository or a role link.
- **A repository's GRN is not its name,** so a deleted and recreated
  name is two entities and no `permit` carries over.

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
- Owners write one `permit` per repository for every member in most
  organizations, which would show members' write is the default after
  all.
- The outbox's backlog is long enough that a created repository is
  denied for longer than a person waits.
- A garam organization is found to need two codehub deployments, which
  would show codehub's id is not unique enough to name a repository.

## Alternatives rejected

- **The path as a resource attribute.** The contract refuses a
  resource's `properties`, and an entity's attributes are what its
  owner writes, so a per-request path would need an entity per path.
  A path is not an entity (ADR-0142).
- **A path in the resource id,** `grn:<org>/codehub/repo/<id>#<path>`.
  The locator is no entity and the engine reads the id before `#`, so
  the path would still reach policy only as context, by a parse of the
  id that ADR-0142 refuses.
- **The repository's name in the GRN.** A name is reused after a
  deletion, so a `permit` for one repository would hold for the next
  of that name. The id costs readability, which item 2 states.
- **Keeping `create_organisation` as `codehub:org.create`.** Garam
  creates organizations, so the action would ask garam whether garam
  may do what it already did. `org.use` asks the one thing garam can
  refuse, whether codehub may serve the organization.
- **Reading `invalid_client` as a deny.** `invalid_client` is every
  failure to authenticate, including a wrong signature, which is a
  fault of the caller and not a refusal of an organization.
- **Subject types `Iam::User` and `Iam::Agent` sent by codehub.**
  codehub does not tell them apart and will not parse a GRN, so it
  would send a type it cannot know.
- **A member's `repo.write` by default.** Rejected in item 1.
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
