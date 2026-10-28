# The #1659 move, as a script

Not for merging: this branch only carries the tool that produces
`claude/move-1659`. Run it on a fresh base, never rebase its output.

```sh
git checkout -B claude/move-1659 origin/dev
git clean -fdx                       # an ignored console/ would nest web/ inside it
MOVE_COMMIT=1 MOVE_TRAILER="$(printf 'Co-Authored-By: …\nClaude-Session: …')" \
  python3 /path/to/move-1659/move.py origin/dev
```

- `move.py` — the steps, one commit each: git mv; module path,
  imports and path citations (with `move_edits.py`'s hand edits, the
  line-width rewrap and prettier); `make gen`; the root
  `pnpm-workspace.yaml`. Each hand edit must match exactly once, or
  already be written by the sweep; the run lists every miss at once.
- `move_edits.py` — the edits the citation sweep cannot read: lines
  relative to a `cd`, root files that moved (`go.mod`), build contexts,
  string literals in tests.
- `move/added.py`, `move/reflow.py` — the line-width pass: violations
  `tooling/check-line-width.sh` reports on lines the move added, plus
  JSDoc lines it does not reach, rewrapped from that line to the
  paragraph's end; only whitespace moves.
- `move/stale.py` — the acceptance check: citations outside ADR bodies
  that name no path. Run on base and head; the lists must match.
- `move/classify.py` — diagnostics: each citation as module-relative,
  root (moved) or neither.
