# La TUI

Una consola de control que abrís en otra terminal, al lado de Claude Code: tus features, el plan, los agentes, la memoria, el gasto y la config, en vivo.

## Para qué sirve

Claude Code es un chat y se lee de arriba para abajo; cuando hay tres lotes corriendo y dos features abiertas, no se ve el conjunto. La TUI lee la misma base que nxy (`~/.nxy/nxy.db`) y la muestra ordenada: qué te necesita, qué está en curso, cuánto cuesta cada cosa. Se actualiza sola (mira la base cada medio segundo) y no necesitás tenerla abierta para que nxy funcione. Además es el lugar donde podés contestar las preguntas de nxy sin volver al chat ([Decidir desde la TUI](decidir.md)).

## Qué ves

Probala sin datos: `nxy tui --demo`.

Arriba van las cuatro secciones (**Inicio**, **Memoria**, **Gasto**, **Config**), a la derecha `● 2 DECISIONES` (las preguntas que esperan por vos) y `hoy $99.97` (lo gastado hoy). Abajo, las teclas de la pantalla en la que estás.

### Inicio

Tus features en tres grupos: **Te necesita** (hay que decidir algo), **En curso** y **Listo hoy**. Cada tarjeta dice el título, el id (`F7K2`), el tiempo, el costo, lo último que pasó, una barra de avance y la fase (`lote 3/4`, `diseño 2/4`, `review`). Un `*` junto al costo quiere decir que un modelo todavía no está en la tabla de precios y se estimó por familia ([Gasto](gasto.md)). Debajo, una línea de worktrees (`3 activos · 1 huérfano`). Si no hay ninguna feature abierta, Inicio dice cómo arrancar una: en Claude Code, `/nxy:feature <lo que querés hacer>` (o `/nxy:engineer`).

Con `enter` abrís la feature: sus lotes con el modelo, el tiempo y el costo de cada uno, y los errores del lote que falló. Si la feature está en el diseño, abre el [modo ingeniero](ingeniero.md). Desde la feature:

- `p` abre el **Plan**: el grafo de lotes, los criterios `R1…Rn`, las decisiones y el impacto de cada lote (ver [Plan y lotes](plan-y-lotes.md)). Con un plan esperando aprobación, ahí se contesta con `a`, `c` y `x` (ver [Decidir desde la TUI](decidir.md)). `enter` abre un archivo del plan con su diff. `r` vuelve a leer el archivo del disco (o recalcula el plan) si editaste algo afuera; abajo dice `Releyendo…` y avisa cuando terminó o con el error.
- `a` abre **Agentes**: el árbol del hilo principal, el planner con sus scouts y un nodo por lote, con tokens, cache, tiempo y costo. Si un lote falló, dice qué reglas recibió y si la citó.
- **Archivo**: el diff de un archivo del plan, con los archivos que lo importan. `↑↓` (o `j`/`k`) desplazan el código, `pgup`/`pgdn` una página y `home`/`end` al principio y al final. `n`/`N` llevan el cambio siguiente o anterior arriba de todo (la cabecera dice `cambio k/N`), `v` alterna diff y archivo entero (con las líneas borradas en rojo donde estaban), `i`/`I` eligen el importador, `enter` abre el marcado, `o` abre el archivo en tu editor (`$VISUAL` o `$EDITOR`; si no hay, `code` en la línea o el programa del sistema; si no hay ninguno, te dice la ruta) y `r` lo relee. Si el pie de teclas no entra en el ancho de la terminal, caen primero las del importador (`enter`, `i`/`I`); `o`, `r` y `?` quedan, y `?` muestra todas.
- `w` desde Inicio abre **Worktrees**: carpeta, rama, feature que la usa, estado (`activo`, `pausado`, `huérfano`) y si hay commits sin pushear o cambios sin commitear ([Varios repos y worktrees](varios-repos.md)).

### Memoria, Gasto y Config

- **Memoria**: tus reglas del repo, las globales y los aprendizajes, con búsqueda y edición en el lugar ([Memoria](memoria.md)).
- **Gasto**: la sesión y la tendencia, con barras; `s` cambia de sesión. En Tendencia, `←` y `→` mueven la ventana un período (7 días u 8 semanas), hasta 12 atrás; al salir vuelve a hoy ([Gasto](gasto.md)). Gasto y Tendencia cargan aparte, sin frenar el resto: con una de las dos en pantalla se recargan con cada cambio de la base; en las demás pantallas (y el hoy $ de arriba) a lo sumo cada 30 segundos. `r` o entrar a la pantalla las recarga al instante.
- **Config**: los roles con su modelo y effort, los módulos, las herramientas que nxy usa y el estado de la statusline y del binario.

En Config, arriba ves las dos capas: **usuario** (`~/.nxy/config.json`) y **repo** (`.nxy/config.json`); cuando las dos dicen algo, gana el repo. La columna **Origen** de cada rol te dice de dónde sale el valor (`default`, `usuario` o `repo`), y un cambio aplica «al próximo agente que se lance». Detalle de cada clave: [Configuración](configuracion.md).

