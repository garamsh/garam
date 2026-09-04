# What each template adoption put in violation, measured at its own commit

A survey of every commit that took content from the conventions
template, taken for #1015. It changes no convention file, no
architecture file, no workflow and no source, and proposes no rule.

Checked against `4cc6ebc`, with `template/main` at `a6a2355`. Every
count below was taken from `git show <commit>:<path>` at the commit
named beside it, not from the working tree.

## Contents
- What the arm is, and what it reaches
- How the adoptions were enumerated
- The controls on the enumeration
- The forty-four adoptions
- The four the issue names
- The six adoptions the arm reaches
- The same question over the convention adoptions
- The rules the tree satisfied
- The rules the tree did not satisfy
- What stands violated today
- What was not measured
- What this does not settle

## What the arm is, and what it reaches

`docs/architecture/README.md` rule 4 puts a change to the rules under
`docs/architecture/` on its own ground: such a change earns an ADR
"unless both arms hold — no passage that had passed review is in
violation after it, and the record it writes down was undivided".
[ADR-0060](../architecture/adr/0060-a-rule-change-under-docs-architecture-owes-an-adr-unless-the-record-was-undivided.md)
§Decision states the first arm and says the pull request's own file set
is where it is read.
[ADR-0061](../architecture/adr/0061-adopting-a-rule-the-template-decided-is-a-decision-taken-here.md)
§Decision adds that an adoption is read by the same two arms, and that
where a rule was first decided is no part of the test.

Two boundaries follow, and the issue's framing crosses both.

- **The arms reach `docs/architecture/` and nothing else.** Rule 4's
  second clause says a rule in `docs/convention/` "earns no ADR however
  much it binds", and ADR-0061 §What this does not decide says the same
  of an adoption into that directory: "Nothing, and rule 4's second
  clause already says so." Of the four commits the issue tabulates,
  none touches `docs/architecture/`. The arm does not reach them, so
  "the arm has never been exercised" over those four is true the way it
  is true of any rule over a case outside it.
- **Arm one asks about a standing passage, not about the tree.** Its
  unit is "a passage standing in the repository: a sentence in a
  responsibility document, in an ADR, or in this index". A workflow
  file, a Go package and a Markdown file outside `docs/architecture/`
  are none of those.

The question the issue actually wants answered is wider than the arm:
did an adopted rule leave content in this repository failing it, at the
commit that adopted the rule. That is the question this report
measures, and it is reported separately from the arm proper wherever
the two diverge.

## How the adoptions were enumerated

The issue's list came from `git log --grep='sync template|template
updates'`. Run here it returns four commits — `b117e89`, `a10efd3`,
`cbe503c`, `9611ac4`, exactly the four tabulated. Nothing about a
commit title makes a commit an adoption, so the enumeration below
starts from content instead.

`template` is a remote of this repository, so the template's history is
readable here. At `a6a2355` it holds 206 commits and 34 tracked paths;
55 paths appear somewhere in that history.

- **The path set.** Every path any commit of `template/main` ever held
  — 55 of them.
- **The line set.** For each of those paths, every line of at least 40
  characters the template ever held at it, with the date of the
  template commit that first held it. 3017 such lines.
- **The sweep.** Every commit on `main` that touched one of the 55
  paths — 241 of them. For each, the lines it added at that path, at
  least 40 characters long, that the template already held at the same
  path on an earlier date.
- **The verdict.** A commit with at least one such line is an adoption.
  44 commits qualify.

The 40-character floor keeps blank lines, `## Contents`, `- Context`
and the like out; the same-path requirement and the date ordering keep
out a line this repository wrote first and the template took later.

## The controls on the enumeration

The failure this enumeration is exposed to is a filter too narrow to
see a category, which is what the issue says the grep did. Two
alternative derivations were run against the same history, and each is
reported with what it caught and what it missed.

