# ADR 0110: A new ADR carries the four sections of the ADR template as headings

> Status: accepted
> Date: 2026-10-18

Drafted for #1240, which reserves this number. The direction — bind
new ADRs to the template's four sections as required headings, allow
further sections, leave merged ADRs untouched — is the PM's, in #1240's
comment at `57fcdd227b19d57b0f613c89a4bf5b7511475f4d`. Facts about the
record hold at `64dd92b279baae1519101a8f85396dc17c7f57df`.

## Context

[adr/0000-template.md](./0000-template.md) gives an ADR four `##`
sections: `## Context`, `## Decision`, `## Consequences` and
`## Signs the decision was wrong`. The fourth arrived with the template
sync in #1238, `c55781acc58b60a916afdf069130151f28e18bbe`, the last
commit to touch that file. No rule in [README.md](../README.md) §Rules
said an ADR here takes that form, so the template was a guide and the
headings an ADR carried were its author's choice.

**The count**, `rg -c '^## (Context|Decision|Consequences|Signs the decision was wrong)$'`
over each numbered file under `docs/architecture/adr/`:

- ADR-0001 through ADR-0095, 93 files (0068 and 0076 do not exist),
  give 3 each: the first three sections, written before the fourth
  existed.
- ADR-0096 through ADR-0107, 11 files (0099 does not exist), give 4
  each, and so does ADR-0109, merged in #1496 at
  `64dd92b279baae1519101a8f85396dc17c7f57df`.
- ADR-0108, merged in #1492 at
  `0c99062fc9c77093dd3a4c2b1bcc0be38f26a8ca`, gives 3. It carries
  `## Context`, `## Decision`, `## Consequences` and a
  `## Rejected alternatives`, and no `## Signs the decision was wrong`.

The expression reads both values it is used to separate: it gives 4 on
the template itself and on ADR-0096, and 3 on ADR-0108, whose headings
`rg -n '^## '` lists.

**The record was divided.** Thirteen ADRs were added after the fourth
section existed — `git merge-base --is-ancestor` puts
`c55781acc58b60a916afdf069130151f28e18bbe` behind the commit adding
each — and all thirteen passed review; twelve carry it and one does
not. When the PM decided #1240, at
`57fcdd227b19d57b0f613c89a4bf5b7511475f4d`, every ADR merged since
#1238 carried all four; ADR-0108 merged after that commit and before
the rule existed, which is the gap this decision closes.

## Decision

- **Every ADR a pull request adds after this decision merges carries
  the four sections of [adr/0000-template.md](./0000-template.md) as
  `##` headings spelled exactly as the template spells them.**
- **Further sections are allowed.** `## Rejected alternatives`,
  `## What this does not decide`, `## Contents` and the like stay the
  author's to add where the decision needs them.
- **An ADR already merged is held to the form it was written in.** Rule
  2 freezes its body, and adding a section is not one of rule 2's
  exceptions, so every ADR merged before this one is out of scope
  however many of the four it carries.
- **The rule is rule 10 of [README.md](../README.md) §Rules** and this
  ADR carries why, the split rule 3 sets for a decision whose final
  state is a rule of the index (ADR-0062).

## Consequences

- **A reviewer checks the form by a command.** The count above, run on
  the added ADR, gives 4 or the pull request is incomplete.
- **Nothing in `make ci` checks it.** A check would have to tell an
  ADR added after this rule from one merged before it, and the number
  does not: the PM reserves a number in the issue and the ADR
  merges when its pull request does, so ADR-0107 merged at
  `bc0dfca5c42fa102ced311f10de790155f6d1ba3`, after ADR-0108 at
  `0c99062fc9c77093dd3a4c2b1bcc0be38f26a8ca`. Review holds the
  rule, as review already holds rule 3's pairing.
- **ADR-0108 stays as it is.** It is not in violation: it merged before
  the rule, and the rule binds the pull request that writes the ADR —
  the reading `docs/convention/README.md` §Scope gives the
  conventions.
- **The order of the four is not decided.** Every ADR that carries them
  keeps the template's order; the rule names the headings and not
  their sequence.
- **A later template sync that changes the four sections changes what
  this rule requires.** The rule names the template as the source of
  the four, so a sync that renames, adds or drops one is a change to
  rule 10 and is read under rule 4 in the pull request that makes it.

## Signs the decision was wrong

- ADRs added after this one carry a fourth section filled with text
  that says nothing the other three do not — "none known", or a
  restatement of §Consequences — which would show the section binds a
  form without producing the record it was introduced for.
- Authors start leaving out a section a decision needs because the
  required four feel like enough, which would show "further sections
  are allowed" reading as "further sections are not expected".
- Review misses a missing heading often enough that a merged ADR fails
  the count, which would show the check belongs in `make ci` rather
  than in review.

## Rejected alternatives

- **Leaving the template a guide.** The record shows what that buys:
  twelve ADRs took the form and one did not, with nothing to say which
  was right. A reviewer could not cite a rule against ADR-0108, and the
  next author meets the same unanswered question.
- **Requiring the fourth section and leaving the first three to
  practice.** Every numbered ADR carries the first three, so binding
  them costs no author anything and keeps the rule one line about one
  form instead of a list of exceptions.
- **Requiring the four and nothing else.** 55 of the 57 ADRs numbered
  0050 and above carry a `## Rejected alternatives`, and 41 ADRs carry
  a `## What this does not decide`; #1240's decision allows them.
- **Adding the missing section to ADR-0108.** Rule 2 forbids it, and
  ADR-0108 broke no rule when it merged.
