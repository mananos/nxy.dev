---
description: Detect and install rtk, ripgrep and codegraph (shows the exact command, runs only after you say yes)
argument-hint:
allowed-tools: Bash(node "${CLAUDE_PLUGIN_ROOT}/hosts/claude-code/entries/setup.mjs" status:*)
---

```
!`node "${CLAUDE_PLUGIN_ROOT}/hosts/claude-code/entries/setup.mjs" status`
```

Show the block above to the user verbatim. It only detects; nothing has been installed. Then ask which tool (rtk, rg or codegraph) to set up, if any. Only after the user explicitly says yes to a specific tool, run exactly `node "${CLAUDE_PLUGIN_ROOT}/hosts/claude-code/entries/setup.mjs" run <tool> --yes` for that one tool, on its own (never chained to another command), and show its output. If it fails because of `sudo`, show the manual command it printed so the user can paste it in a terminal. Never install anything on your own initiative.