## Qué significa

- **Te necesita** quiere decir que nxy espera algo tuyo: aprobar el plan, elegir qué hacer con un lote fallido, contestar una bifurcación del diseño.
- **Trabajando** es un agente haciendo su parte; **Verificando**, que se está corriendo un test o una review; **Listo**, que la feature cerró.
- **Huérfano** (en Worktrees) es un worktree de un repo con una feature abierta que ninguna feature de nxy tiene tomado. La pantalla lista sólo los worktrees de los repos de tus features abiertas y deja afuera el checkout principal.
- La TUI funciona fuera de un repo, pero ahí Memoria no tiene «este repo» y Config no tiene la capa del repo.
- Si actualizás nxy con la TUI abierta, arriba ves **nxy se actualizó: cerrá y volvé a abrir**; hasta que la reabras no contesta nada. Y si la base es de un nxy más nuevo que el binario, `nxy tui` se niega a abrir con «la db es de un nxy más nuevo: abrí la TUI de esa versión».

## Qué hacer

Abrila en **otra terminal**. En Claude Code, `/nxy:tui` te muestra el comando exacto (nxy no abre terminales solo). Si `nxy` está en el PATH es:

```
nxy tui
```

Si no, `nxy tui where` te da el comando con la ruta completa del lanzador, para PowerShell o para Git Bash, Linux y macOS; para que sea `nxy tui` hace falta `nxy` en el PATH ([Instalación](instalacion.md)). El `nxy` que Claude Code agrega para sus comandos no cuenta: `tui where` da la ruta del lanzador hasta que nxy esté en tu PATH. Para mirar antes de usarla: `nxy tui --demo` abre el mismo programa con datos de ejemplo, sin tocar tu repo ni tu base.

Las teclas que valen en todas las pantallas:

| Tecla | Qué hace |
| --- | --- |
| `↑` `↓` (o `k` `j`) | Mover el cursor |
| `enter` | Abrir lo que está bajo el cursor |
| `esc` | Subir un nivel; en la raíz de una sección vuelve a Inicio |
| `tab` · `shift+tab` | Cambiar de sección (en Gasto, `tab` pasa primero a Tendencia) |
| `d` | Abrir la ventana de decisión ([Decidir](decidir.md)) |
| `space` | Pausar o reanudar la feature bajo el cursor (en Inicio y en la feature) |
| `?` | Ayuda con las teclas de la pantalla |
| `ctrl+g` | Íconos nerd o unicode |
| `q` · `ctrl+c` | Salir |

Pausar y reanudar corren `nxy feature pause|resume --feature <id>` por vos. En Config: `e` cambia el modelo (o el valor de un módulo), `f` el effort, `r` guarda el valor en el repo, `i` instala una herramienta o la statusline, `c` copia el comando de actualización del binario. Por defecto un cambio se guarda en tu usuario; el repo se elige con `r`.

Si los íconos se ven como cuadrados, el tipo de letra de tu terminal no es una Nerd Font: `nxy tui --icons unicode` (o `NXY_ICONS=unicode`) usa símbolos comunes.

## Comandos y config

| Comando | Qué hace |
| --- | --- |
| `nxy tui` | Abre la TUI sobre tus datos |
| `nxy tui where` | Muestra el comando para pegar en otra terminal (`/nxy:tui` hace lo mismo) |
| `nxy tui --demo` | Datos de ejemplo, sin tocar nada |
| `nxy tui --icons auto\|nerd\|unicode` | Íconos (por defecto `auto`) |
| `nxy tui --cwd <carpeta>` | Otra carpeta en vez de `CLAUDE_PROJECT_DIR` o la actual |
| `nxy tui --dump <pantalla> [--feature <id>] [--width N] [--plain]` | Imprime una pantalla y sale, sin abrir la TUI |

`--dump` sirve para pegar una pantalla en un issue o para ver un estado sin abrir nada. Pantallas válidas: `inicio`, `worktrees`, `gasto`, `tendencia`, `tendencia.semana`, `config`, `feature`, `agentes`, `decision`, `ingeniero`, `ingeniero.hoy`, `ingeniero.claude`, `plan`, `memoria`, `memoria.aprendizaje`, `memoria.buscar`, `memoria.editar`, `archivo` y `archivo.entero`. Con un nombre que no existe te lista las válidas.

La variable `NXY_ICONS` y el resto del entorno están en [Configuración](configuracion.md).

## Relacionados

- [Decidir desde la TUI](decidir.md): contestar las preguntas de Claude sin volver al chat.
- [Modo ingeniero](ingeniero.md): la pantalla Ingeniero.
- [Varios repos y worktrees](varios-repos.md): la pantalla Worktrees.
- [Memoria](memoria.md) · [Gasto](gasto.md) · [Configuración](configuracion.md)
- [Instalación](instalacion.md): poner `nxy` en el PATH.
- [Mensajes de nxy](mensajes.md): los avisos que ves en la TUI y en el chat.
- [Cómo funciona por dentro](como-funciona.md): cómo está hecha la TUI y su contrato de pantallas.
