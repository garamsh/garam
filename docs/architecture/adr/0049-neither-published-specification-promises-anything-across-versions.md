# ADR 0049: Neither published specification promises anything across versions

> Status: accepted — §Decision's first heading is superseded, for the
> interservice contracts it names and from the first release whose
> window is tested, by
> [ADR-0079](./0079-interservice-contracts-keep-adjacent-releases-compatible-and-replay-by-stable-identifier.md)
> Date: 2026-08-31

Every measurement below was taken at `86c30e8`. The commands are in
`#540` and in the pull request carrying this ADR; re-run them there
rather than reading a number here as current.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives
- Errata

## Context

Both `api/openapi.yaml` and `api/machine.yaml` carry `version: 0.1.0`.
`git tag` returns nothing, `gh api repos/garamsh/garam/releases`
returns an empty list, and no ADR names a deprecation path, a support
window or what counts as a breaking change. So the field is a number in
a published contract with no decision behind it.

**The field is inert, and that was measured rather than assumed.**
Setting both files to `version: 9.9.9` and running `make gen` and
`pnpm --dir web gen:api`: both exit 0, no generated Go file moves at
all, and `web/src/types/api.ts` regenerated at `9.9.9` is byte-identical
to the same file regenerated at `0.1.0`. `9.9.9` reaches none of
`api/server.gen.go`, `api/machine/server.gen.go` or
`web/src/types/api.ts`. A bare absence proves nothing about the
instrument, so the probes were run on the same files first:
`HandlerWithOptions` appears 5 times in each generated server and
`export interface paths` once in the TypeScript. Deleting the
`version:` line outright, both `oapi-codegen` invocations and
`openapi-typescript` still exit 0.

Nothing reads the field, nothing enforces it, and the two numbers
cannot disagree in a way anything would notice. There is no versioning
mechanism here to repair.

**What is left is the promise, and it is not harmless.**
`api/machine.yaml` is published:
[ADR-0019](./0019-machine-openapi-spec.md) §Consequences makes the
machine listener's contract "publishable to whoever writes an operator,
which is what [ADR-0012](./0012-agent-operator-model.md)'s
bring-your-own operator needs", and that operator runs in a customer's
own cluster on their upgrade schedule. garam never dials it (ADR-0012)
and cannot make it upgrade. A version number in a published contract
reads as a promise whether or not one was made, and a reader taking
`0.1.0` to mean the surface has not changed since is wrong every time
it has.

[api-surface.md](../api-surface.md) §Not decided has carried the
interim — "Treat every surface as unversioned, and do not read that
field as a promise." That is the honest state and it is in the file the
audience does not hold. A consumer outside this repository reads
`api/machine.yaml` and nothing under `docs/`, so an interim recorded
only there reaches everyone except the reader it was written for.

[ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
publishes both images on a `vX.Y.Z` git tag. garam therefore has a
versioning mechanism for what it ships and none for what it promises,
and the two are easy to read as one thing.

## Decision

### Neither specification promises anything across versions, and each says so in its own text

The `info.description` of `api/openapi.yaml` and of `api/machine.yaml`
states that `version` is required by OpenAPI, promises nothing, and
does not move when the surface changes — so a caller must not read
stability, a deprecation notice or a breaking change out of it, and the
image tag ADR-0039 publishes under names a build rather than the
contract.

The statement goes in the specification rather than only in a
responsibility document because the specification is the artifact the
audience holds. This is the case `documentation.md` §Keep it current
describes from the other side: a claim about a reader who cannot reach
this repository has to travel in what that reader does reach.

### Both files take the same answer

The promise is what garam owes, and garam owes the same nothing on
both. The two files differ in exposure — `api/openapi.yaml`'s only
generated consumer is `web/src/types/api.ts` in this repository, under
`make web-check-gen`, while `api/machine.yaml`'s is a third party — and
that difference argues for the same answer rather than against it. A
weaker disclaimer on the more exposed file would be backwards, and a
stronger one would say garam owes a browser client more than it owes an
operator, which no decision here supports.

The two statements are therefore the same sentence in both files, and
neither carries a mechanism the other lacks.

### The field stays, and `0.1.0` stays with it

OpenAPI 3.1 requires it. The published schema at
`https://spec.openapis.org/oas/3.1/schema/2022-10-07` — whose own `$id`
is that URL — has `info.required: ["title", "version"]`, and both files
declare `openapi: 3.1.0`. That garam's generators accept a document
without the field, measured above, does not make such a document valid;
it means the generators do not check.

`0.1.0` stays because any other value would be a claim, and there is
nothing to claim. A number that moves without a rule saying when is
worse than one that never moves, because it invites the reading this
decision refuses.

## What this does not decide

- **When garam starts promising something.** This records that it
  promises nothing now. The decision to promise something is a
  different one, and it needs a rule about when the number moves that
  no evidence gathered here selects.
- **Whether the two surfaces would be versioned together or apart, if
  versioned.** They have two audiences and two exposures, so it is a
  real question and answering it early would be answering it from a
  sample of nothing.
- **ADR-0039's image tag.** It names a build and keeps naming one. What
  this decision adds is that it does not also name the contract.
- **Any compatibility marker in `api/*.yaml`.** No `deprecated:`, no
  sunset header, no support window. `#540` §Not scoped here bars
  settling that in a diff, and it is still unsettled.
- **What the `Version` field inside a policy document means.** That is
  the authz model's (`api/openapi.yaml` `PolicyDocument`,
  [ADR-0002](./0002-iam-style-policy-model.md)) and shares only a word
  with this.

