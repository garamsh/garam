## Summary

This moves today's tree into the layout ADR-0125 records, with no change
in behaviour.

| From | To |
|---|---|
| `go.mod`, `go.sum`, `cmd/`, `internal/`, `api/`, `proto/`, `migrations/`, `tests/`, `buf.yaml`, `buf.gen.yaml`, `sqlc.yaml`, `.mockery.yaml` | `platform/` |
| `web/` | `console/` |
| `scripts/` | `tooling/` |

The module path becomes `github.com/garamsh/garam/platform`.
`platform/sdk` and `cowork` keep theirs.

Closes #1659

## Changes

The script that made the move ships in the body below. It runs on a
fresh base and commits each step, so the four commits can be read and
regenerated one by one.

1. `refactor: move today's tree…`: `git mv` only. Every file is a 100%
   rename, so `git log --follow` reaches its history before the move.
2. `refactor: rewrite the module path, imports and path citations…`:
   - **Module path.** It is rewritten in every file under `platform/`
     except `platform/sdk`. `platform/go.mod`'s replace becomes
     `=> ./sdk`. `go.work` names `./cowork`, `./platform` and
     `./platform/sdk`.
   - **Path citations, outside ADR bodies.** A citation is rewritten
     where it named a path at the repository root on the base and does
     not resolve inside the citing file's module. Under that rule,
     platform's own `internal/...` in its Go comments stays as it is.
     Everything under `web/` moves with it, tracked or not
     (`.env.local`, `.next`). `pnpm --dir web` and `git -C web` become
     `console`.
   - **Hand edits, listed in the script.** These are the lines whose
     path is relative to a directory a command changes into, or that
     name a moved root file:
     - the `Makefile`'s generators now run `cd platform && …`, and
       `GEN_PATHS` and `TOOLCHAIN_PAIRS` read the platform's configs;
     - the two Go Dockerfiles copy `platform/go.mod` and download with
       `go -C platform`;
     - `ci.yml` uses `go-version-file: platform/go.mod`;
     - `dependabot.yml` names `/platform` and
       `/platform/tests/testprincipal`;
     - release.yml's web context is `console`;
     - the two shell tests build their stand-in repositories in the
       new layout;
     - two TypeScript tests read files from the root.
   - **Line width.** A longer path pushed some lines past column 72.
     Each such paragraph is rewrapped from that line on, and only
     whitespace moves. `tooling/check-line-width.sh` counts as many
     violations as on the base (below).
   - prettier re-formats the TypeScript a rewrite lengthened.
3. `chore: regenerate…`: protoc-gen-go embeds each file's `go_package`
   in its descriptor, so the two `.pb.go` files carry the new path.
4. `build: name every web package in a root pnpm workspace`: ADR-0125
   decision 3.
   - Measured: `pnpm -r list --depth -1` from the root lists
     `console`, `cowork/engine` and `cowork/web`.
   - `pnpm root` inside `console` still answers
     `console/node_modules`, because pnpm reads the nearest
     `pnpm-workspace.yaml`.
   - So each package keeps its lockfile and `allowBuilds`.

Not in this change: target names. `make web-ci` and the other `web-*`
targets now act on `console/`. Per-module Makefiles with one set of
verbs (`make -C console e2e`, `make -C cowork/web e2e`) are #1661's
work: its target paths name each module's `Makefile`. This change keeps
one whole-tree gate.

## Verification

Base `BASE_SHA`, head `HEAD_SHA`. Each run is a fresh clone as a
non-root user.

COUNTS_TABLE

- **History.**
  `git log --follow --oneline -- platform/internal/operator/operator.go`
  reaches FOLLOW_EVIDENCE.
- **Only paths change.** `git diff -M --stat BASE_SHA HEAD_SHA` shows
  RENAME_STAT. A word diff of commit 2, with comment markers ignored,
  holds no token other than a rewritten path. A sample:

  ```
  WORD_DIFF_SAMPLE
  ```

- **No document cites a path the move removed.** The checker below
  counts citations that name no path, outside ADR bodies, resolving each
  from the root, from the citing file's module, or from a `cd X &&`
  on its line. On the base: `stale: 64 of 1126 citations name no path`.
  On the head: `stale: 64 of 1161 citations name no path`. The two
  lists name the same 64 tokens, every one of them stale before the
  move. The checker's source is in the details block below.
- **Line width.** `tooling/check-line-width.sh`:
  - base: 48 document violations, 145 code violations;
  - head: 48 document violations, 144 code violations.
- MORE_RUNS

## Conventions consulted

- docs/convention/README.md
- docs/convention/stack-go.md (§Monorepo envelope)
- docs/convention/ci.md
- docs/convention/git.md
- docs/convention/documentation.md
- docs/convention/code-comments.md
- docs/convention/simplicity.md
- docs/architecture/README.md (rule 2: ADR bodies are frozen)

## Defaults not taken

N/A

## Restructure rejected

- **Splitting the root `Makefile` per module here.** It would make the
  move's diff more than paths, which the issue's second criterion
  rules out. It is #1661.
- **One root pnpm lockfile.** That changes how each image installs, so
  it is a behaviour change for #1660, where `design/` first needs a
  shared workspace.

## Convention concerns

An unnamed case, settled by the nearest rule: how a path citation inside
a module reads once the repository holds several modules. A Go comment
in `platform/` citing `internal/cli` reads from its module root, as
`stack-go.md` §Monorepo envelope has every rule hold "with the folder
standing for the module root". Documents at the root cite
`platform/internal/cli`. The PM may want this stated in
`documentation.md`.

<details><summary>The move script and the citation checker</summary>

SCRIPTS

</details>

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_013WL3A4jtjKu2LEQw5HZwMn
