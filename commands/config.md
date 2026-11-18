---
description: Mirá o cambiá la config de nxy — filtro, motor, modelo y esfuerzo por rol, TTL del prompt cache
argument-hint: [show | set <key> <value|default> [--repo] | cache-ttl 1h|off]
allowed-tools: Bash(. "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" config:*)
---

```
!`. "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" config $ARGUMENTS`
```

Show the block above to the user verbatim. If it lists the keys, the user can change one with `/nxy:config set <key> <value>` (`--repo` writes this repo's `.nxy/config.json`, otherwise `~/.nxy/config.json`; `default` removes the key). Never edit nxy's config files or `~/.claude/settings.json` yourself.
