# ADR 0065: garam states what deleting an organization destroys, and the browser listener reads the authority's presence to do it

> Status: accepted
> Date: 2026-09-04

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

`DELETE /orgs/{org}` issues one statement against `orgs`. Everything
else that goes, goes through a foreign key declared `ON DELETE
CASCADE`, so what a deletion takes is fixed by the migrations and
stated in no surface at all: not `api/openapi.yaml`, not the console,
and until this pull request not the responsibility documents either.

The cost is on record twice. #991 finding 3 reports a deletion that
took three agent definitions, four agents, one operator and the
organization's certificate authority, with nothing naming any of them
before or after. Three days before this ADR, an operator took a
seven-table dump believing it was a backup, deleted an organization,
and found the dump had not included `certificate_authorities`. The
same gap produced both: a person clicking and a script writing a
backup each have to know what a delete reaches, and nothing tells
either.

The certificate authority is the part that does not recover.
`migrations/000005` keys it on the organization and stores the private
key sealed in the same row; `internal/ca`'s `Store` declares `Create`
and `Get` and nothing else, so no route imports, restores or
re-creates one, and `Create` would need an organization row the same
delete removed. Every certificate that authority signed stops
verifying for good.

A count assembled by the caller does not answer this. The console's
own organization page totals `listGroups(...).data.length`, which is
one page of a paged list: right while nothing is filtered and nothing
has moved, and wrong exactly when a person is about to delete
something they are no longer looking at whole.

## Decision

**garam answers what deleting an organization would destroy, before
the act, on the server.** `getOrgDeletionPreview` returns a count per
resource kind — groups, projects, members, invitations, policies,
agents, agent definitions, operators, consents — read from the store
rather than from a page, and it deletes and reserves nothing.

**The certificate authority is a field of its own and not an entry in
the count map.** It is the only thing a deletion takes that cannot be
made again, and an enumeration is precisely where that is lost. The
field reports presence, because an organization that has needed no
certificate has no authority and telling its owner otherwise would be
false.

**The counted set is derived from the schema, not from a list somebody
wrote down.** What the operation reports is what carries a foreign key
to `orgs`, plus what carries one to those in turn, which the response
attributes to its parent rather than counting apart.

**A count is not filtered by what the caller may list, so a zero
means the organization holds none of that kind.** The deletion
consults no per-kind permission — the cascade takes every row whatever
the caller can read — so a preview narrowed to one caller's view would
under-report exactly what it exists to report. A zero that meant
`not authorized` and a zero that meant `nothing there` would be the
same number for different facts, and only one of them is the
consequence being stated. The permission is on the operation, once.

**A read that describes a write is decided on that write's action.**
The operation is authorized on `org:delete` and not `org:read`, so the
description of an act reaches exactly the caller who may perform it.
No role gained an action: the owner already holds `org:*`.

**The browser listener reads whether an organization has an authority,
through a seam the composition root satisfies.** `internal/api`
declares `AuthorityReader` and does not import `internal/ca`, in the
shape the agents and operators domains already reach that authority
through (ADR-0047). Presence is the whole of the seam, so `garam serve
api` still reads no key-encryption key and still opens nothing.

## What this does not decide

- **Whether the console gains a delete control.** It has none today
  and never has: `deleteOrg` appears in `web/src` only in the
  generated `web/src/types/api.ts`. Whether that button should exist
  is a scope decision this ADR does not take, and the answer needed by
  a console dialog, the CLI and a backup script is the same one, which
  is why it is in the API rather than in a dialog.
- **What deleting an organization does.** No cascade moves, no
  migration moves, no deletion semantics change. This states a
  consequence; it does not alter one.
- **Undo, soft delete, or export.** Each is a decision of its own.
  Telling the truth before an irreversible act is not a step toward
  making it reversible, and nothing here is one.
- **Whether the counts are a transaction.** They are separate reads,
  so an organization written to while they run answers from either
  side of that write. What the operation refuses is a page length, not
  concurrency, and the response says so.
- **Whether any other surface publishes this.** `api/machine.yaml`
  gains nothing: an operator does not delete an organization.

## Consequences

- **A caller can ask what a delete takes and get an answer that is
  right for a filtered or stale view**, which is the case a
  client-side total gets wrong.
- **The unrecoverable row is named as unrecoverable**, in the
  specification's prose and in the response's shape, rather than
  sitting among counts that are all re-creatable.
- **The browser listener now reaches a certificate authority, for one
  question.** [domains.md](../domains.md) said it reached none and is
  corrected in the same pull request. The seam is what keeps that
  reach to presence: widening it is a change to a declared interface
  rather than an edit inside a handler.
- **Two facts the cascade holds are now stated where a reader meets
  them.** Deleting one organization removes the consent relation the
  other organization is still a side of, without that organization
  acting (ADR-0035); and `invitations` goes, which #1013's own list
  did not name. Both are in
  [organizations.md](../organizations.md).
- **Four domains gained a count method each.** They are counts and not
  listings because a list that pages answers for one page, which is
  the defect this operation exists to remove.
- **Nothing detects a table added with a cascade to `orgs` and left
  out of this operation.** `make ci` reads no rule of the kind, and
  the response is assembled by a handler rather than derived from the
  catalog at runtime. The guard is review, which is the class
  [README.md](../README.md) §What nothing detects already describes.

## Rejected alternatives

- **Build the console's confirmation dialog.** #1013 asked for it, and
  it assumes a delete control the console does not have. Adding one
  would ship a new way to perform the irreversible act in the name of
  warning about it, and whether the console should delete an
  organization is the owner's decision rather than this issue's.
- **Count from the lists the caller already holds.** No API change,
  and it is the defect: a total over a filtered or paged list is wrong
  exactly when the person is about to act on what they can no longer
  see whole.
- **Report the certificate authority as one entry in the count map.**
  Uniform, and it loses the distinction that made this issue exist. A
  reader scanning nine numbers has no way to see that one of them is
  not like the others.
- **Say the authority will be destroyed without reading whether one
  exists.** No seam, no query. It states a falsehood for every
  organization that has needed no certificate, and this issue exists
  because text near an irreversible act was confidently wrong.
- **Give `internal/api` the `ca.Store` directly.** Fewer moving parts
  than a seam. It puts sealed key material in the process that has no
  key to open it and no business holding it, and it makes the listener
  import a domain that [domains.md](../domains.md) records it as not
  importing — a wider change than the question asked needs.
- **Authorize the preview on `org:read`.** It is a read, and the shape
  is the obvious one. It publishes the size of an organization to
  every member who cannot delete it, for no gain: the caller who needs
  this answer is the one holding the delete.
- **Add a `deletion-preview` operation to the machine listener too.**
  Symmetric, and nothing asks for it: an operator does not delete an
  organization, so the operation would have no caller
  (`simplicity.md` §Rules, no speculative abstraction).
