---
description: Parallel features as git worktrees — list, new, pause, resume, close (nxy dev)
argument-hint: list | new --name <n> --base <branch> [--issue <n>] | new --branch <b> | pause|resume|close --path <worktree> [--force]
allowed-tools: Bash(. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" feature:*)
---

```
!`. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" feature $ARGUMENTS`
```

Show the block above to the user verbatim. It already ran what the user typed (nxy never commits or pushes). If it says a worktree has uncommitted changes and `close` needs `--force`, ask before running `. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" feature close --path <worktree> --force` yourself. Do not run git worktree commands on your own.

If the block above ends with a line that starts with "Para el modelo" (after `pause`), follow it: ask the user once, without insisting, whether there is something to leave for the next session; if they answer, run the `note save` command that line gives (the text on stdin); if they say no, do nothing.
