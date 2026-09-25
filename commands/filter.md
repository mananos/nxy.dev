---
description: nxy output filter — status, on, off
argument-hint: status | on | off
allowed-tools: Bash(node:*)
---

nxy filter control. The current state is:

```
!`node "${CLAUDE_PLUGIN_ROOT}/hosts/claude-code/entries/filter.mjs" $ARGUMENTS`
```

Show the block above to the user verbatim. If it contains install hints, repeat the exact commands. Do not install anything yourself and do not edit `~/.claude/settings.json`. If the user asked for `on` or `off`, the block already reports the new state.