- **Blob identity — too narrow, and the control says so.** Taking a
  commit as an adoption when it produced a file byte-identical to one
  the template ever held gives 32 commits. Fired at a case known to
  qualify it comes back empty: ADR-0061 §Context records #603 as adding
  rule 5 "byte-identical to the template's", and `6c650c3` is not among
  the 32, because the file it edited carries this project's own rules
  around the adopted line. The same is true of #612 and #762. Blob
  identity is reported as a subset of the 44 and used for nothing else.
- **Cross-path identity — wider, and it adds nothing.** Matching an
  added line against the template's lines at *any* path, rather than
  the same one, gives 16 commits. Twelve are already among the 44. The
  four that are not carry 1, 1, 1 and 4 matching lines each and no rule
  on reading them: a code example copied out of `stack-nextjs.md` into
  `web/src/proxy.ts` (#100), a workflow line shared between
  `ci.yml` and `images.yml` (#857), a shell idiom shared between
  `check-contents-list.sh` and `scripts/check-line-width.sh` (#1009),
  and the re-wrapping of paragraphs #612 had already adopted (#1001).
  The union is 48 candidates, 44 adoptions and 4 rejected on reading.
- **The four the issue names are all inside the 44.** So the issue's
  list is correct and 4 of 44 complete. Its per-commit file counts were
  checked too and all four hold: #607 is 4 of 4 under
  `docs/convention/`, #379 is 3 of 5, #126 is 0 of 4, #123 is 0 of 2.

## The forty-four adoptions

"Template-first lines" is the count of added lines the template already
held at the same path, over the count of added lines at least 40
characters long. A low ratio is a commit that took a little and wrote
a lot, not a weaker adoption.

| commit | date | pull request | files | template-first lines | area |
|---|---|---|---|---|---|
| `d327b3e` | 2026-07-25 | #1 | 20 | 223 of 245 | convention/architecture |
| `ab6c9aa` | 2026-07-25 | #14 | 5 | 18 of 19 | other |
| `da9f428` | 2026-07-25 | #27 | 7 | 49 of 49 | other |
| `e9f6069` | 2026-07-25 | #34 | 3 | 8 of 8 | other |
| `981c537` | 2026-07-25 | #37 | 10 | 95 of 118 | convention/architecture |
| `613b19f` | 2026-07-26 | #44 | 1 | 3 of 3 | convention |
| `b52fb20` | 2026-07-28 | #45 | 9 | 58 of 58 | other |
| `b51eefa` | 2026-08-02 | #46 | 3 | 30 of 30 | convention |
| `bb50cea` | 2026-08-02 | #49 | 2 | 11 of 62 | convention |
| `1770037` | 2026-08-02 | #50 | 3 | 38 of 38 | other |
| `22fb51c` | 2026-08-02 | #51 | 3 | 9 of 9 | convention |
| `1c3e22f` | 2026-08-02 | #52 | 1 | 26 of 26 | other |
| `25207e9` | 2026-08-02 | #98 | 2 | 283 of 283 | convention |
| `9611ac4` | 2026-08-15 | #123 | 2 | 2 of 2 | other |
| `cbe503c` | 2026-08-15 | #126 | 1 | 1 of 1 | other |
| `ae648d2` | 2026-08-15 | #127 | 3 | 13 of 15 | convention |
| `78ba6a9` | 2026-08-15 | #128 | 4 | 20 of 21 | convention |
| `3351208` | 2026-08-16 | #149 | 7 | 36 of 36 | convention |
| `2abb3ee` | 2026-08-16 | #151 | 8 | 379 of 435 | convention |
| `9f8071a` | 2026-08-16 | #177 | 1 | 9 of 9 | convention |
| `b91a579` | 2026-08-16 | #178 | 4 | 25 of 31 | convention |
| `232d21f` | 2026-08-16 | #206 | 3 | 7 of 7 | convention |
| `5e038b9` | 2026-08-17 | #225 | 4 | 12 of 12 | convention |
| `cde0991` | 2026-08-17 | #291 | 2 | 3 of 3 | convention |
| `d54c0ec` | 2026-08-17 | #293 | 1 | 35 of 35 | convention |
| `34bef09` | 2026-08-17 | #307 | 5 | 10 of 11 | convention |
| `d3df7d8` | 2026-08-17 | #318 | 4 | 6 of 6 | convention |
| `7453f6e` | 2026-08-17 | #323 | 2 | 9 of 9 | convention |
| `19ca6c2` | 2026-08-17 | #325 | 1 | 1 of 1 | other |
| `a10efd3` | 2026-08-19 | #379 | 5 | 16 of 17 | convention |
| `ca3e46d` | 2026-08-23 | #429 | 1 | 1 of 2 | convention |
| `6c650c3` | 2026-08-26 | #603 | 1 | 1 of 2 | architecture |
| `b117e89` | 2026-08-26 | #607 | 4 | 16 of 16 | convention |
| `bfcc1f4` | 2026-08-26 | #612 | 1 | 3 of 5 | architecture |
| `5668576` | 2026-08-26 | #616 | 1 | 1 of 2 | convention |
| `a6b06c0` | 2026-08-26 | #638 | 1 | 58 of 58 | convention |
| `6994a4b` | 2026-08-26 | #643 | 1 | 2 of 4 | convention |
| `b0125ed` | 2026-08-27 | #762 | 1 | 2 of 3 | architecture |
| `226f6fd` | 2026-08-27 | #769 | 2 | 21 of 21 | convention |
| `9aab6be` | 2026-08-27 | #771 | 1 | 1 of 1 | architecture |
| `5d1c118` | 2026-09-02 | #890 | 1 | 1 of 1 | other |
| `040df2d` | 2026-09-02 | #897 | 1 | 14 of 14 | other |
| `5a7c3d7` | 2026-09-03 | #972 | 1 | 1 of 1 | convention |
| `b43961a` | 2026-09-03 | #980 | 1 | 1 of 103 | convention |

Of the 44: 29 touch `docs/convention/`, 6 touch `docs/architecture/`,
2 of those touch both, and 11 touch neither — role documents under
`docs/ai/`, agent adapters, `AGENTS.md`, the pull request template, and
`check-contents-list.sh`.

## The four the issue names

The issue reads `b117e89` (#607) as "the shape": a sync whose whole
file set is `docs/convention/`, so a rule arriving in it appears in no
diff that touches what it governs. Measured, that commit is the one of
the four that comes out clean, and the one with the impure file set is
the one that fails.

- **#123 (`9611ac4`) — brought in no rule.** Two sentences, both under
  `docs/ai/`: one telling a bootstrapping project to start at
  `adoption.md`, one telling a worker to read the index before the
  responsibility documents. Neither governs any content in this
  repository. `docs/ai/` was removed entirely at #127. Verdict: this
  adoption governs nothing here.
- **#126 (`cbe503c`) — one claim, true when written.** It added to
  `AGENTS.md`: "Role adapters live in `.claude/agents/` and
  `.opencode/agents/`". `documentation.md` §Keep it current requires
  every claim about this repository to be verifiable in it. At
  `cbe503c` both directories existed and held the three role files
  each. Both are gone today, and so is the sentence, removed with them
  at #127. Verdict: satisfied at the commit, and not standing.
- **#379 (`a10efd3`) — one rule violated, unnoticed, for 4 to 13
  days.** Detailed under §The rules the tree did not satisfy.
- **#607 (`b117e89`) — every rule it brought was satisfied.** Detailed
  under §The rules the tree satisfied.

## The six adoptions the arm reaches

These are the adoptions that touched `docs/architecture/`, which is the
only ground rule 4's third clause covers. ADR-0060 §Rejected
alternatives refuses to make the test retroactive, so none of these is
judged a breach; the arm is applied here as a measurement.

- **#1 (`d327b3e`) — the file arrives, before the numbered rules.**
  The bootstrap adopted the template's §Structure and three ad-hoc
  edit rules, and its index named all 4 documents it created.
- **#37 (`981c537`) — rules 1 to 4 arrive; arm one holds.** #37
  replaced those three bullets with numbered rules 1 to 4 taken from
  the template. Rule 4 ("every responsibility document and notable ADR
  is listed here") is the one of the four with standing content to
  fail, and at `981c537` 0 of 10 documents under `docs/architecture/`
  were unlisted. Rules 2 and 3 govern what a later pull request does
  to an ADR and had nothing to reach: no ADR had yet been merged under
  them. Rule 1's skeleton was not measured — see §What was not
  measured.
- **#603 (`6c650c3`) — arm one fails, and the pull request ran it.**
  Rule 5, "Every domain or concern has a responsibility document", was
  adopted byte-identical. Its own body carries the heading "**The check
  #602 asks for, run before the rule was added**", enumerates 7
  responsibility documents against 21 packages under `internal/`, and
  reports the one concern with none: `web/`. The finding was named and
  left to the tracker rather than repaired in the pull request.
  `docs/architecture/web-app.md` was added by #631 (`1dbe5bd`) 1 hour
  56 minutes later. ADR-0060 §What this does not decide records the
  same window from the other side.
- **#612 (`bfcc1f4`) — arm one fails twice, and the pull request ran it
  twice.** Rules 6, 7 and 8 were adopted verbatim. Its Convention
  concerns field names both consequences without being asked to: that
  `docs/convention/arch-domain.md` and the new
  `docs/architecture/structure.md` then stated the same rules, against
  `docs/convention/README.md` §Independence's one file, one territory;
  and that rule 7's "it is a decision under rule 4" pointed at a rule 4
  which at that commit read only "Keep this index current". #616
  (`5668576`) retired `arch-domain.md` 9 minutes later; #762
  (`b0125ed`) repaired rule 4 10 hours 26 minutes later.
- **#762 (`b0125ed`) — arm one holds.** Rule 4 widened from "notable
  ADR" to "every ADR". At `b0125ed`, 0 of 57 documents under
  `docs/architecture/` were unlisted, so the widening put nothing in
  violation. Its other adopted clause, that `structures/` is not
  listed, governs nothing here: no commit in this repository's history
  has ever touched `docs/architecture/structures/`.
- **#771 (`9aab6be`) — governs nothing standing.** It added one
  sentence to `adr/0000-template.md` restating that an ADR is
  append-only. The sentence is guidance a real ADR replaces, no ADR in
  the tree carries it, and none is required to.

The arm reaches six adoptions. Five of them brought a rule with
standing content that could have failed it: it failed on two and held
on three. On both failures the author had already run the arm and
written the answer into the pull request body, one of them under that
heading. So the arm is unexercised by any check, and on this ground it
has been exercised by hand every time it had something to catch.

## The same question over the convention adoptions

Outside `docs/architecture/` the arm does not apply, but the substance
does: an adopted rule can leave standing content failing it. Six rules
with a reach wide enough to measure were tested at the commit that
adopted them. Each query was fired at a case known to qualify before a
zero was read as a zero.

- **`documentation.md` §Format, the Contents-list rule.** Adopted at
  #149 (`3351208`) as "A file past ~50 lines opens with a `## Contents`
  list of its section headings; a shorter one does not"; rewritten at
  #607 (`b117e89`) to turn on the heading count. It governs every
  Markdown file in the repository.
- **`documentation.md` §Format, "No decoration".** Present since #37
  under §GitHub artifacts, where it governed issues, pull requests and
  comments; moved into §Format at #149 with "This holds for files under
  `docs/` as much as for GitHub artifacts: a check-mark glyph in a
  table is decoration where `yes` is a word."
- **`documentation.md` §Format, the file-name rule.** Lowercase
  kebab-case, with named uppercase exceptions.
- **`ci.md` §Verify a pinned dependency, the pin rule.** Adopted at
  #151 (`2abb3ee`) as "Pin to a reference that cannot move: a digest,
  or a full commit SHA."
- **`ci.md` §Verify a pinned dependency, the mover rule.** Adopted at
  #379 (`a10efd3`): "a pinned dependency also names how it gets moved —
  a bot that proposes the bump, a scheduled review, an owner".
- **`README.md` §Independence, the no-cross-reference rule.** Adopted
  at #151: "A convention file does not send the reader to another
  convention file." Before #151 the section read "Cross-references may
  mark scope boundaries but never hold required content", which permits
  them.

## The rules the tree satisfied

Named here so the negatives below can be read as negatives.

- **`documentation.md` §Format, the Contents-list rewrite, at #607
  (`b117e89`).** Both wordings were run over the same tree. The
  superseded rule put 12 of 84 Markdown files in violation at
  `b117e89^`; the adopted rule puts 7 of 84 in violation at `b117e89`.
  The two sets were compared by name: the adoption put **0** files in
  violation and lifted **5** out —
  `adr/0003-single-cli-topology.md`,
  `adr/0008-identity-based-policy-attachments.md`,
  `adr/0011-console-shell.md`,
  `adr/0028-oauth-state-is-a-server-side-record.md` and
  `docs/architecture/README.md`. Arm one, had it applied, would have
  held.
- **`documentation.md` H1, "every authored Markdown document" widened
  to "every Markdown document", at #607.** The widening reaches
  generated Markdown, and this repository has none: no Markdown file at
  `b117e89` or at `4cc6ebc` opens with a generated-file header. Control
  on the query: the same search over the tree at `4cc6ebc` names
  `Makefile`, `api/server.gen.go`, `api/machine/server.gen.go`,
  `internal/agent/db/db.go` and `internal/agent/db/models.go`, so it
  finds such headers where they exist. Verdict: governs nothing here.
- **`README.md` §Scope, the forge-template clause, at #607.** It brings
  a template a forge copies into every issue under the conventions, and
  carries its own carve-out in the same sentence: "What it must open
  with, and the order its parts come in, is the tool's." The three
  files under `.github/ISSUE_TEMPLATE/` open with YAML front matter,
  which is exactly what that clause exempts. Nothing goes into
  violation.
- **`git.md` §Commits, the trailer clause, at #607.** It replaced
  "Tooling does not append trailers, footers, or attribution lines"
  with a clause admitting `Co-authored-by:`. It relieves rather than
  binds, so no passage could go into violation by it.
- **`README.md` §Independence, the no-cross-reference rule, at #151
  (`2abb3ee`).** At that commit, 0 convention files other than the
  index named another convention file. The same query fired at earlier
  trees returns 1 at `d327b3e`, 3 at `981c537`, 4 at `bb50cea` and 2 at
  `3351208`, so the zero is readable. The rule went from permissive to
  prohibitive and the same pull request removed the last two
  cross-references, by replacing `go.md` with `stack-go.md`.
- **`documentation.md` §Format, the file-name rule.** 0 Markdown files
  outside `.github/` carry a name outside lowercase kebab-case at
  `b117e89` or at `4cc6ebc`. The same query at `981c537` returns 2 —
  `.opencode/PLAN.md` and `docs/ai/BOOTSTRAP.md` — so it fires.
- **`docs/architecture/README.md` rule 4, index currency, at all six
  architecture adoptions.** 0 unlisted at each, over 4, 10, 53, 55, 57
  and 57 documents read. Control: the same check swept over all 211
  commits that touched `docs/architecture/` fires on 10 of them, the
  last being #105 (`1f06749`) with ADR-0009 unlisted.

## The rules the tree did not satisfy

- **`documentation.md` §Format, the Contents-list rule, at #149
  (`3351208`).** The rule arrived here; §Format had carried no Contents
  rule before it. At the adoption commit **17 of 46** Markdown files
  were in violation, none of them among the 9 files that pull request
  changed. Reading "~50" more generously moves the count to 15 at a
  threshold of 55 and 15 at 60, so at least 15 of 46 stand however the
  approximation is read. The count fell to 11 of 45 at #151 eight
  minutes later and to 4 of 45 at #158 (`ac0e202`) an hour later, and
  never reached zero: `b117e89^` still reads 12 of 84 ten days on. Its
  successor rule was still unsatisfied at 4 of 104 on 2026-09-02, until
  #897 (`040df2d`) adopted `check-contents-list.sh` and `make lint`
  began failing on it. **The rule went 17 days between arriving and
  being satisfied, and what closed it was a check, not a sweep.**
- **`documentation.md` §Format, "No decoration", widened at #149.**
  Moving the rule from §GitHub artifacts to §Format, and naming a
  check-mark glyph in a table, put **13 lines in 2 files** in violation
  the moment it merged: `docs/architecture/domains.md` (5) and
  `docs/convention/stack-nextjs.md` (8). Neither file is in that pull
  request. `stack-nextjs.md` was cleared 8 minutes later at #151, as a
  side effect of re-syncing a file the template had itself just
  cleaned; `domains.md` was cleared 59 minutes later at #158, whose
  title says what it was: "bring existing documents up to the new
  documentation rules".
- **The same rule against the content an adoption itself brought, at
  #98 (`25207e9`).** `stack-nextjs.md` was adopted whole, carrying the
  8 glyph lines the template had held since its own first commit. At
  that date "No decoration" governed GitHub artifacts and not files, so
  the adopted lines broke nothing then; #149 is what made them a
  violation, 14 days later. Worth separating from arm one, which asks
  only about passages that had already passed review: text an adoption
  brings in is not one, so the arm as written does not reach this
  class at all.
- **`ci.md`, the pin rule, at #151 (`2abb3ee`) — violated, and caught
  in the same minute.** At the adoption commit **10 of 10** `uses:`
  references in `.github/workflows/ci.yml` pinned a tag. Issue #155 was
  filed at 04:17:45Z, 1 minute 56 seconds before #151 merged at
  04:19:41Z, and quotes the rule's before and after wording. #157
  (`f6377c3`) pinned all ten at 05:10:35Z, 51 minutes after the
  adoption. The query reads 10 of 10 before and 0 of 10 after, so
  neither number is a broken query.
- **`ci.md`, the mover rule, at #379 (`a10efd3`) — violated, and
  nothing in the pull request asked.** The adopted sentence requires a
  pinned dependency to name how it gets moved. At `a10efd3` the tree
  named none: a search of `.github/`, `AGENTS.md`, `docs/convention/`
  and the `Makefile` for `dependabot`, `renovate`, `scheduled review`
  or an owner returns exactly one line, the rule itself. The same
  search at `4cc6ebc` returns 11 lines across `.github/dependabot.yml`,
  `.github/workflows/ci.yml`, `AGENTS.md` and the `Makefile`, so the
  one-hit result is a real absence. #379's own body lists the rule
  under "Adopted", records its verification as "`make ci` — pass" and a
  diff against `template/main`, and writes `N/A` in Convention
  concerns. `.github/dependabot.yml` first appears at #423 (`9b12cc4`),
  **4 days 9 hours** later; the residue — the pins no bot proposes —
  was given an owner in `AGENTS.md` at #804 (`ca80f71`), **13 days 1
  hour** later.

Counted as adoptions rather than as rules: **5 of the 44** left
standing content in violation, over 7 rule-instances. Four of the five
were seen within the hour — #603 and #612 in their own pull request
bodies, #151 in an issue filed 2 minutes before it merged, #149 in
#158, a repair pull request 59 minutes after whose title names what it
was for. Seen is not the same as closed: #149's Contents rule was
still unsatisfied 17 days later. **One was not seen at all:
#379.** It is one of the four the issue names, and it is not the one
the issue names as the shape — #607, whose file set is 4 of 4 under
`docs/convention/`, is the commit that comes out clean.

## What stands violated today

- **Nothing among the rules measured above.** At `4cc6ebc`: 0 of 113
  Markdown files fail the Contents rule, 0 carry decoration, 0 carry a
  name outside the file-name rule, 0 of 81 documents under
  `docs/architecture/` are unlisted, 0 of 17 `uses:` references are
  unpinned, and every pinned dependency has a named mover. Each of
  those zeros is a query shown above to return non-zero somewhere in
  this history.
- **Two cross-reference candidates, judged not violations.** The
  §Independence query returns 2 hits at `4cc6ebc`:
  `documentation.md:116` uses `runtime-safety.md` as an example of a
  file name, and `git.md:46` names `stack-go.md` inside an example
  commit title. Neither sends a reader to that file for a rule, which
  is what the rule forbids. Recorded because the query fired, and a
  bare count of 2 would read as two violations.
- **The hard-wrap rule, 141 violations in 46 files — and it is not an
  adoption.** `scripts/check-line-width.sh` reports them at `4cc6ebc`.
  The rule was written here at #975, not taken from the template: the
  template's `documentation.md` §Format carries no wrapping rule at
  any commit in its history. It is named here only so that the clean
  result above is not read as a claim about the whole corpus.

## What was not measured

- **The rules 31 of the 44 adoptions brought in.** The six convention
  rules tested above are the ones whose governed set is enumerable from
  the tree. The
  adoptions of `stack-go.md` (#151, #178, #225, #323, #638, #643),
  `stack-nextjs.md` (#98, #151, #177, #178, #293),
  `stack-tailwind.md` (#98, #151), `stack-container.md` (#769),
  `testing.md`, `code-comments.md`, `simplicity.md`, `review.md` and
  `arch-domain.md` brought hundreds of rules governing Go packages,
  React components, test files and reviews. None was run against the
  tree at its adoption commit. Their verdict here is *not measured*,
  not *satisfied*.
- **`docs/architecture/README.md` rule 1, the responsibility document
  skeleton.** Whether a document's current decisions are consolidated
  and synthesized is not decidable by a query, which §What nothing
  detects in that file already records at length.
- **Every rule governing a review or a commit message.** `review.md`
  gained rules at #37, #44, #51, #128, #149, #291, #307, #318, #379,
  #429 and #607, and `git.md` at #1, #37, #128, #149 and #607. These
  govern artifacts on GitHub, not passages in the repository, and the
  arm's unit does not reach them.
- **Whether any measured violation was reported at the time.** The
  pull request and issue bodies for #603, #612, #151 and #379 were
  read; the rest were not.

## What this does not settle

- **Whether a rule is wanted.** That is the PM's and the owner's, and
  ADR-0061 has already decided the adjacent question — an adoption owes
  an ADR under the same two arms, and §Decision records that nothing
  checks it, deliberately, so that "the next measurement" would not
  file the absence as an unnoticed gap. This report is that next
  measurement, and the absence is not filed as a gap.
- **What evidence would earn one.** Under `AGENTS.md` §What earns a
  rule, a new rule wants a decision that divided for lack of it, met in
  more than one project — or in one, where the divergence belongs to
  the tool rather than the project. What this report holds is one
  project, and one clear instance in it: #379's mover rule. The peer
  project the issue cites is a *received* case, not a met one, and
  §What earns a rule says a case that travelled between projects is one
  case. So what would change the picture is the same class met in a
  second project and reported as met, or a second instance here where
  the sync's author had no way to ask the question rather than simply
  not asking it. Against #603 and #612, where the author did ask and
  wrote the answer down, one instance of not asking is a thin sample.
- **Whether #379 was a breach.** It was not, and nothing here says
  otherwise: `docs/convention/` adoptions owe no arm, and rule 4 binds
  what is written after it.
- **What to do about the 31 unmeasured adoptions.** Naming them is
  cheap; running their rules against the tree at their commits is not,
  and no part of it was attempted here.
