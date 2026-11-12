---
description: Detect and install rtk, ripgrep, codegraph and d2 — shows the exact command, runs only after you say yes (nxy dev)
allowed-tools: Bash(. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" setup status:*)
---

```
!`. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" setup status`
```

Show the block above to the user verbatim. It only detects; nothing has been installed. Then ask which tool (rtk, rg, codegraph or d2) to set up, if any. Only after the user explicitly says yes to a specific tool, run exactly `. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" setup run <tool> --yes` for that one tool, on its own (never chained to another command), and show its output. If it fails because of `sudo`, show the manual command it printed so the user can paste it in a terminal. Never install anything on your own initiative.
