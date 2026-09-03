# ADR 0059: The two specifications' prose is not held to agree, and each defines every member its own caller meets

> Status: accepted
> Date: 2026-09-03

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

`api/openapi.yaml` and `api/machine.yaml` each carry two enums over one
stored value: `AgentProvisioningState` and `ProvisioningState` over
`internal/agent`'s `ProvisioningState`, `AgentRuntimeState` and
`RuntimeState` over its `RuntimeState`. Each pair names the same
members — four and three — and until #937 each pair carried the same
description, byte for byte.

#937 defined `provisioned`, `ready` and `failed` on `api/machine.yaml`,
and left `api/openapi.yaml`'s `AgentProvisioningState` naming `pending`
alone. The machine specification now defines four members and the
browser-facing one defines one, and the browser-facing one is what a
console developer holds: `web/src/types/api.ts` is generated from it,
and `web/src/app/console/orgs/[org]/agents/_components/agent-list.tsx`
maps all four provisioning members and all three runtime members to a
badge. Raised as Convention concern 3 on #937 and filed as #941.

Measured on 2026-09-03 by extracting the four descriptions the same
way and diffing each pair: the runtime pair is identical and the
provisioning pair differs by the ten lines #937 added. The control is
that pair — an extraction that reported sameness for both would be
reporting the method rather than the files.

Nothing decides whether the two must agree.
[ADR-0019](./0019-machine-openapi-spec.md) splits the two
specifications on which caller reaches the listener.
[ADR-0053](./0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)
puts both state reports on the machine listener and adds nothing to the
browser-facing one — "What was missing was a writer, not a reader".
Neither reaches the prose.

What #937 added is not one kind of sentence. Three of its four say what
a member means. The fourth says when a reporter may choose one:
"`failed` is reserved — report it only from an observation that
separates a workload that cannot start from one that is still
starting". The browser-facing listener admits no reporter, so that
sentence has no reader in `api/openapi.yaml`. Copying the description
across puts an instruction in front of a console developer who cannot
act on it; leaving `api/openapi.yaml` where it stands leaves that
developer rendering four badges from a file that names one.

Nothing compares the two files, and what generation does reach is
narrower than [api-surface.md](../api-surface.md) §What nothing checks
stated. Measured on 2026-09-03 by perturbing one description of each
kind in each specification and running `make gen` and `make web-gen`,
where each output is read against the specification it is generated
from:

| Description | `api/server.gen.go` | `api/machine/server.gen.go` | `web/src/types/api.ts` |
|---|---|---|---|
| A schema's | moves | moves | moves |
| An operation's | unchanged | unchanged | moves |
| A tag's | unchanged | unchanged | unchanged |

The two outcomes in that table are each other's control: an instrument
that reported "moves" for every kind would be reporting that `make gen`
rewrites the file. So `make check-gen` and `make web-check-gen` fail on
a schema description edited without regenerating, and pass on any text
at all — each compares one specification against its own output, never
one specification against the other, and never prose against what the
code does.

## Decision

**Two descriptions, two audiences.** #941 states two readings and this
takes the second.

- **Each specification's prose is written for the caller its listener
  admits, and the two are not held to agree.** ADR-0019 split the files
  on the caller; this extends that split from what each file describes
  to how it describes it. `api/machine.yaml`'s callers report an
  agent's state and `api/openapi.yaml`'s caller renders it, so a value
  both files carry is described in each for what that file's caller
  does with it. Divergence is the intended state and not a defect to
  reconcile, and the byte-identity that held until #937 was both files
  having said the minimum rather than a rule being kept.
- **Each file defines every member a caller of its own listener can
  meet.** This is what the clause above would otherwise leave open, and
  it is the whole of what counts as a defect here: a member a caller of
  that listener can be handed, undefined in the file describing that
  listener. `api/openapi.yaml` was in that state three times over on
  `AgentProvisioningState` and twice on `AgentRuntimeState`, and this
  pull request ends it.
- **The obligation is the file's and not the schema's.** Where a member
  is defined inside a file is that file's own business.
  `api/machine.yaml` defines `ProvisioningState`'s four members on the
  schema and `RuntimeState`'s on `reportRuntimeState`'s description —
  "The agent's own account of whether it is running" — and is complete
  either way, which is why nothing in it changes here. Recording that
  stops the asymmetry being read later as an oversight.
- **An instruction to a party belongs only in the file that party
  holds.** `ProvisioningState`'s rule for choosing `failed` stays on
  `api/machine.yaml`. `api/openapi.yaml` states the same fact as its
  consequence for a reader — a state other than `failed` is not
  evidence the pod can start — and names the machine specification as
  what decides when an operator may report each.
- **Where both files state one fact and neither caller needs it put
  differently, they state it in the same words.** `pending`,
  `provisioned` and `ready` are `api/machine.yaml`'s sentences
  unchanged in `api/openapi.yaml`. Two wordings of one fact are two
  claims to keep true, and nothing would catch the second going stale.
