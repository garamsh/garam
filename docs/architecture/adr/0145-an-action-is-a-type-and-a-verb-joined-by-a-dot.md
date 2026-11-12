# ADR 0145: An action is a type and a verb joined by a dot

> Status: accepted; built (#1783)
> Date: 2026-11-12

Drafted for #1783, under #1774. It supersedes in part
[ADR-0143](./0143-a-service-is-declared-by-one-manifest-that-is-its-cedar-schema-and-garam-serves-what-the-manifests-say.md)
(item 3, the separator of an action's id and the spelling of a type's
group) and
[ADR-0144](./0144-an-organization-may-write-permit-policies-over-resources-inside-itself.md)
(item 7, the name of the action that writes a `permit`; the rest of
that item stands). garam is read at
`fb1c1d4deb5ae835b30e8d2f238087f3cc967183`, and the two matches below
at `85d848ffd88e586e32488df905db59fc6a16882c`, the head of #1782; a
claim below that names no revision holds for the first.

## Context

- **ADR-0143 item 3 joined a type and a verb with the hyphen the
  types already use.** An action id is `<type>-<verb>` and a type's
  group is `<type>-*`, so `iam:org-delete` is
  `Iam::Action::"org-delete"` and `iam:org-*` its group. A slug,
  which a type is (ADR-0142 item 4), is lowercase letters, digits and
  `-`, so the hyphen is in the type, the verb and the separator alike.
- **Two names parse two ways.** `iam:agent-template-read` is the verb
  `read` of the type `agent-template`, or the verb `template-read` of
  the type `agent`; `iam:agent-execution-read` is the verb `read` of
  `agent-execution`, or `execution-read` of `agent`. The manifest
  check in `policy/crates/manifest/src/manifests.rs` accepted both
  readings, because it asked only that an id be a slug holding a
  hyphen.
- **A group matched what it did not hold.** Read off #1782's head, a
  pattern matches by prefix, so `iam:agent-*` matches
  `iam:agent-template-publish`, an action of another type, and the
  administrator's `iam:policy-*` in `platform/internal/org/role.go`
  matches `iam:policy-permit`, the action ADR-0144 item 7 put outside
  that group so that an administrator does not hold it. ADR-0144's
  guarantee rested on a name that a pattern happened not to reach in
  the platform's own evaluator, and was false in it.
- **A dot is in no slug.** A Cedar action id is a string, so a dot is
  legal in it, and the platform's evaluator treats it as a literal.

## Decision

1. **An action id is `<type>.<verb>`, and on the wire
   `<service>:<type>.<verb>`.** Both parts are slugs: lowercase
   letters, digits and `-`. A dot is in neither, so a name has one
   parse: `iam:agent-template.read` is the verb `read` of the type
   `agent-template`, and `iam:agent.execution-read` the verb
   `execution-read` of the type `agent`. `iam:org.delete` is
   `Iam::Action::"org.delete"` and `cowork:page.read` is
   `Cowork::Action::"page.read"`.
2. **A type's group is `<type>.*` and holds exactly that type's
   actions.** `iam:org.*` is `Iam::Action::"org.*"` and holds every
   action of type `org` in its service and no other. The manifest
   check fails where a `<type>.*` group lacks an action of its type or
   holds one of another. A pattern `iam:agent.*` is then no prefix of
   `iam:agent-template.read`, since the character after `agent` is a
   dot and not a hyphen.
3. **A role group is `role-<name>`, the one id with no dot.**
   `Iam::Action::"role-owner"` is unchanged, and the manifest check
   accepts a dotless id only in that form.
4. **Writing a `permit` is `iam:permit.write`, replacing
   `iam:policy-permit`.** Its type is `permit`, so `iam:policy.*` does
   not name it. The rest of ADR-0144 item 7 stands: `role-owner` holds
   it and no other role, and whatever serves the write asks for it.
5. **The platform's delegated operations follow, by an explicit list
   and not a pattern.** `iam:agent-template-read` becomes
   `iam:agent-template.read` and `iam:agent-execution-read` becomes
   `iam:agent.execution-read`; the other eight map likewise, and a new
   migration rewrites the names stored in delegation grants and
   authorities and restates the checks that list them. No stored
   policy document is converted: garam is not operating (ADR-0143
   Context).

## Consequences

- **A name has one meaning, to a person and to a program.** The
  registry ADR-0143 item 6 decides can answer "what type is this
  action of" by splitting at the dot.
- **`iam:policy.*` no longer reaches `iam:permit.write`.** ADR-0144's
  statement that an administrator writes no `permit` holds in the
  platform's evaluator as well as in the policy service's.
- **A group is no wider than its type, and the manifest check holds
  it.** A new verb of a type is in the group or the check fails; an
  action of a type that has no group is simply held singly.
- **`agent.activate` has no role group.** It is a delegated operation
  the platform writes (ADR-0078), declared in `Iam` so the guard tests
  hold the platform's names to the manifest, and no role holds it.
- **Every action moves a second time** in the evaluator, the roles,
  the console, the CLI, the SDKs, cowork and the documents, and the
  ten delegated operations move in storage. Moving once more is the
  price of a name that is right before a second service writes one.

## Signs the decision was wrong

- A type's name contains a dot, or a verb does, which would show the
  slug rule changed under it and the parse is ambiguous again.
- A pattern `<type>.*` matches an action of another type in the
  platform's evaluator or in the policy service's, which would show
  the two read the wildcard differently.
- An action is written with no type, as a bare verb, because no noun
  fits it, which would show `<type>.<verb>` is not the shape of every
  action.
- A third rename of the same names is proposed.

## Alternatives rejected

- **Keep the hyphen and document the ambiguity.** The two names above
  would stay readable two ways, and `iam:agent-*` and `iam:policy-*`
  would keep reaching actions of other types. Documenting it moves the
  ambiguity to every reader and every pattern, which is what a
  convention was meant to settle once.
- **A slash,** `iam:agent/template-read`. A `/` is the separator of a
  GRN's path (ADR-0142), and a `*` in a policy pattern matches `/`, so
  a pattern for an action would read like one for a resource. A dot is
  in no GRN segment and does not collide with it.
- **A name with no type,** `iam:read-agent-template` or a verb alone.
  The group of a type's actions would then have no name to be
  `<type>.*`, and ADR-0143 item 5's role groups would hold actions
  with nothing to say which kind of thing each is done to.