## Consequences

- **A consumer reading either specification learns the field is
  decoration from the specification.** That is the whole of what
  changes for the audience, and it is the half that was missing.
- **`api-surface.md` §Not decided loses an entry and does not gain a
  replacement.** The state it recorded is now decided, and the fact now
  lives where a reader meets it; §What a specification is authoritative
  for records that the specifications carry it, rather than repeating
  it, under that document's own rule that a fact one of them already
  answers is named there rather than copied.
- **A pull request that changes either surface owes nothing to the
  version field.** That was already true and was not written down;
  saying it removes a question every such change could otherwise stop
  to ask.
- **Deciding to promise something later is a change to a published
  claim, not the filling of a blank.** Whoever supersedes this removes
  a sentence a consumer may have built against, which is a larger act
  than editing an undecided field and should be.
- **Nothing detects a version that moves without a decision.** No gate
  reads the field — that is what §Context measures — so the sentence in
  each file is guarded by review alone, which is the class
  `api-surface.md` §What nothing checks already names for the prose in
  these two files.

## Rejected alternatives

- **Leave the interim in `api-surface.md` and close the issue.** It is
  accurate and it is unreachable by the reader it addresses. The
  operator author holds `api/machine.yaml` and nothing else, so the
  disclaimer would reach every reader who did not need it.
- **Delete the `version` field.** Both generators accept its absence,
  which is what makes this tempting, and the OpenAPI 3.1 schema
  requires it. Removing it would trade an unexplained number for an
  invalid document, and a third-party toolchain that validates would
  refuse the file garam publishes.
- **Define a versioning scheme now — semver, and the number moves on a
  breaking change.** It is the shape the field's presence suggests and
  it needs a rule for *when*, which nothing here supplies. It would
  also be a promise made on the first occasion anyone asked, against
  `AGENTS.md` §What earns a rule, and a promise is harder to withdraw
  than to postpone. The one incident on record — the
  `renewIdentityCertificate` skew `#540` names — was resolved by
  `git merge-base` and a probe, which says a version would have helped
  and not what the version should have promised.
- **Move the number to `0.0.0` or `0.0.0-dev` to signal instability by
  convention.** It replaces a sentence a reader parses with a
  convention a reader has to already know, and it is still a claim
  nobody stands behind. The sentence says the thing; the number cannot.
- **Version the machine surface and leave the browser one
  unversioned.** It follows the exposure difference honestly and it
  splits one promise into two documents to keep true, for a surface
  garam has not yet decided to promise anything about. If garam ever
  versions one and not the other, that is the later decision's to make
  with a case in front of it.
- **State it once in `api/machine.yaml` only, since that is the
  published one.** `api/openapi.yaml` carries the same undecided
  number, and a reader comparing the two files would read the silence
  in one as the opposite of the sentence in the other.

## Errata

- 2026-09-02 — §Context's "`git tag` returns nothing, `gh api
  repos/garamsh/garam/releases` returns an empty list" was measured at
  `86c30e8`, and `v0.1.0` falsified both halves on 2026-09-01. Re-run
  on 2026-09-02, `git tag` returns `v0.1.0` and `gh api
  repos/garamsh/garam/releases` returns one release, also `v0.1.0`. The
  decision is untouched, and what the release moves is the reader
  rather than the promise: both specifications carry `version: 0.1.0`,
  and the images that tag published carry `0.1.0`
  ([ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)),
  so the reading §Decision was written to forestall — "the image tag
  ADR-0039 publishes under names a build rather than the contract" — is
  one a reader can now actually make from two numbers that match.
  Neither specification is what `v0.1.0` released, so the sentence each
  carries, "garam publishes no release of this contract and no
  compatibility window", stands as written.
