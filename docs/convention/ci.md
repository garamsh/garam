# Continuous Integration

How a project's checks are named, run, and secured. The rules hold on
any toolchain; one toolchain appears at the end as a reference
implementation.

## Contents
- Core principle
- One entry point per task
- Run the checks before pushing
- Reuse
- Verify a pinned dependency before adopting it
- Security baseline
- Reference implementation

## Core principle

Local checks and CI checks are the same checks, invoked by the same
names. CI re-runs what a contributor already ran; it does not define a
second way to run it.

## One entry point per task

- **One name per task.** Each of lint, format, test, and build has
  exactly one name that a human and CI both invoke. What implements that
  name — a task runner, a script, a package manifest entry — is the
  project's choice; the name is the contract.
- **One name for the whole set.** A single name runs lint, format, test,
  and build in order, so "run the checks" is one command locally and one
  step in CI.
- **CI calls the names, not the commands behind them.** A pipeline that
  spells out the underlying tool invocations has created a second set of
  commands, and the two drift.
- **An entry point reports what it did.** A suite that skipped every
  test, a generator that found no input, a check that read only tracked
  files — each exits zero and reads exactly like the run that did the
  work. The output has to tell those apart: a count of what was covered,
  or a failure when the count is zero.
- **Entry points stay thin.** An entry point that has grown past a few
  lines is one name doing several jobs; re-split it into named tasks
  rather than letting it become a script. What it may not become is a
  pipeline component: it runs on a contributor's machine as well as in
  CI.

## Run the checks before pushing

- Run the project's checks locally before code leaves the machine. CI is
  the second line of defence, not the only one.
- Automating that locally — a version-control hook, a watch task, an
  on-save editor command — is the contributor's choice. No tool is
  mandated, and running the checks by hand satisfies the rule.

## Reuse

A pipeline step is one of two kinds. A check — lint, format, test, build
— is invoked by its entry-point name (§One entry point per task).
Everything else a pipeline needs — toolchain setup, caching, artifacts,
deploys — is a step this section governs.

For each of those steps:

1. **Search for a maintained component** — a published action, plugin,
   or reusable job — at its current version, before writing pipeline
   code.
2. **Verify it** per the next section before pinning.
3. **Use it as its own documentation shows**. If no maintained component
   covers the step, a checked-in script is the fallback — not a license
   to hand-roll per-job.

## Verify a pinned dependency before adopting it

Before pinning anything the pipeline pulls in — a published component,
container image, or tool version — read its own documentation and
determine:

1. **Current stable version** — not the remembered one. The version an
   agent recalls may be deprecated, archived, or replaced.
2. **Exact inputs that version accepts** — inputs shift between
   releases. Check them against the version's own documentation.
3. **Exact usage pattern** — follow what the documentation shows. Do not
   paraphrase inputs or rearrange the documented pattern.

Pre-trained recollection is a starting point, not source of truth. An
agent that picks a version from memory silently uses a stale or
nonexistent release.

Pin to a reference that cannot come to mean something else. A digest or
a full commit SHA always satisfies that. So does a version where the
ecosystem verifies content against a checksum log the repository also
records — a Go module version under `go.sum` and the checksum database,
where republishing a tag over different content fails verification
instead of substituting silently; bypass that database and the property
goes with it. A tag resolved through a mutable pointer, and any branch,
satisfies it nowhere.

A pin fixes what a reference means, not what it points at: a digest
never receives the patch its tag would have carried. So a pinned
dependency also names how it gets moved — a bot that proposes the bump,
a scheduled review, an owner — because pinning without that trades a
moving dependency for a frozen one.

For every third-party dependency, also check the ecosystem's advisory
source for known vulnerabilities in the candidate version.

## Security baseline

- **Minimal permissions**: the pipeline's default credential grants read
  only; grant write on the single job that needs it.
- **No plain-text secrets**: keep tokens, keys, and credentials in the
  platform's secret store. Prefer short-lived federated credentials
  (OIDC) over long-lived keys for cloud access.
- **Avoid script injection**: pass attacker-controlled input to a
  command through the environment, never interpolated into the command
  text.
- **Mask derived values**: values derived from secrets that can reach
  the logs are masked with the platform's masking mechanism.

## Reference implementation

The rules above bind. This section does not: it is one toolchain — Make
for the entry points, GitHub Actions for the pipeline — that satisfies
them. A project on another toolchain replaces this section and keeps
every rule.

### Entry points as a Makefile

The living list is the `Makefile`. This project has two toolchains and
one whole-set name over both:

- `ci` — `format lint test build check-gen check-toolchain chart
  web-ci`, the Go module, the chart it ships, and the `web/` half
  below.
- `web-ci` — `web-format web-lint web-test web-build web-check-gen`,
  the `web/` app, which has its own package manager and its own gate.
  `ci` runs it last, so the cheaper Go half fails first.

Both run identically on a contributor's machine and in CI. The rest:

- `gen` regenerates oapi-codegen, sqlc, buf and mockery output;
  `check-gen` fails when what is committed is stale.
- `db-up` / `db-down` bring up and remove the dockerised Postgres the
  two Go suites below need; `GARAM_DB_PORT` moves that one server's
  host port, default 25432.
- `db-prune` drops the per-worktree test databases `db-up` created for
  worktrees that are gone. Nothing else collects them short of removing
  the volume, so a stack left up accumulates one per worktree.
- `backend-up` brings up postgres, migrate, authz and api from the
  same compose file — the backend the browser suite drives.
- `integration` runs the store round-trips and the services over them
  in process, against real Postgres and a database of its own per
  worktree; `e2e` runs the built binary against that same database.
  `web-e2e` runs Playwright against the built app and `backend-up`'s
  backend. `image-build` builds the images a release publishes, each
  from what the release workflow says it is built from, and
  `web-image-e2e` runs `tests/image/`'s checks against the web one
  rather than against the app.

`ci` and `web-ci` stop short of the four suites that need Docker.
Locally they are `make db-up integration e2e`, `make backend-up
web-e2e` and `make web-image-e2e`. CI runs each of those three
commands as a job of its own; the last builds two images before it
checks one, so it runs from a workflow of its own that a
documentation-only pull request does not start.

### GitHub Actions specifics

- **Existing components**: search the GitHub Marketplace before writing
  inline `run:` blocks.
- **Advisories**: for every third-party action, check the GitHub
  Advisory Database
  (`github.com/advisories?query=type%3Areviewed+ecosystem%3Aactions`)
  for the candidate version.
- **Toolchain setup**: use the official setup action for the project's
  language; take its version and inputs from its own docs.
- **Checks**: run `make lint`, `make format`, `make test`, `make build`
  — the targets, not the commands inside them.
- **Artifacts**: use the artifact action.
- **Permissions**: `permissions: read-all` at the workflow top; `write`
  granted per job.
- **Secrets**: repo or environment secrets, OIDC for cloud deploys,
  `::add-mask::VALUE` for values derived from secrets.
- **Pinning**: a full commit SHA.
