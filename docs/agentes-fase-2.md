# agents/ (fase 2)

Reservado para la fase 2 (scouts Haiku, agentes por fase). Un agente se define en `agents/<nombre>.md` con frontmatter:

```yaml
---
name: scout
description: ...
model: haiku      # haiku | sonnet | opus
effort: low       # low | medium | high | xhigh | max — Haiku no acepta effort; Sonnet no acepta xhigh
tools: Read, Grep, Glob
---
```

En fase 1 no hay agentes: el plugin no debe sumar descripciones de agentes al contexto de cada turno.
