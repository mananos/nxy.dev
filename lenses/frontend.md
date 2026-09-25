---
name: frontend
title: Frontend components (Angular / React / Solid)
paths: **/*.component.ts, **/*.component.html, **/*.tsx, **/*.jsx, **/components/**, **/*.service.ts
content: @Component, useEffect(, useState(, createSignal(, createEffect(, subscribe(
---
- **Reuse**: an existing component, pipe, hook or service already does this (or almost) — extend it instead of adding a near-copy.
- **State**: one source of truth; no state duplicated between parent and child; derived values computed, not stored.
- **Lifecycles**: subscriptions and listeners released (Angular `takeUntilDestroyed`/`async` pipe, React effect cleanup, Solid `onCleanup`); effect dependencies complete and not causing loops.
- **Rendering**: lists keyed (`trackBy`, `key`); no heavy work in templates or on every render.
- **UX basics**: loading and error states handled; inputs labelled; nothing that only works with a mouse.
