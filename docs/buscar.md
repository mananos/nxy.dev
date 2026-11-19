# Buscar sin leer medio repo

`/nxy:locate` encuentra dónde vive algo en el repo y te devuelve un `path:line`, sin llenar tu contexto de archivos.

## Para qué sirve

Cuando Claude busca algo en el chat, lee archivos enteros y todo eso queda en el contexto, que se reenvía en cada llamada de ahí en más ([Gasto](gasto.md)). nxy manda a un **scout**: un agente aparte que busca con herramientas que no gastan tokens, confirma con dos o tres lecturas y devuelve sólo las ubicaciones. Todo lo que leyó muere con él. El planner de una feature también arranca por esta búsqueda ([Una feature, paso a paso](feature.md)).

## Qué ves

```
/nxy:locate ¿dónde se calcula el IVA?
```

Claude despacha al scout y te contesta con `path:line` y una línea de qué hay ahí, más lo que miró y descartó. También podés correr la búsqueda vos, sin modelo, desde la terminal (cero tokens, milisegundos):

![nxy locate en un repo de ejemplo](assets/gifs/cli-locate.gif)

El resultado tiene hasta tres bloques, uno por buscador:

- **repo index**: los símbolos (funciones, clases, métodos) del repo cuyo nombre coincide con tu consulta, con `archivo:línea` y la firma.
- **exact text (rg)**: dónde aparece el texto literal, si tenés `rg` instalado.
- **codegraph**: los símbolos relacionados, si tenés codegraph instalado y el repo indexado.

Cuando un buscador no está o no encontró nada, el informe lo dice (`_None of the terms appear literally anywhere in the repo._`, `_Unavailable (no-index) — answered with the index and rg instead._`) y sigue con los otros. El informe está acotado a unos 40.000 caracteres.

## Qué significa

- Nxy separa tu pregunta en **términos** (descarta palabras como «dónde», «archivo» o «where»; parte `CalcularIVA` en `Calcular` e `IVA`) y los busca en los tres lados a la vez.
- **El índice de símbolos es propio de nxy.** Se arma solo la primera vez, después se actualiza sólo con los archivos que cambiaron y entiende JS/TS (`.js`, `.mjs`, `.cjs`, `.jsx`, `.ts`, `.tsx`, `.mts`, `.cts`), Java, Scala, Python, Go y C#. Respeta lo básico de tu `.gitignore` (nombres, `carpeta/`, `/raíz`, `*.ext`) y se salta `node_modules`, `.git`, `dist`, `build`, `target`, `vendor` y archivos de más de 1 MB. Vive en `.nxy/local/index/` dentro del repo, pesa unos 100 bytes por símbolo y se borra con esa carpeta.
- **Sin resultados**: si no encuentra nada, te dice qué términos probó. Suele significar que el código usa otras palabras que el dominio («anular turno» contra `cancelBooking`): preguntá de otra forma.
- **Para qué usar el `path:line`**: directo. Si volvés a abrir los archivos enteros «para verificar», pagás lo mismo que el comando evitaba.
- En una sesión larga, el hilo principal que intenta una búsqueda amplia (`Grep` o `Glob` sin apuntar a un archivo, `rg`, `grep -r`, `find`…) con el contexto ya grande es frenado y se le pide que mande un scout: ese freno es el mismo mecanismo de este comando ([El freno](gate.md)).

## Qué hacer

Casi siempre, nada: Claude manda al scout solo. Si querés buscar vos:

```
nxy locate "dónde se calcula el IVA"
nxy locate --callers CalcularIVA
nxy locate --callees EmitirFactura
nxy locate --impact CalcularIVA
nxy locate --status
nxy locate --index
```

- `--callers X`, `--callees X` y `--impact X` piden relaciones: quién llama a X, qué llama X, qué se rompe si lo cambiás. Con codegraph instalado y el repo indexado usa sus relaciones; sin él, cae a `rg` y dice qué degradó.
- `--status` muestra qué buscadores hay (por ejemplo `repo map: 1491 files, 15824 symbols`, `ripgrep (rg): available`, `codegraph: … indexed`).
- `--index` reconstruye el índice de símbolos por completo; `--rebuild "<consulta>"` lo reconstruye y busca.
- `--json` devuelve el resultado como JSON, para scripts.
- Una consulta entre barras (`/regex/i`) se toma como expresión regular.
- Para sumar los buscadores: `rg` y codegraph se instalan con `/nxy:setup` ([Instalación](instalacion.md)). Para apagar codegraph, escribí `"scout": { "codegraph": false }` en tu `~/.nxy/config.json` (o en el `.nxy/config.json` del repo): `nxy config set` no tiene esa clave ([Configuración](configuracion.md)).

## Comandos y config

| Comando | Qué hace |
| --- | --- |
| `/nxy:locate <pregunta>` | Despacha al scout con tu pregunta tal cual |
| `nxy locate "<consulta>"` | Los tres buscadores, sin modelo |
| `nxy locate --callers\|--callees\|--impact <símbolo>` | Relaciones entre símbolos |
| `nxy locate --status` | Qué buscadores hay |
| `nxy locate --index` · `--rebuild "<consulta>"` | Reconstruir el índice |
| `nxy locate … --json` · `--cwd <carpeta>` | Salida JSON · otra carpeta |

| Clave | Qué hace |
| --- | --- |
| `scout.codegraph` | `false` desactiva codegraph (por defecto, `true`); se edita a mano en el JSON |
| `roles.scout.model` · `roles.scout.effort` | Modelo y effort del scout (por defecto sonnet, medium); `nxy config set` o la pantalla Config de la TUI |

## Relacionados

- [Una feature, paso a paso](feature.md): el planner busca con `nxy locate` antes de armar el plan.
- [Modo ingeniero](ingeniero.md): el reconocimiento del diseño lo hace un scout.
- [Memoria](memoria.md): `/nxy:mem related` busca tareas cerradas que tocaron los mismos archivos.
- [Gasto](gasto.md): por qué un contexto grande encarece cada llamada.
- [Instalación](instalacion.md): `rg` y codegraph.
