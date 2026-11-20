# ADR 0173: The console writes role links, guardrails and permits through the relay

> Status: accepted; built in `platform-web` (#1980)
> Date: 2026-11-17

Drafted for #1980, which reserves this number, under #1796. It builds
the console's share of
[ADR-0169](./0169-the-platform-relays-an-organizations-role-links-guardrails-and-permits-to-the-policy-service.md):
the writes its routes accept. [web-app.md](../web-app.md) states the
rule beside the pages it governs.

## Context

ADR-0169's relay creates a policy, appends a version and activates it,
and the console already activates, moves the default and deletes. It
wrote nothing else: the list said a policy is created in the API or the
CLI. Three questions were left to this round.

- **What a role form may write.** The platform derives a user's
  organization role and a team's or project's own scoped link from a
  membership and refuses to store one, with a 400.
- **How Cedar is written.** The console held CodeMirror from the earlier
  JSON policy documents. Cedar is parsed by the policy service alone,
  so no editor here can check it.
- **Where a write is asked.** A policy that does not exist yet has no
  resource, and the platform answers `iam:permit.write` only on the
  organization; the four `iam:policy.*` writes it answers on the
  organization as well (#1796).

## Decision

1. **A role form writes only what the relay accepts.** At organization
   scope the principal is a team or an agent and the resource is the
   organization; a user is not offered, since its organization role is
   its membership's. At team-or-project scope the principal is a user
   and the resource a team or a project. The roles are owner, admin and
   member. A pair the relay still refuses is a 400, shown at the form.
2. **A role link takes no new version.** A changed link is a new link,
   so the form is a create form only and a link's page offers no new
   version.
3. **Cedar is written in a plain monospaced text field.** The editor
   checks nothing. Text the service refuses is shown at the field with
   the service's own message. The `@codemirror/*` and `@lezer/*`
   dependencies and the test shim they needed are removed.
4. **A new version is stored and not the default.** The editor stays
   where it is after a save and says so; moving the default stays the
   policy page's control.
5. **A create form is a route, not a dialog.** `permissions/new` takes
   the kind, name and body, and opens the policy it created as the
   draft it is. `permissions/{policy}/new-version` is the version
   editor, starting from the default version's text.
6. **Each write is offered unless the server denies it, asked on the
   organization.** A new policy is asked with `iam:policy.create` for a
   guardrail, `iam:permit.write` for a permit and `iam:policy.attach`
   for a link. Every write of an existing policy is asked on the
   organization too, never on the policy: `iam:policy.update` for a
   version, `iam:policy.detach` and `iam:policy.delete` for a removal,
   and `iam:permit.write` for a permit's writes, which the platform
   answers on the organization alone. Only an explicit deny withdraws a
   control, and the policy service enforces every write either way. A
   permit's removal stays `iam:policy.delete`, which is what the
   service takes it as.
   A role link is offered no new version: a changed link is a new link,
   and the console has no meaning for a link's version beyond the one
   it was created with, though the relay accepts one.
7. **A write's refusal is worded for the write.** A 400 shows the
   service's message at the body; 403, 404, 409 and 504 are the
   console's sentences for that write, and any other error is the SDK's.

## Consequences

- The console writes every kind of policy. Nothing in the console
  drafts, previews or explains what Cedar would decide.
- A person sees the service's parse error and nothing the editor knew
  first: no highlighting, no inline marks.
- The platform answers every write asked on the organization (#1979,
  #1981), so a caller denied one is not offered its control; where a
  check cannot be asked, every control is offered, and the service still
  refuses a caller who may not, with a 403 said at the form.
- One fewer client dependency family to keep current, and no browser
  shim in the component tests.

## Signs the decision was wrong

- People author Cedar in the console often enough that a refusal with
  only the service's message costs them more than an editor with
  highlighting and a local parse would.
- People ask for a changed role link to keep its identifier and
  history.
- A caller denied a write is routinely offered it because the check was
  unknown, and meets the 403 as often as the form.
