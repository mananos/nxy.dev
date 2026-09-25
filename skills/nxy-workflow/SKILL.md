---
name: nxy-workflow
description: How to carry out a code change with nxy — when to edit directly, when to delegate to the implementer, when to plan first. Use it at the start of any task that writes code (a feature, a fix, a refactor, a multi-repo change), and when the user runs /nxy:feature.
---

# Working with nxy: the recipe by size

Decide the size first, from the request and what you already know. Do not read the codebase to find out: that is the cost this recipe avoids.

## Small — one or two files, the change is clear

Edit directly while your context is light. When nxy's write gate stops an edit, dispatch `nxy:implementer` with the `path:line`, the exact change and the command that verifies it.

## Medium — a few files, nothing left to decide

1. If you do not know where the code is, dispatch `nxy:scout` with the question: it returns `path:line`, not files.
2. Dispatch `nxy:implementer` for each coherent change, with `path:line`, the change and a verifying command.

## Large — several files, a design choice, a new endpoint/entity/component, or more than one repo

Also when the repo sets `flow.plan: "always"` in `.nxy/config.json` and the change touches a second file.

1. Dispatch `nxy:planner` with the task, and follow the `Next (main thread):` it returns, exactly:
   - **questions** → ask them as given, then dispatch the planner again with the answers;
   - **checkpoint** → show the plan and ask Approve / Change as given.
2. After Approve **you do not edit**: one `nxy:implementer` per batch, its prompt starting `Batch N — `. Batches whose `Depends:` already passed can go in parallel (several Agent calls in one message).
3. After each batch nxy adds `nxy verify: batch N ✔ / ✘ / – manual`. On ✘, dispatch that batch again with the failure, or ask the user exactly as nxy says.
4. When every batch is green nxy says to dispatch `nxy:tester` once, then `nxy:reviewer` once. The reviewer returns **checkpoint 2**: ask it as given, dispatch one implementer per finding the user picks, and `nxy:documenter` if the user picks "Update docs".
5. Close by telling the user what was done, what was verified, what was fixed and what was left.

## Always

- Keep the handoff current after each finished step: `node --disable-warning=ExperimentalWarning "${CLAUDE_PLUGIN_ROOT}/hosts/claude-code/entries/mem.mjs" handoff save`. It is what survives `/clear`.
- When nxy suggests cutting the session, say it once; the user decides. You never run `/clear`.
- If the user corrects something after a review, offer once to keep it with `review escape` (nxy gives the command).
- `mem handoff done` only when the user says the task is over.
- Never commit, push or open a PR: the user does.
