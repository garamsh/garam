# ADR 0050: A specification states its listener's 5xx once and declares none per operation

> Status: accepted
> Date: 2026-08-31

Every measurement below was taken at `86c30e8`. The commands are in the
pull request carrying this ADR; re-run them there rather than reading a
number here as current. `#685` carries an earlier set taken at
`5c01aed`, and two of its numbers have moved since — see §Context.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

**Neither specification declares a single 5xx.** Across 62 operations —
51 in `api/openapi.yaml`, 11 in `api/machine.yaml` — the two files
declare 266 responses and none of them is a 5xx. Neither carries a
`default:` response either: the two `default:` matches in
`api/openapi.yaml` are `default: 0` on the `limit` and `offset`
parameters. A zero read from an instrument that returns nothing proves
nothing, so the same anchored pattern was run for the classes that are
there: 203 responses are 4xx and 61 are 2xx, 2 are 3xx, and 1xx returns
zero as it should.

`#685`'s body counted 260. The difference is not a disagreement about
the file: `#698` removed `registerPassword`'s `'422'` and added three
4xx, and `#736` and `#778` moved others. The per-status figures that
issue lists have moved with them — 400 is 27 rather than 22, 404 is 45
rather than 44, 429 is 2 rather than 1, and no `'422'` remains. **The
claim the issue rests on is the one that has not moved: zero 5xx, in
either file, on every operation.**

**A 5xx is not hypothetical, and the two that occur are test-proven.**
`RecoverPanics` is in the middleware slice of each surface's generated
router (`internal/cli/serve.go`, `internal/cli/machine.go`), so every
operation on both listeners answers `500` when its handler panics, and
`internal/api/error_test.go` asserts it. `503` is reached by any
operation whose request needs a dependency garam reaches — the policy
engine, the database, or, on the browser listener only, a sign-in
provider — and `internal/api/error_test.go` asserts that `getOrg`
answers it when the engine's channel is in `TRANSIENT_FAILURE` while
`api/openapi.yaml` declares 200/401/403/404 for that operation.

**`501` and `504` are unreachable, and that is what is recorded for
them.** Nothing in the module produces `KindUnimplemented` or
`KindDeadlineExceeded`: every non-test occurrence of either is a `case`
arm in a kind-to-code translation, the arm in `internal/wire`'s own
map, or the constant's declaration — sinks, not sources. The other
route in is a gRPC code through `wire.KindOf`, and
`codes.Unimplemented` needs an RPC the server does not implement, while
every method both protos declare is implemented — 2 of 2 on
`AuthzService`, 10 of 10 on `PolicyService`. Nothing writes either
status literally: `StatusGatewayTimeout` and `StatusNotImplemented`
appear nowhere outside `internal/wire`'s map, so `writeError` is the
only path to them and it is reached by no kind that produces one.

One fact `#685` rested this on has since been falsified, and the
conclusion survives it. That comment recorded "no `ReadTimeout`,
`WriteTimeout`, `ReadHeaderTimeout` or `IdleTimeout` on any of the four
servers". `#745` has since added `requestReadTimeout` to two of them
(`internal/cli/serve.go`, `internal/cli/machine.go`). It does not
produce a `504`: its own recorded reason is that it "bounds how long a
request has to arrive, not how long a handler may run", so it sets no
deadline on a request context, produces no `errorx` error, and reaches
`StatusOf` by no path. `504` stays unreachable for the reason above —
that nothing produces the kind — rather than for the absence that has
gone.

**A green test looks exactly like reachability evidence here, and is
not.** `internal/machineapi/error_test.go` asserts that `postMessage`
answers `501` and `504`, in a table over every kind, by setting the
error on a fake engine. What it proves is that `StatusOf` renders those
kinds correctly; nothing in it says a real engine hands them back.
`#685`'s investigation found this and the PM filed it separately.

**Nothing checks a declared response set, in either direction, and that
was measured here.** Adding one `'500'` to one operation in
`api/openapi.yaml` and running `make gen` leaves `api/server.gen.go`
byte-identical — so `make check-gen`, which
[api-surface.md](../api-surface.md) §What a specification is
authoritative for calls the reason the authority "is enforced rather
than asserted", cannot see a response at all. The same edit does move
`web/src/types/api.ts`, which gains
`500: components["responses"]["Error"];` in that operation's response
union.

That pair of facts is the whole of the decision below. **A declared
response reaches a generated client and no check.**

## Decision

### A 5xx belongs to the listener, so each specification states it once and declares none per operation

The `info.description` of `api/openapi.yaml` and of `api/machine.yaml`
states that an operation's `responses` list is what it answers when
garam is working and is not the complete set of statuses it can return;
that every operation can answer `500` and one reaching a dependency
garam reaches can answer `503`; that both carry the `Error` schema with
`message` set to `internal error`, so a 5xx names no cause; and that
`501` and `504` are in garam's mapping and nothing produces them.

No operation gains a `'500'`, a `'503'` or a `default:`.

This follows the shape `#671` moved the code to. Recovery is held by
the server rather than by the route, and its stated reason is that a
router with covered and uncovered paths is a trap because whoever adds
the next route cannot tell which side they land on. OpenAPI has no
server-level response, so encoding a server property 62 times has
exactly that shape: operation 63 is added without `'500'`, and the
measurement above is that nothing catches it.

### `501` and `504` are recorded as unreachable rather than declared

Declaring a status nothing produces is the defect `#698` corrected on
four operations, arrived at from the opposite direction. The
specification says they are in the mapping and unreachable, which is
what a reader meeting the mapping needs and is not a response any
generator will hand a client.

### Both files take the same answer

