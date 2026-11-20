---
description: Features en paralelo como worktrees de git — list, new, pause, resume, close
argument-hint: list | new --name <n> --base <branch> [--issue <n>] | new --branch <b> | pause|resume --path <worktree> | pause|resume --feature <id> | close --path <worktree> [--force] | close [--feature <id>] [--force]
allowed-tools: Bash(. "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" feature:*)
---

```
!`. "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" feature $ARGUMENTS`
```

Show the block above to the user verbatim. It already ran what the user typed (nxy never commits or pushes). If it says a worktree has uncommitted changes and `close` needs `--force`, ask before running `. "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" feature close --path <worktree> --force` yourself. Do not run git worktree commands on your own.

`pause --feature <id>` pauses a whole feature and frees its checkouts for another one; `resume --feature <id>` takes them back (it says which feature holds a checkout if one is busy). `close` without `--path`, or with `--feature <id>`, closes every worktree of a finished or paused feature; it refuses an active one, and a feature that worked in the session's folder has no worktrees. `list` also shows the active and paused features with their repos and branches.

If the block above ends with a line that starts with "Para el modelo" (after `pause`), follow it: ask the user once, without insisting, whether there is something to leave for the next session; if they answer, run the `note save` command that line gives (the text on stdin); if they say no, do nothing.
