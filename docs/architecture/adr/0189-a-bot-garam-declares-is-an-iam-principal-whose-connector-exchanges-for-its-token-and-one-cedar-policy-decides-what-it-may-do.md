# ADR 0189: A bot garam declares is an IAM principal whose connector exchanges for its token, and one Cedar policy decides what it may do

> Status: accepted; superseded in part by
> [ADR-0192](./0192-every-system-that-connects-to-garam-is-an-app-installed-in-an-organization-and-each-running-copy-is-an-instance-with-its-own-key.md)
> (decision 2's actor)
> Date: 2026-11-25

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2027, for which the Garam PM reserved this number. The
owner's decision is
<https://github.com/garamsh/discussions/discussions/6#discussioncomment-19791501>.
The PM settled the GRN's form on 2026-11-25, correcting the
`platform/bot` form accepted before it. garam is read at
`dc030cd1c48b8324b207a65a2606448ecb9203ea`, and garam-bot at
`18f95d28443ccdb47b1202ce8cf5bd16dc0db2a0`, where garam-bot#1208
merged its ADRs 0036 to 0040 and `docs/architecture/bot-system.md`. A
claim below that names no revision holds for those.

## Context

- **garam-bot's design (garam-bot#1208).**
  - A bot is declared in garam, as a CRD in a cluster, or as a file at
    a PC or a naru-host place. garam owns only the first kind
    (garam-bot ADR 0036 decision 1).
  - garam-bot ADR 0037 decision 1 has a bot authenticate with a token
    of at most one hour, which its connector obtains by an RFC 8693
    exchange naming the bot, with the place as the actor. The token
    stays at the place.
  - `bot-system.md` §Authentication lists the exchange's claims for
    garam#2049 to build.
  - `bot-system.md` §Declare anywhere, manage in garam gives a bot
    declared at its place no garam token, and leaves its identity open
    with garam.
- **What garam has.**
  - A connected cluster is a machine identity whose token reaches the
    cluster-connection routes, and the mint of the tokens of a bot
    placed on it by a live declaration, and nothing else (ADR-0184
    decision 10, the interface's AUTH-4 and AUTH-5).
  - Declarations are placed under an identifier that names one
    declaration, once, across every record (ADR-0187 decision 4).
  - The policy service decides Cedar policies behind an AuthZEN-shaped
    contract (ADR-0130). A GRN names every resource (ADR-0142). garam
    is an OIDC issuer that exchanges a token for one bound to an
    audience (ADR-0141).
- **ADR-0179 renames `grn:<org>/iam/agent/<id>` to
  `grn:<org>/iam/bot/<id>`**, and `grn-model.md` §Ownership puts the
  platform's types under `iam`.
- **The owner's decision (comment 19791501)** puts a bot's identity,
  permissions, models and tools under garam's IAM:
  - one Cedar policy decides;
  - a bot's runtime obtains a short-lived token per bot, as an
    instance profile does;
  - tools are MCP servers in three auth modes;
  - there is no Connection resource;
  - no secret enters a workspace;
  - static keys are sealed to the cluster's public key.

## Decision

1. **A bot garam declares is an IAM principal,
   `grn:<org>/iam/bot/<declaration id>`.** The identifier is the
   declaration's, which already names one declaration, once, across
   every record.
   - Declared bots and the agent records ADR-0179 migrates to
     `iam/bot` share one identifier space. A declaration whose
     identifier names an existing agent record's id is refused 409, as
     an identifier taken. #2049 builds that refusal.
   - A bot declared at its place has no garam identity and no garam
     token. Its identity stays open, as garam-bot#1208 left it.
2. **A declared bot's token is an RFC 8693 exchange its place's
   connector makes, with the claims `bot-system.md` §Authentication
   names:**
   - `grant_type` is `urn:ietf:params:oauth:grant-type:token-exchange`,
     at the machine listener's token endpoint;
   - `actor_token` is the connector's own access token;
   - `subject_token` is of a garam-defined token type whose value is
     the bot's GRN, and #2049 names that type in
     `platform/api/machine.yaml`;
   - `audience` is garam's, and `scope` lists what the bot may call,
     with nothing implied;
   - the token answers `sub` the bot's GRN, `act.sub` the place's GRN,
     `aud` garam's audience, `scope` what was asked and allowed, and an
     expiry within an hour;
   - garam refuses a bot that no live declaration places on that place
     (ADR-0184 decision 10, AUTH-5). A tombstoned declaration stops the
     mint on the next request.

   The token is opaque, and garam stores only its hash. It is read on
   every request at garam, as a connector's token is (AUTH-2), and
   answers its claims to introspection (RFC 7662). A party outside
   garam that needs to verify offline is given a narrower JWT, bound
   to its audience, by a further exchange of the bot's token (ADR-0141
   decision 3). That JWT lives no longer than the bot's token.
3. **Tools, model providers and workspaces are GRN resources under
   `iam`:** `iam/tool`, `iam/model-provider` and `iam/workspace`. Each
   is declared in `policy/schema/iam.cedarschema` by the pull request
   that first builds it (ADR-0143).
4. **One Cedar policy decides what a bot may do.** A bot's grants are
   Cedar policies of its organization, decided by the policy service
   over GRNs (ADR-0130, ADR-0142). A declared bot's
   `spec.permissions` becomes the policies garam holds for it.
   - A request that reaches garam is decided there as an AuthZEN
     evaluation, a brokered tool token among them (garam-bot ADR 0038).
   - garam sends a declared bot's policies to its place, and the place
     decides the bot's calls there with a Cedar evaluator of its own,
     against the same schema.
   - A bot run without garam has its `spec.permissions` translated
     into the same Cedar policies and decided locally, so one bot is
     decided the same way with garam or without it.
5. **Tools are MCP servers behind one Tool interface, in three auth
   modes** (garam-bot ADR 0035, ADR 0038):
   - `garam`: an organization connects a service through garam's app
     for it, or its own (garam-bot ADR 0038 decision 5), and garam
     holds the tokens encrypted per organization. A declared bot's
     token is exchanged for a short-lived token for that service;
   - `secretRef`: a Secret at the place that the declaration names,
     whose value garam never sees;
   - `federated`: garam's issuer is the identity, and the target
     trusts a JWT exchanged for the bot (decision 2) without a secret.

   There is no Connection resource: an installation is held under its
   organization, and a grant in IAM says which bot may use it with
   which scope. No secret enters a workspace (interface 2's CRED-1).
6. **A static key reaches a place sealed to that place's public key**,
   as Sealed Secrets seals one, so garam holds only ciphertext and the
   place alone opens it. It waits for cluster-connection's version
   that carries it. `v1alpha1` defers it (the interface's §Open
   questions). An organization that must keep its keys uses its own
   app and its own secret store behind the same connect button.

## Consequences

- #2049 builds decision 2 and decision 1's refusal. The mint becomes
  an RFC 8693 exchange at the machine listener's token endpoint,
  rather than a route of its own.
- The agent records' migration to `iam/bot` (ADR-0179) checks
  declaration identifiers, and a declaration checks agent ids.
- The policy service's schema gains `iam/bot` as a principal, and
  `iam/tool`, `iam/model-provider` and `iam/workspace`, when each is
  built.
- A place runs a Cedar evaluator and receives policies from garam. How
  they travel is a version of the place's protocol, garam-bot's to
  write with garam.
- A bot declared at its place reaches tools by `secretRef` or
  `federated` alone, and none of garam's routes.

## Signs the decision was wrong

- A bot's token read on every request at garam loads the database
  more than its calls are worth. That would make a short JWT the bot's
  token, with revocation bounded by its life.
- Local evaluation at a place and the policy service disagree on a
  request often enough to be noticed, which would show the evaluators
  or the schema drifting apart.
- Declaration identifiers and migrated agent ids collide often enough
  that the shared space confuses an organization.

## Rejected alternatives

- **`grn:<org>/platform/bot/<id>`.** `platform` is the policy
  service's caller name (ADR-0140), not a GRN service, and the
  platform's types sit under `iam` (`grn-model.md` §Ownership,
  ADR-0179).
- **A mint route of its own.** garam-bot ADR 0037 names RFC 8693, and
  the token endpoint already serves the connector's exchange.
- **A JWT as the bot's token.** A tombstone would stop nothing until
  the JWT expired, while #2049 has it stop the mint on the next
  request and keep only a hash.
- **A Connection resource.** It would hold a credential and a scope
  together, where IAM already holds the grant and garam's app holds
  the installation (comment 19791501).