The reason a 5xx is not per-operation is that it is a property of the
listener, and both files describe a listener. Their audiences differ —
`api/openapi.yaml`'s only generated consumer is `web/src/types/api.ts`
in this repository, under `make web-check-gen`, and `api/machine.yaml`
is published to whoever writes an operator (ADR-0019 §Consequences) —
and the difference argues for the same answer here too. Under the
rejected shape a third party's generated client would gain `500` and
`503` in every operation's response union; under this one it gains
nothing, and the sentence it reads is the same sentence the browser
client's author reads.

The two statements differ in one clause and only where the listeners
do: `api/openapi.yaml` names a sign-in provider among the dependencies
a `503` can come from, because `garam serve machine` reaches none.

### What each specification is authoritative for is narrower than it read

A specification is the source of truth for the shape of what its
listener serves (ADR-0004, ADR-0019). It is not, and was never, an
enumeration of every status an operation can answer. This decision
writes that boundary into the two files and into
[api-surface.md](../api-surface.md); it does not move it.

## What this does not decide

- **Which operations can answer `503`.** The statement is true without
  the enumeration, which is the point of holding it at the listener.
  Settling it per operation is a 62-operation reachability pass, and it
  is a precondition of the rejected shape rather than a follow-up to
  this one.
- **The per-operation 4xx sets.** `#698` corrected four and the rest
  are their own issue. Nothing here declares, removes or audits a 4xx.
- **The refusals that answer `text/plain` where the specification
  declares the JSON `Error` body.** The outermost `recoverPanics`
  (`internal/cli/observability.go`) is one, and its own recorded reason
  is that it runs where no specification describes the path — the
  probes, `/metrics`, an unmatched path. Whether that holds for every
  path it covers is `#685`'s named out-of-scope item and stays open.
- **Any handler's behaviour or any kind-to-status mapping.**
  `internal/wire/error.go` is untouched. This is about what the
  specifications say.
- **What closes the gap that nothing checks a response set.** The
  measurement above is that `check-gen` cannot see one. Making it see
  one is a change to a gate, and this decision reduces what such a gate
  would have to police rather than building it.

## Consequences

- **A new operation is covered by having been added.** It inherits the
  listener-wide statement, so there is no per-operation line for
  operation 63 to be missing.
- **A generated client gains nothing from this, in either
  repository.** `web/src/types/api.ts` does not move — `info.description`
  reaches no generated output — and a third party's operator client is
  unchanged. What changes is what a person reading either file is told.
- **The statement is prose no generator reads.** That is the class
  `api-surface.md` §What nothing checks names as this repository's
  recurring defect in these two files, already corrected twice in
  `api/machine.yaml` alone. This decision adds to what that section
  governs, deliberately, and the alternative added something to a
  contract instead.
- **A reader can no longer take a `responses` list as complete.** Every
  operation's list is now read against a sentence saying it is not,
  which is a change to how the whole file is read and is the point.
- **A change that makes `501` or `504` reachable falsifies a sentence
  in both files.** Adding an unimplemented RPC, or a deadline on the
  request path, would do it. Nothing detects that, so it is a claim
  review has to hold — the same standing as every other sentence in
  these files.
- **`#685`'s acceptance criterion is met by the second branch for two
  of the four statuses and the first for neither.** No operation
  declares a 5xx; all four have their reason recorded where a reader of
  the specification meets it.

## Rejected alternatives

- **Declare `'500'` on all 62 operations and `'503'` on those that can
  answer it (`#685`'s shape A).** It is the only shape a generator
  sees, and it costs three things this project has already paid for
  once. It re-creates in the contract the per-route framing `#671` moved
  the code away from, and the measurement above is that nothing catches
  operation 63. It cannot be written truthfully without first settling
  which operations are in the "nearly" for `503`, because a declared
  response set asserts what is *not* in it — the defect `#698`
  corrected. And it adds `500` and `503` to every operation's response
  union in a contract published outside this repository, which is a
  change to what garam promises a third party rather than a recording
  of what it already does.
- **Declare `'500'` only, since it is universal, and leave `503`
  undeclared.** It halves the enumeration and keeps its worst property:
  62 sites of a server-level fact, none of them checked, and an
  operation that answers `503` while its list says the set is
  otherwise complete. Splitting the two statuses across two mechanisms
  also makes the file say that one 5xx is per-operation and the other
  is not, which is not true of either.
- **Add a `default:` response to every operation.** It is per-operation
  in exactly the way rejected above, and it is less precise than the
  sentence: `default:` says "any other status", which swallows the 4xx
  sets `#698` was opened to make exact. It would undo that work in
  order to describe two statuses.
- **State it only in `api-surface.md`.** That document is where
  §What a specification is authoritative for already governs, and it is
  guarded by nothing either. The reason it is not enough is the
  audience: the operator author holds `api/machine.yaml` and nothing
  under `docs/`. `#685` says the same from the other end — a reason a
  status is not declared "belongs in the file a reader meets it in".
- **Put the statement in `components/responses/Error`'s
  `description`.** That object is `$ref`'d by every declared 4xx and by
  no 5xx, so a 5xx statement there would sit on the one object the 5xx
  responses do not reach, and would be rendered against every 4xx as
  though it described them.
- **Declare `501` and `504` too, on the strength of
  `internal/machineapi/error_test.go`.** The test is correct for the
  mapping it tests and says nothing about reachability. Taking it as
  reachability evidence is the trap `#685` identified, and it would put
  two statuses garam cannot produce into a published contract.
- **Leave both files silent and close the issue as accurate-by-
  omission.** Silence in a `responses` list is not neutral: `#698`
  established that the set asserts what is not in it, so the files
  currently assert that no operation can fail internally, which is
  false on both listeners and test-proven false on two operations.
