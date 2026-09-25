---
name: base
title: Base — always
always: true
---
- **Scope**: every change serves the plan's goal and batches. Code the plan did not ask for (extra endpoints, refactors "while there", new config) is a finding.
- **Acceptance**: each batch's `Accept:` test actually exercises the behaviour it names — not a test that passes whatever the code does.
- **Reuse**: nothing re-implements a DTO, service, helper or component that already exists in the repo.
- **Conventions**: the repo's conventions listed in this packet hold, and so does the style of the surrounding code (naming, layering, error handling, comment density).
- **Tests**: new behaviour and each fixed bug have a test; edge cases the change introduces (empty, null, limits) are covered or named.
- **Design**: SOLID/KISS/DRY where it matters — one responsibility per class/function, no speculative abstraction, no copy-pasted blocks.
- **Leftovers**: no debug output, commented-out code, TODOs without an owner, or unused imports and parameters.
