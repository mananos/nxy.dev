---
description: Gate de nxy — status, once [búsqueda], reads on|off
argument-hint: status | once [search] | reads on|off [--repo]
allowed-tools: Bash(sh "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" gate:*)
---

nxy gate control. The current state is:

```
!`sh "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" gate $ARGUMENTS`
```

Show the block above to the user verbatim. `once` lets the next main-thread edit of a code file through (`once search`: the next broad search); it is single use and expires. `reads on|off` changes the user config (`~/.nxy/config.json`); add `--repo` for this repo's `.nxy/config.json`. Do not edit any config yourself.
