---
description: Filtro de salida de nxy — status, on, off
argument-hint: status | on | off [--repo]
allowed-tools: Bash(. "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" filter:*)
---

nxy filter control. The current state is:

```
!`. "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" filter $ARGUMENTS`
```

Show the block above to the user verbatim. If it contains install hints, repeat the exact commands. Do not install anything yourself and do not edit `~/.claude/settings.json`. If the user asked for `on` or `off`, the block already reports the new state. `on`/`off` change your user config (`~/.nxy/config.json`); with `--repo` they change this repo's (`.nxy/config.json`).
