# Current State

What neither the roadmap, the ADRs, nor the issue tracker records. Tier
progress lives in [roadmap.md](./roadmap.md); shipped work is in the git
log and the closed issues; open work is `gh issue list`. Do not mirror
them here — this file went stale twice doing exactly that (#65).

## Standing deferrals

Known gaps that are deliberate, not backlog. None has an issue, because
none is scheduled.

- **Resource-based policies** — a statement names no `Principal`, so a
  policy reaches subjects through an identity attachment only
  (ADR-0008).
- **Per-org direct roles** — a role stored on the user applies in every
  org it belongs to. Scoping needs subject × role × scope persistence.
- **`Condition` in policy documents** — reserved: a statement carries
  one through the parse boundary and reads it back as it was written,
  and nothing evaluates it (ADR-0002). It is the `Statement` type in
  `internal/authz` that holds it, as raw JSON, because the model names
  nothing that may appear inside it; no evaluation path reads that
  field.

## Known corrections applied

Why the code looks the way it does where the history alone would
mislead.

- The module path is `github.com/garamsh/garam`. It spent a period
  under an interim name and was renamed back (#96); the name is uniform
  across the module path, `cmd/garam`, the proto package
  `garam.authz.v1`, `GARAM_*` variables, session cookies, and the
  `garam_policy_change` NOTIFY channel.
- Authorization was once headed for Rego/OPA. That is superseded by
  IAM-style documents (ADR-0002) and the legacy interfaces were removed
  (#28, #40, #47, #59). No Rego package exists.
- The in-process suite against real Postgres lives in
  `tests/integration/` with a root-relative migrations path, after
  several attempts to place it (#53, #58, #64, #70) and a rename out of
  `tests/e2e/` (#874), which now holds only what drives the built
  binary.
- There is one dev Postgres per machine, not per worktree, and `make
  integration e2e` owns a separate `garam_test` database because the
  first of the two truncates (#112). `deploy/local/README.md`
  §Databases is the detail.
