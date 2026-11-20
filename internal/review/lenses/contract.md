---
name: contract
title: Contract between repos
repos: 2
---
- **What each repo exposes**: for every route and method, request and response fields and types, status codes, event names and payloads, shared schemas (DTO, OpenAPI, proto) and config keys the diff adds or changes, find who consumes it in the other repo. The diff labels files as `repo/path`; for the consumer side outside the diff, read it through the table of repos.
- **Compare both sides**: names, types, nullability, letter case, enum values, required vs optional, and version. A mismatch of any of them is a bug at runtime, not at compile time.
- **One side changed alone**: if a repo changed what it exposes and the other repo that uses it did not change (or the reverse), that is a finding. Cite both places as `repo/path:line`.
- **Order of rollout**: if the two sides cannot go live at the same moment, the old consumer must still work with the new producer (or the change says otherwise).
