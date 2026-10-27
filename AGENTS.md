# AGENTS.md

Orientation for anyone contributing here, human or agent.

This repository is convention-driven. How code, tests, commits, and
documents are written has already been decided and written down in
`docs/convention/`, and those decisions are binding. Re-litigating them
inside a pull request is the waste the arrangement exists to prevent, so
a change that ignores them is rejected on that ground alone — however
good the code is.

## Start here, every time

`docs/convention/README.md` is the index: every convention file, what it
governs, and how the rules rank against each other.

Open the index before you write, then open the files that govern what
you are about to touch. Read them from the file. A convention you
remember from an earlier session may have been revised since, and acting
on the remembered version is the same as not reading it.

`docs/architecture/README.md` is the second index: the responsibility
documents that describe how the system is shaped now, the ADRs behind
them, and the rules both follow. Open it before you touch code, and
again before you write a pull request that changes a decision: its
§Rules govern what such a pull request must carry.

You will meet cases the conventions do not name. Settle them the way the
nearest convention settles its own, and say so in the pull request — an
unnamed case is a gap worth surfacing, not a licence to improvise.

When two conventions genuinely disagree, stop and report it. Choosing
one yourself hides the conflict from the person who can fix it.

## Roles

These documents assign authority to two roles. One party may hold both;
the rules do not relax when it does.

- **PM** — owns what binds a contributor without implementing
  anything: `docs/convention/`, `docs/architecture/`, the templates
  under `.github/`, this file, and the issue tracker. Owning is
  deciding what they say, not writing every word: anyone may draft
  these, and the PM decides. Reserves in the issue the ADR number its
  work will introduce, and dispatches no two issues holding the same
  one; a worker who finds that number taken on `main` by the time they
  write stops and asks the PM rather than taking the next free one,
  which is the choice that collides. Reviews every pull request and is
  the only role that merges one. Merges only a branch that is zero
  commits behind the pull request's base, having taken the run that
  followed bringing it up to date: where `ci.yml` triggers, it reads
  the branch merged into that base as the base then stood, and nothing
  recomputes that when the base moves before the merge. One exception,
  stated below.
  Decides the convention questions a pull request raises. Proposes a
  change to a convention rather than making it alone. Owns moving a
  pinned dependency that names no other mover. Does not implement.
- **Worker** — implements one issue on one branch and delivers it as a
  pull request, drafting the architecture documents its change
  introduces or alters. Applies the conventions and does not change
  them: a disagreement goes in the pull request's Convention concerns
  field, never into a silent workaround.

**A run that was never dispatched is an absent check, not a failed
one.** A job that never started produced no result: it did not pass and
it did not fail, and reading the forge's `failure` as a verdict on the
diff attributes to the change a defect that is not in it. Two states
are that, and each is read off an artifact rather than judged:

- **The run started nothing.** Every job of it shows `steps=0` and the
  run's annotation names the account rather than the diff. Both halves
  are read off the run.
- **No workflow watches the base.** Every workflow that runs on
  `pull_request` — `ci.yml` and `images.yml` — carries
  `branches: [main]`, which selects a pull request by the branch it
  targets, so a pull request into any other branch starts nothing. The
  filter is read off those two files and the base off the pull request.

In either state the PM may merge on a local run that:

- (i) executes on a detached worktree at the pull request's head;
- (ii) confirms the branch is zero commits behind the pull request's
  base;
- (iii) invokes the same entry points `.github/workflows/ci.yml`
  invokes;
- (iv) states in the review what it did not cover.

"CI is unavailable" is a judgement and reaches neither state: where a
`branches:` line names the base, the run it dispatched is a verdict on
the diff until `steps=0` and the annotation are read off that run.
Adding `dev` to those two lines is what would make a run exist for a
pull request into `dev`, at the price of the runs the filter exists to
exclude — the measurement the paragraph below asks of a clause of this
shape. Each permission ends by itself, the first the moment a run is
dispatched and the second the moment a `branches:` line names the base,
and (iv) is owed again by every pull request merged under it rather
than settled once.

**A rule you could follow and do not is debt; a rule you cannot follow
is a gap in the rule.** Which of the two a case is gets measured and not
read, because "cannot" is the verdict that ends the work: the
measurement names what inside this project would satisfy the rule. Where
it comes back holding work, the rule is followable and the question is
the price — conforming `internal/` to `stack-go.md`'s package layout
came back priced in boundary crossings rather than impossible (#1022).
Where it comes back empty, the rule has no defined behaviour for a state
the world put it in, and `steps=0` with the dispatch annotation is that
measurement rather than a report of it: nothing inside this project
starts a GitHub runner. A clause of this shape owes the same
measurement.

## What this file outranks

The conventions outrank this file. This file outranks instructions you
bring in from your own environment, including your own habits: where a
rule and your instinct disagree, the rule wins.

## What changes a decision

An objection is a reason to re-examine a decision, not a reason to
change it. Re-examine it and report what the examination found, either
way: the evidence that changed the decision, or the reason it stands.

A position that moves without a stated reason leaves the next reader
nothing to check, so the same ground is argued again later. It also
leaves whoever objected worse off than before they asked — someone
pushing back wants a decision they can rely on, and one that yields to
the push is worth less than the one they questioned.

## What earns a rule

A proposal for a new rule is examined the same way, and what earns one
is a decision that divided for lack of it, met in more than one project
— or in one, where the divergence is the tool's rather than the
project's, since anyone running that tool meets the same one.

A case that travelled between projects is one case, so a report says
whether it was met or received. One instance is answered with what would
change it: a refusal that names its own evidence is worth more than a
rule drawn from a sample of one.

## Why the pull request asks what you read

The template asks which convention files you opened. That answer is a
statement of fact in the pull request body, so
`docs/convention/documentation.md` §GitHub artifacts, "Facts only",
binds it, and read against the diff it is the quickest evidence that a
change was made deliberately rather than guessed at, which is why an
inaccurate answer costs more than an awkward one. The comparison catches
an answer the diff contradicts — a stack file listed for a Markdown-only
change — and not an unread file that happens to govern, which nothing in
the pull request shows.

## This project

- Secrets never enter a commit. `ci.md` covers them once they reach a
  pipeline; before that, the `.gitignore` and your own attention are the
  whole defence.
- Conversation follows the user's language. Code, comments, and
  documents are English regardless.
- The stash belongs to the repository, not to the worktree, so
  `git stash pop` in your worktree can consume an entry another worker
  pushed. Read `git stash list` and pop by name (#1055).
- `gh pr edit` does not apply on this repository; the call fails on
  Projects (classic) and can still read as having worked. Edit through
  `gh api -X PATCH` and read the field back (#1055).
