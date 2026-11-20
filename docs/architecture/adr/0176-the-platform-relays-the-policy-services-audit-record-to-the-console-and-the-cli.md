# ADR 0176: The platform relays the policy service's audit record to the console and the CLI

> Status: accepted; built in `platform` (`listAuditRecords`),
> `platform-web` (the Audit section) and `garam-cli` (#1991); builds
> [ADR-0148](./0148-the-policy-service-records-every-policy-write-and-every-denial-for-the-organization-it-concerns.md)
> Date: 2026-11-17

Drafted for #1991, which reserves this number, under #1796 and
completing #1785. The policy service's half, `ListAuditRecords`, is
built; this is the platform's, the console's and the CLI's.
[audit.md](../audit.md) §The policy service's record states the rule
beside the code it governs.

## Context

ADR-0148 decides that the policy service records every policy write
and every denial for the organization of the resource, and that an
organization's owners and administrators read the record on
`iam:audit.read` through a read on the service's contract that the
platform calls. The service holds the record and serves the read. No
person reaches it: the platform has no operation for it, the console
has no page for it and the CLI has no command for it. ADR-0169 has
the platform relay the policy writes as the signed-in user and store
nothing of them, and the read has the same shape.

## Decision

1. **One operation relays the read.** `listAuditRecords` is
   `GET /orgs/{org}/audit-records`, with `pageSize` and `pageToken`,
   and answers `records` and, where more follow, `nextPageToken`. It
   authenticates the session and calls the service's
   `ListAuditRecords` naming the signed-in user as the reader, as
   ADR-0169 names the writer. The platform stores no record, filters
   none and asks no question of its own.
2. **The service decides the read.** It asks `iam:audit.read` on the
   organization, current, and its refusal is the answer: 403 for a
   reader it refuses, 400 for a page token it never issued, 503 when
   it cannot be reached and 504 when the call times out. A member of
   the organization and another organization's owner are refused. The
   platform adds no check of its own, so an organization role that
   grants `iam:audit.read` in the service is the whole of who reads.
3. **A record is relayed as the service makes it.** It carries its
   id, its kind (`write`, `denial` or `denial-overflow`), the time it
   was first and last made, the count of identical denials it
   collapses, the calling service, the subject asking, the action, the
   resource, the outcome, the ids of the policies in the reasons, and,
   for a write, the version it came from and the version it made. A
   denial's `lastAt` moves as it collapses, so the order is the
   service's, by record id and newest first, and not by time. An
   overflow record names its day and nothing of who asked, what or
   where: it holds the denials the day's cap stored no row for. A
   write with no subject was made by the platform on a membership's
   behalf, and a removal names no version. No policy text, request
   context or token is in one.
4. **The console shows it as a section.** Audit, under Organization,
   lists a page of records with who asked, the action, the resource,
   the outcome, the policies, a write's step and a denial's count, and
   the time, and links the older records by the page's token. Its read
   is `iam:audit.read`, so the sidebar and the palette drop the
   section where the server denies it. A refused read is said so and
   never shown as empty, and a service that does not answer in time is
   said as unreachable. The table sorts nothing, because a sort would
   reorder one page and read as the order of all of them.
5. **The CLI lists it from the specification.** The operation is
   derived as `garam audit-records list`, with `--page-size`,
   `--page-token` and `--org`, and no command is written for it.

## Consequences

- The record ADR-0148 builds is read by the people it names, as
  themselves, and revoking an administrator's role ends their read at
  the service's next decision.
- The platform and the console keep no copy of a record and no
  retention of their own, so what the service removes is gone from both.
- A page is as the service ordered it. A client that wants time order
  sorts what it has read and does not know whether an older page holds
  a later `lastAt`.
- The platform's specification, the two SDKs and the CLI each gain an
  operation, generated.

## Signs the decision was wrong

- A reader the service refuses is shown a record, or one it permits is
  refused, because the platform decided the read itself.
- An organization's people ask for the records by time or by subject,
  which the page-by-page relay cannot serve and the service's read
  would have to filter.
- The service names a second read of the record, such as an export,
  that this one operation does not carry.
