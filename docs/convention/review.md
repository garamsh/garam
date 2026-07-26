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

- A check you did not verify is marked `unverified` — never guessed.

## Citing violations

- A violation claim cites both sides: `rule file §section` and `diff file:line`.
- If you cannot cite a rule, it is a preference. Preferences never block a merge.

## Blockers and nits

- Tag every requested change **blocker** or **nit**.
- A nit never blocks a merge. A blocker always states the expected fix direction — "wrong" without "do this instead" is noise.

## Single-account setups

When the PM and authors share one GitHub account, GitHub rejects `approve` on one's own PR (comment and request-changes still work). In that setup:

- **The merge is the approval.** Post the evidence table as a `comment` review, then merge immediately.
- Request-changes reviews work normally — use them as usual.

## Responding to a review (author)

- Address every blocker, or rebut it by citing a rule or path that supports your approach. Silent ignoring is not allowed.
- Answer nits too: fix, or state briefly why not.
- Push fixes as new commits to the same branch; never open a replacement PR.
