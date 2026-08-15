# Review Conventions

What makes a PR review valid, and how authors respond. Applies to every review and every response; violations are grounds for rejection.

## Submitting a review

- **Reviews are formal.** Decisions are submitted as approve / request changes / comment via the review mechanism — never as bare comments. Review states gate merges; comments do not.
- **Every review body carries the evidence table**, approval included:

```
| Check | Result | Evidence |
|---|---|---|
| Scope | pass / fail / unverified | — |
| Conventions | … | rule file §section — diff file:line |
| Architecture | … | … |
| Documentation | … | … |
| Verification | … | … |
| Depth | … | … |

Decision: approve / request changes / reject
```

- An approve requires the Scope, Verification, and Depth rows to be `pass` or `fail` — never `unverified`. The other three rows may be `unverified` only with a stated reason (some PRs touch nothing a convention governs).
- A check you did not verify is marked `unverified` — never guessed. Skipping a check is allowed; hiding the skip is not. A review full of `unverified` rows tells the reader the review did not really happen.
- **Reject maps to request changes.** GitHub has no `reject` review state, so a reject decision is submitted as a request-changes review whose decision line reads `reject` (in single-account setups, the `comment` substitute per §Single-account setups). What reject does that request changes does not: the reviewer closes the PR and files an issue describing the right direction.

## Citing violations

- A violation claim cites both sides: `rule file §section` and `diff file:line`.
- If you cannot cite a rule, it is a preference. Preferences never block a merge.

## Blockers and nits

- Tag every requested change **blocker** or **nit**.
- A nit never blocks a merge. A blocker always states the expected fix direction — "wrong" without "do this instead" is noise.

## Single-account setups

When the PM and authors share one GitHub account, `comment` is the only review state GitHub accepts on one's own pull request — `approve` and `request changes` are both refused. Every decision is submitted as a `comment` review carrying the evidence table and its decision line, plus one line saying the state was substituted because GitHub allows no other. That comment is the review; it is not a bare comment.

- **Approve.** Post the review, then merge immediately — the merge is the approval.
- **Request changes.** Post the review and leave the PR open. The author responds to it as to any request-changes review.

## Responding to a review (author)

- Address every blocker, or rebut it by citing a rule or path that supports your approach. Silent ignoring is not allowed.
- Answer nits too: fix, or state briefly why not.
- Push fixes as new commits to the same branch; never open a replacement PR.
- A point about a commit message is fixed in the commit that carries it — amend or rebase, then force-push the same branch. A later commit cannot remove text from an earlier one, so it cannot carry that fix. The branch and the PR stay the same.
