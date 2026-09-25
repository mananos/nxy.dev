---
description: Print or install the nxy statusline (tokens, cost, context, cache)
argument-hint: [--apply]
allowed-tools: Bash(node:*)
---

```
!`node "${CLAUDE_PLUGIN_ROOT}/hosts/claude-code/entries/statusline-setup.mjs" $ARGUMENTS`
```

Show the block above to the user verbatim. If it printed a snippet (no `--apply`), tell the user they can either paste it into `~/.claude/settings.json` themselves or run `/nxy:statusline --apply` to have it merged with a backup. Never edit settings yourself; `--apply` is the only path that writes, and only when the user chose it.
