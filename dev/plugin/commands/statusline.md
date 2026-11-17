---
description: Mirá, instalá o sacá la statusline de nxy (tokens, costo, contexto, cache) (nxy dev)
argument-hint: [install | remove]
allowed-tools: Bash(. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" statusline setup:*)
---

```
!`. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" statusline setup $ARGUMENTS`
```

Show the block above to the user verbatim. Without arguments it only shows the snippet and the current state: tell the user they can run `/nxy:statusline install` (merges it into `~/.claude/settings.json` with a backup) or paste it themselves. Never edit settings yourself; `install` and `remove` are the only paths that write, and only when the user typed them.
