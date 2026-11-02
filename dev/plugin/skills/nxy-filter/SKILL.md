---
name: nxy-filter
description: When a Bash result looks filtered/truncated by nxy or rtk (footer mentioning "rtk recall", "hidden", "truncated") and you need the full output.
---

# Recovering full command output (nxy filter)

nxy rewrites Bash commands through `rtk` so you receive a compact result. When you genuinely need more than the summary:

1. **Prefer the recall hash.** If the result ends with something like `[... rtk recall <hash>]`, run `rtk recall <hash>` — it prints the complete original output from RTK's local store. Grep that output for what you need instead of reading all of it.
2. **Re-run unfiltered only as a last resort.** Prefix the same command with `NXY_RAW=1 ` (or append ` # raw`). The hook leaves such commands untouched. Do this only when a targeted `rtk recall ... | grep` cannot answer the question.
3. **Never disable the filter globally** (`/nxy:filter off`) just to see one output; that is the user's decision.
4. If the filtered result already answers the question (test counts, the failing test names, the error line), continue — do not fetch the raw output out of curiosity.
