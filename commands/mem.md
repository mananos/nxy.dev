---
description: Memoria del repo — reglas, aprendizajes, búsqueda, promover, exportar e importar
argument-hint: [show | rule "<text>" --question "<q>" | note "<text>" --files a,b | promote <A7> --question "<q>" | search <text> | related <files> | export [--notes] | import <file> [--apply] | bootstrap]
allowed-tools: Bash(sh "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" mem:*)
---

Request: $ARGUMENTS

Run `sh "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd" mem <subcommand and arguments>` with the Bash tool, and show its output to the user verbatim. The request may be a subcommand (`show`, `rule`, `note`, `promote`, `search`, `related`, `export`, `import`, `bootstrap`) or plain words: translate the words to the subcommand that does it (for example "buscá lo de los montos" is `search` with that text). Put every free text (the rule, the note, the search text) between single quotes, writing a `'` inside it as `'\''`; the text may have parentheses and double quotes. A rule always needs the question it settles (`--question`): without it the command fails, so ask the user for it instead of inventing one. Never save a rule or a learning on your own: only when the user asked for it with this command. `import` without `--apply` only shows what would change. `bootstrap` starts the proposal of the repo's first rules: the output tells you to dispatch the `nxy:scout` agent with the prompt `nxy bootstrap` in the foreground; do exactly that and nothing else, nxy asks the user which rules to keep. Never edit nxy's database or export files yourself.
