# ADR 0115: A list drops each object its caller may not read, asked of the engine in one batch

> Status: proposed
> Date: 2026-10-19

Drafted for #1569, for which the Garam PM reserved this number on
2026-10-19. The PM decided the two questions it settles the same day:
a batch check added to the authz service, and a page filtered as read.
garam is read at `b14f5f5e2c55ed0bea5d4f2551e93b953acb99a2`; a claim
below that names no revision holds for it.

## Context

`listGroups`, `listProjects`, `listAgents`, `listAgentTemplates` and
`listPolicies` were each authorized once, on their read action on the
organization, and answered every row their store paged. Each object's
own read (`getGroup`, `getProject`, `getAgent`, `getAgentTemplate`,
`listPolicyVersions`) is authorized on that object's GRN. A policy
denying one group therefore refused that group's read and left it in
the list, `member=me` included (#1567).
[ADR-0113](./0113-the-console-is-roster-one-policy-shaped-ui-where-people-and-agents-are-members.md)
has the console show what the caller's policies let it read, which a
list answering a denied object contradicts.

Asking per object the way a read asks costs, for each object, a read
of the caller's membership and one of its groups, for a group or a
project two reads of that object, and one `Check` RPC. Resolving the
caller once, as `checkPermissions` does, removes the reads but still
costs one RPC per object, and a list's `limit` defaults to 0, the
whole list. The engine had no call that takes more
than one object.

## Decision

- **The engine answers many objects in one call.** `CheckBatch` in
  `proto/garam/authz/v1/authz.proto` takes one subject, one action, the
  subject's groups and organization, the statements that hold for every
  object, and per object its GRN and the statements that reach the
  subject through it alone. It answers one decision per object, in
  order. Each is decided as `Check` decides it, over the persisted
  statements read once for the call. One object outside the subject's
  organization refuses the whole call, as `Check` refuses it. `Check`,
  `CheckCurrent` and `Explain` are unchanged.
- **The api server's `Evaluator` seam gains `EvaluateBatch`**, the
  gRPC client's call of it.
- **Each of the five lists drops the objects whose own read the caller
  is denied.** The list is authorized on the organization as before,
  and the caller it resolved is asked about every object of the page in
  one `EvaluateBatch`. The statements sent are those the object's own
  read collects: the membership and direct roles for every object, and
  a group's or project's roles for the object they are scoped to, built
  from the rows the list already read and the caller's group and
  project memberships, each read once.
- **A page is filtered as read.** `limit` and `offset` count the stored
  rows, so a page can come back shorter than its limit with more after
  it. No row is skipped or answered twice across pages.
- **Lists gated on a parent object are not filtered**: a group's or
  project's members, a policy's versions and attachments, an operator's
  keys. Each is authorized on the object it belongs to, and the rows it
  answers carry no read of their own.

## Consequences

- A Deny on one object hides it from every list that answers it and
  from no other caller, and the list and the object's read agree.
- A list costs the engine one round trip, however long the page.
  `checkPermissions` and the Your access panel can ask the same seam.
- A short page no longer means the end. A client pages until a page
  comes back empty.
- An engine predating `CheckBatch` answers it `UNIMPLEMENTED`, and a
  list against it fails rather than answering unfiltered.
- One request carries every object of the page, so a page of an
  unbounded list is bounded by the gRPC message size rather than by
  garam.

## Signs the decision was wrong

- A client stops early on a short page and misses objects after it:
  the page wanted to be filled to its limit, read past the stored rows,
  at the price of an unbounded read.
- A list of an organization's whole set fails on the gRPC message size:
  the batch wanted a bound of its own, and the list chunked against it.
- A list and the object's own read disagree on an object: the
  statements the list sends were not the ones that read collects, and
  the list wanted to ask the read's own question rather than rebuild
  it from rows it held.
