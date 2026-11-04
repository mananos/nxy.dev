---
description: Repo memory — rules, learnings, search, promote, export and import (nxy dev)
argument-hint: [show | rule "<text>" --question "<q>" | note "<text>" --files a,b | promote <A7> --question "<q>" | search <text> | related <files> | export [--notes] | import <file> [--apply] | bootstrap]
allowed-tools: Bash(. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" mem:*)
---

```
!`. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd" mem $ARGUMENTS`
```

Show the block above to the user verbatim. A rule always needs the question it settles (`--question`): without it the command fails, so ask the user for it instead of inventing one. Never save a rule or a learning on your own: only when the user asked for it with this command. `import` without `--apply` only shows what would change. `bootstrap` starts the proposal of the repo's first rules: the output tells you to dispatch the `nxy:scout` agent with the prompt `nxy bootstrap` in the foreground; do exactly that and nothing else, nxy asks the user which rules to keep. Never edit nxy's database or export files yourself.
