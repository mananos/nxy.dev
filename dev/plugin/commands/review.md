---
description: Review — mirá la última review, las stats de los lentes o reportá un problema que se le escapó (nxy dev)
argument-hint: [show | status | escape "<text>" --question "<q>" [--lens base]]
allowed-tools: Bash(. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" review:*)
---

```
!`. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" review $ARGUMENTS`
```

Show the block above to the user verbatim. `escape` always needs the question the rule settles (`--question`): without it the command fails, so ask the user for it instead of inventing one. Never report an escape on your own: only when the user asked for it with this command. With an active feature the escape is only a candidate for the closing question; without one it is saved as a repo rule right away.
