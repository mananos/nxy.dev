---
description: Find where something lives in the repo without reading files (nxy)
argument-hint: <question: "where is it decided whether a command goes through rtk">
allowed-tools: Task
---

Dispatch the **`scout`** subagent (type `scout`) with this question, exactly as it was given:

$ARGUMENTS

Do not search yourself. Do not read files to "give it context": the scout has its own search engines and its own index, and everything it reads dies with it. Wait for its report.

When it comes back, use its `path:line` directly — do not re-verify by reading the whole files, which is exactly the cost this command exists to avoid. If the report says it found nothing, settle it with the user before exploring on your own.
