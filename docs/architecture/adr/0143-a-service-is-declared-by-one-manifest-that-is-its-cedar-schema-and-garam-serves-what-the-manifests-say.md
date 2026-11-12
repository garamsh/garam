# ADR 0143: A service is declared by one manifest that is its Cedar schema, and garam serves what the manifests say

> Status: accepted; partly built. Items 1, 2, 3 and 5 are built
> (#1777); item 4, the platform's own action names, item 6, the
> registry, and item 7's proofs are decided and not built
> Date: 2026-11-12

Drafted for #1775, under #1774, which reserves this number and holds
the five decisions of the owner of 2026-11-12 this records. It
supersedes in part
[ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
(§3, the one schema file and its unqualified names),
[ADR-0140](./0140-owners-feed-the-policy-service-from-a-dirty-set-of-entities-read-at-delivery-and-a-caller-is-a-closed-list-of-services.md)
(§2, where the ownership of a type is written) and
[ADR-0141](./0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md)
(§5 and §13, how the list of services is held and that an ADR adds one).
It settles two points
[ADR-0142](./0142-a-grn-names-its-organization-its-service-and-a-path-of-type-and-id-pairs-with-a-locator-the-service-reads.md)
left open, the per-service manifest and how an action is named once
types belong to services, and supersedes nothing of it. garam is read
at `cb8f5366132bfe743fb859e12d957a929490d4ef`; a claim below that names
no revision holds for it.

## Context

- **A service is declared in four hand-written places.** The Cedar
  vocabulary is one file, `policy/schema/garam.cedarschema`, with no
  namespace: cowork's actions carry a `cowork:` prefix and the
  platform's a `<type>:` prefix, and nothing says which service
  declared a type. The GRN catalogue is a map in `lib/grn/grn.go`.
  The audience list is `Audiences` in
  `platform/internal/identity/issuer.go`. The policy service's table
  of which caller writes which types is `PLATFORM_TYPES` and
  `Caller::owns` in `policy/crates/server/src/caller.rs`. Only the
  last is held to the schema by a test (ADR-0140 §2).
- **The schema language already says most of a service.** It declares
  entity types, what each sits in, and the actions with the types they
  apply to. A second format for the same facts would state them twice.
  What it does not carry is the service's name, the GRN type an entity
  type is written as (`Team` is `group`) and the caller that writes
  it.
- **Adding a service costs an ADR and four edits.** ADR-0141 §5 and §13
  make a service's audience and caller entry an ADR and a pull
  request, and the next service after cowork, codehub, is waiting
  (#1760).
- **The owner asked that an agent be able to ask garam what a name
  is.** An agent that holds a GRN has no way to learn which service
  owns it, what it sits in or what may be done to it, and the answer
  is what the four places above would hold if they were one.
- **garam is not operating** (owner, 2026-11-12), so no stored policy
  or entity holds a name that moving one would strand.
- **Annotations are supported where this needs them.** Measured with a
  throwaway program on `cedar-policy` 4.14.0, the version
  `policy/Cargo.lock` pins, under rustc 1.99.1: the human-readable
  schema syntax parsed `@service("iam")` on a namespace, `@grn("org")`
  and `@caller("platform")` on an entity type, and `@doc("read an
  org")` on an action, and the schema's JSON form kept each under that
  namespace, entity type or action. A second fragment holding
  namespace `Cowork`, which names `Iam::Org` as a parent, `Iam::User`
  as a principal and `Iam::Action::"role-member"` as an action group,
  merged with the first through `Schema::from_schema_fragments`. The
  program is not committed.

## Decision

**A service's manifest is its Cedar schema file, one per service,
declaring one namespace. The GRN catalogue, the issuer's audiences and
the policy service's table of caller ownership are generated from the
manifests or checked against them, so a type or an action is declared
in one place. An action takes its service's name, and the platform
serves what the manifests say as a read-only registry.**

1. **A service's manifest is its Cedar schema file,**
   `policy/schema/<service>.cedarschema`, declaring one namespace:
   `Iam` for the platform's types and `Cowork` for cowork's. The
   schema already declares types, their parents and the actions with
   what they apply to; a second format would restate it. What the
   schema language does not carry is added as annotations:
   - **`@service("<name>")` on the namespace** is the service's one
     name (ADR-0142 item 2), the `service` segment of its GRNs and the
     first half of its actions' names. The namespace is that name in
     the form Cedar spells an identifier; the annotation, not a
     derivation, is what says which service a namespace is.
   - **`@grn("<type>")` on an entity type** is the `type` segment the
     entity is written as in a GRN. A type with none is not named by a
     GRN: `OrgMembers` is an entity the engine reads and no name
     refers to.
   - **`@caller("<sub>")` on an entity type** is the caller that
     writes it, a token's `sub` (ADR-0140 §1), or `none` for a type no
     caller writes, as `Policy` is the policy service's own. Every
     entity type carries one, and `none` is no caller's name.
   - **The one file is split.** `policy/schema/garam.cedarschema`
     becomes `iam.cedarschema`, and cowork's types and actions move to
     `cowork.cedarschema`. The service loads every manifest as one
     schema, so a policy is validated against all of them.
2. **What reads a manifest.** One reader parses the manifests, the
   policy workspace through the `cedar-policy` crate that ADR-0130 §3
   names the reference implementation, and no second parser of the
   schema is added. Each consumer below is generated or checked, as
   follows. Where the generated file is written and which command
   writes it are the implementation issue's.
   - **The GRN catalogue is generated.** `lib/grn` holds a map of a
     service to its types and nothing else, and it must compile into
     Go with no Cedar at run time, so a check alone would leave two
     copies to keep equal. The generated file is committed, and a
     check CI runs regenerates it and fails when that changes
     anything. The same file holds each type's parent types and each
     action with the types it applies to, which the registry
     (item 6) answers from.
   - **The audience list is checked.** It holds a service that has no
     manifest: `policy` is the decision point and not a service that
     declares types, and `codehub` is an audience before it has a
     manifest (#1760). A generated list could not hold them. The
     check fails when an audience names no manifest's service and is
     not one of those two, which the check names and which shrinks
     when `codehub` declares itself. Whether a manifest also says
     that its service verifies tokens is left open.
   - **The caller table is read, not copied.** The types a caller may
     write are the `@caller` annotations of the schema the service
     already loads, so no table exists to generate or to keep equal.
     What stays in code is the closed list of `sub` values the service
     accepts (`Caller`, ADR-0140 §1), since a caller is added only
     with a proof of its identity. A check fails on an `@caller` value
     that is neither `none` nor in that list, and on a `Caller` no
     manifest names. ADR-0140 §2's test, that every type is owned or
     stated as no caller's, becomes the rule that each entity type
     carries an annotation.
   - **What every check holds** includes that each manifest declares
     one namespace and one `@service`, that no two services share a
     name, that `@grn` is unique within a service and follows
     ADR-0142 item 4's character rule, and that every action's
     identifier follows item 3.
3. **Names on the wire.** An entity type is its qualified name,
   `Iam::User` and `Cowork::Page`, and a GRN is the identifier of an
   entity of the type its service and `@grn` name:
   `grn:acme/iam/group/eng` is `Iam::Team::"grn:acme/iam/group/eng"`
   (ADR-0142's settled identifier, the whole name before `#`). An
   action is `<service>:<action>`, the action's id inside its
   namespace being `<type>-<verb>`: `iam:org-delete` is
   `Iam::Action::"org-delete"`, and cowork's `cowork:page-read` keeps
   its spelling as `Cowork::Action::"page-read"`. The group of a
   type's actions is `<type>-*`, so `iam:org-*` is
   `Iam::Action::"org-*"`. `<type>` is the noun the action is done
   to, usually an entity type and not required to be one: the
   platform's `message:send` has none.
4. **Every action moves,** the platform's own included. `org:delete`
   becomes `iam:org-delete` wherever it is written, the platform's
   present evaluator, the role definitions, the console and every
   document among them, so one name is in use and no mapping between
   two is kept. No stored policy is converted: garam is not
   operating, which is the reason no compatibility form is owed.
5. **A role is the `Iam` namespace's,** `Iam::Action::"role-owner"`,
   `"role-admin"` and `"role-member"`, and a service's action joins a
   role by naming that group as its parent in its own manifest:
   `action "page-read" in [Iam::Action::"role-member"]`. A role
   template names the group in its qualified form, so the system tier
   changes with item 4.
6. **The registry.** The platform's browser API serves, read-only,
   the services, their types and their actions, and for a GRN its
   service, its type, its parent and the actions that apply to it.
   The parent is the name with its last pair removed (ADR-0142
   item 3), and the actions that apply are those whose `appliesTo`
   names the type. The CLI carries the same as a command. Both answer
   from the generated file of item 2 and read no entity: the registry
   says what a name is and what may be done to a thing of its type,
   never whether the thing exists or whether a caller may do it. A
   GRN of no declared service or type is answered as unknown.
7. **Adding a service is one pull request adding one manifest.** No
   ADR is owed for a service, which supersedes ADR-0141 §13's "added
   by an ADR". The pull request still holds what proves the service,
   since no row exists before a caller proves it (ADR-0141 §13):
   - the regenerated catalogue file;
   - an audience entry, where the service verifies tokens;
   - a `Caller` variant, where the service calls the policy service,
     with the service principal and key ADR-0129 gives it, which the
     platform records and the deployment holds;
   - the service's own feeding of its entities and its own calls,
     where it makes them.
   A service that declares a manifest and does none of the rest is
   known to the registry and accepted by no verifier.
8. **Left for later, and open.**
   - **A service an organization defines.** The manifests are the
     repository's, so an organization cannot add a type.
   - **A served registry of locator shapes.** ADR-0142 item 5 leaves a
     locator to its service, and the registry does not say what one
     looks like.

This leaves open, for the implementation: the route's shape and who
may read it, whether `none` stays a value or an absent annotation
becomes the statement, whether the caller name `platform` follows the
service name `iam` (ADR-0142 item 2), and the order the platform's
evaluator, the policy service and the console move onto the new action
names.

## Consequences

- **A type or an action is declared once.** A reviewer reads one
  schema file for a service and a check holds the three copies to it.
- **Every name a policy writes is qualified.** `Action::"role-member"`
  and `principal is Agent` in `policy/system-policies/` become
  `Iam::Action::"role-member"` and `principal is Iam::Agent`, and
  `ApplyEntityChanges` carries a qualified type. ADR-0140 §3's bound
  of 64 bytes on a type now counts the qualified name.
- **cowork's declaration lives in `policy/schema/`.** The policy
  workspace is below every service (ADR-0130 §10), so the one reader
  is there and a service's manifest sits beside the others, and
  cowork's change to its own types is a change under `policy/`.
- **Three copies stop resting on review.** The catalogue is generated
  and committed, so a regeneration that changes it is a failure rather
  than a review comment; the audience list and the `Caller` list are
  held by checks; the caller table is no copy.
- **A role names its actions in one form.** `Role.Actions` stops
  naming `org:*` and names `iam:org-*`, so the platform's role
  definitions and the schema's group carry one spelling.
- **The registry is derived from the same file as the catalogue,** so
  it cannot say a type exists that a GRN refuses.
- **The decision rests on a Cedar release.** Annotation support was
  measured at 4.14.0 only. A release that changes how an annotation
  reads is an upgrade owing this ADR's checks.
- **Nothing is built.** `garam.cedarschema` is one file, the catalogue
  and the audience list are written by hand, and every action keeps
  its old name, until the implementation issues under #1774 land.

## Signs the decision was wrong

- A service's name cannot be spelled as a Cedar namespace, as a name
  holding `-` cannot, and the annotation stops being a spelling of the
  namespace and becomes a second name to keep equal.
- An action wants a name `<type>-<verb>` cannot give, as `message:send`
  has no entity type, and the exceptions outnumber the rule.
- The audience check's two named exceptions grow beyond services
  waiting for a manifest, which would show the audience list is not
  derived from services.
- Two callers write one entity type, which would show `@caller` is one
  value where ownership is several.
- A Cedar release drops or renames an annotation a manifest rests on.
- A service is added with a manifest alone, and a verifier or the
  policy service then refuses it, which would show item 7's list is
  longer than the pull request carries.

## Alternatives rejected

- **A YAML manifest that generates the schema.** The schema language
  already states types, parents and actions with what they apply to,
  so the YAML would restate it and add a generator whose output Cedar
  reads. A reviewer would read the YAML and the generated schema
  diff, and the facts the schema cannot carry fit in annotations.
- **One schema file with prefixes,** which is today's. A prefix is a
  convention and not a namespace: nothing declares which service
  owns a type, a second service edits the first's file, and a name
  collision is found by reading.
- **Keeping the hand-written lists.** The three lists beside the
  schema are kept equal by reviewers, one of them by a test, and each
  service added edits all four places under an ADR. The registry would
  be a fifth.