- **Each file says which caller it is written for, once, in its own
  `info.description`.** `api/machine.yaml` already did — "This is a
  different listener from the one `api/openapi.yaml` describes and
  shares nothing with it". `api/openapi.yaml` gains the counterpart, so
  a reader of either meets the split before meeting a description
  shaped by it.
- **Nothing checks this, and nothing is added.** No target compares one
  specification with the other, and none could tell a member left
  undefined from one whose name is its whole definition. It is held by
  review, under `docs/convention/review.md` §What each check verifies,
  whose Architecture row fails a diff where "a decision or a domain
  changed without the documents that describe it" — a rule about what a
  diff must carry alongside another change, which is what this is.
  Naming a rule reviewer-held is the decision rather than a deferral,
  as
  [ADR-0057](./0057-a-template-edit-moves-the-chart-version-and-a-reviewer-is-what-checks-it.md)
  §Decision says for the chart version, and recording it here is what
  stops the next measurement filing the absence as an unnoticed gap.

## What this does not decide

- **Whether either file owes its caller the report-level facts the
  other states.** `api/machine.yaml` tells a reporter, on both report
  routes, that garam keeps the last report and expires none, so a state
  stands until it is reported again. That is a fact about reports
  rather than about a member, so the obligation above does not reach
  it, and whether `api/openapi.yaml` owes its reader a form of it is
  open. It is not settled here by implication, and it wants an issue of
  its own.
- **The two names.** `AgentProvisioningState` and `ProvisioningState`
  are one stored value under two names, and nothing here moves either:
  the browser-facing file prefixes what it serves on `Agent` and the
  machine one does not.
- **Any duplicated prose beyond these two pairs.** The enum pairs are
  what #941 named and what was measured. A schema both files carry
  under different names elsewhere is read against the same rule when it
  is met, and no sweep for one was run.
- **Whether a check or a generator could hold this later.** #941 asks
  for the decision here and the build filed separately. The rejected
  alternatives below state what makes one costly, not that one may
  never exist.
- **What either enum's members are.** Four and three, the same four and
  the same three in both files, unchanged.

## Consequences

- **A member added to one of these enums is prose in two files.** Paid
  by whoever adds it, and nothing reports the second file left behind —
  the same failure mode as the one this ADR answers, now with a rule to
  cite.
- **The two descriptions will drift in wording as well as in content,
  and that is allowed.** A reader diffing them finds differences that
  are not defects. The test is not whether they match but whether
  either leaves a member its own caller can meet undefined.
- **A prose edit to a schema description is never silent.** It moves
  generated output, so `make check-gen` or `make web-check-gen` fails
  until it is regenerated. What they catch is the stale copy and never
  the wrong sentence.
- **`api/openapi.yaml`'s two enum descriptions are now long, and they
  reach `api/server.gen.go` and `web/src/types/api.ts` as one-line doc
  comments.** That is where the console developer meets them, and it is
  the reader the length is spent on.
- **A reviewer who misses it leaves no trace.** No run reports a member
  left undefined, so the failure mode is ADR-0057's: a correct-looking
  green on a change that skipped the second file.

## Rejected alternatives

- **One description, two renderings — #941's first reading.** It is
  coherent while both descriptions say only what a member means, and
  #937 is what ended that. Three of its four sentences define members
  and the fourth tells a reporter when to choose one, so a single
  source renders to the browser-facing file either whole — putting a
  reporting instruction in front of a caller that can report nothing —
  or under a rule saying which sentences cross, which is this ADR's
  reading wearing the other's name. What it would buy is that a member
  cannot be defined in one file and left undefined in the other, and
  that is bought here by stating the obligation instead.
- **One description with a generator.** The cost is not that a
  specification may not be part-generated: `docs/convention/README.md`
  §Scope allows one file to be generated in one part and authored in
  the rest. It is that the input would be a third file, authored
  nowhere a caller reads, while ADR-0004 and ADR-0019 make each
  specification the source of truth for its own listener; and that the
  two schemas differ in name, so the generator needs a pairing table
  neither file carries. #941 rules out building one under it in any
  case.
- **A `make lint` check comparing the two files.** It can compare only
  what it can pair, so it needs that same table, and what it would then
  assert is byte-identity — the reading rejected above. Under this
  ADR's rule there is nothing mechanical left to compare: whether a
  sentence defines a member is not a string relation between two files.
- **Copying `api/machine.yaml`'s `ProvisioningState` description across
  verbatim.** It is the cheapest fix and it closes #941's stated
  defect. It also publishes "report it only from an observation
  that…" on a listener that accepts no report, and it settles nothing —
  the next divergence arrives with both readings still available, which
  is the state #941 was filed to end.
- **Leaving it undecided and settling each case in review.** That is
  the state #941 measured: two defensible readings, one enum pair
  diverged, its twin identical by accident, and nothing for a reviewer
  to cite. The cost of stating it is this file and two descriptions;
  the cost of not stating it was #937 shipping a specification that
  says least about the states its own reader renders.
