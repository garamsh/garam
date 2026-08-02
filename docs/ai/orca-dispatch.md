# Orca Dispatch

How the PM dispatches workers into Orca worktrees. Applies only when the environment provides the Orca CLI; skip this document otherwise. On Linux outside Orca-managed terminals the executable is `orca-ide` (bare `orca` is the GNOME screen reader); inside Orca terminals it is `orca`.

## Dispatch sequence

```bash
orca-ide worktree create --name <task-name> --no-parent \
  --agent <id> --prompt "<task brief>" --json
# → capture result.startupTerminal.handle — this is the only agent handle

orca-ide terminal wait --terminal <handle> --for tui-idle --timeout-ms 60000 --json
orca-ide terminal read --terminal <handle> --json
```

- Always pass `--agent` and `--prompt` together. An agent without a prompt starts and idles forever.
- The brief must be self-contained (task, constraints, paths) — the worker has no other context.
- **A dispatch is not done until `terminal read` shows the agent working.** "Worktree created" is not "work started."

## If the worker sits idle

1. `terminal read` — confirm whether the prompt text appears in the terminal.
2. Prompt missing or agent waiting at an empty prompt: `terminal wait --for tui-idle`, then `terminal send --terminal <handle> --text "<brief>" --enter`.
3. Never create a second terminal or worktree to "retry" — fix the delivery in the existing handle. If the handle reports `terminal_handle_stale`, reacquire with `terminal list --worktree id:<repoId>::<path>`.

## Tracking and completion

- Watch progress: `orca-ide worktree ps --json` (status, comment, agent state), or `terminal read` for detail.
- Workers update their own card: `worktree set --worktree active --comment "..." --workspace-status in-progress`.
- After the worker's PR is merged: `orca-ide worktree rm --worktree id:<repoId>::<path> --force --json`.

## Anti-patterns

- Bare `worktree create` then `terminal create --command <agent>` — leaves a fallback shell and invites lost input; use `--agent` instead.
- `terminal send` before the TUI is ready — input is lost; wait for `tui-idle` first.
- Sending to both an old and a replacement handle — one handle at a time.
- Creating a worktree per attempt when delivery failed — diagnose the existing one.
