<p align="center">
  <img src="resources/nxydev.png" alt="nxy.dev" width="320">
</p>

<p align="center">Plugin de Claude Code para gastar menos tokens sin perder calidad, y para ver en qué se va cada dólar.</p>

nxy hace cuatro cosas:

- **Te muestra lo que gastás.** Una statusline con el costo del turno y de la sesión, y reportes por sesión, modelo, subagente y día. Los números salen del `usage` real que Claude Code guarda en sus transcripts: no son estimaciones.
- **Achica lo que Claude lee.** Las salidas de tests, builds y `git` pasan filtradas por [RTK](https://github.com/rtk-ai/rtk). Cuando la sesión ya está cargada, las búsquedas y las ediciones las hacen subagentes baratos que se descartan al terminar.
- **Le pone método a los cambios grandes.** Primero un plan que aprobás vos, después cada lote verificado contra un test, una review al final y los arreglos que vos elijas.
- **Recuerda.** Las decisiones y las convenciones del repo, y el punto exacto de cada tarea (el *handoff*), sobreviven al `/clear` y se pueden compartir por git.

Funciona con cualquier lenguaje, en Windows, Linux/WSL y macOS, con Node 22.13+ y sin dependencias.

## Contenido

- [Instalación](#instalación)
- [Cómo se usa](#cómo-se-usa)
- [Mensajes de nxy y qué hacer con cada uno](#mensajes-de-nxy-y-qué-hacer-con-cada-uno)
- [Comandos](#comandos)
- [La statusline](#la-statusline)
- [Ver en qué se gasta](#ver-en-qué-se-gasta)
- [La memoria](#la-memoria)
- [Configuración](#configuración)
- [Si algo no anda](#si-algo-no-anda)
- [Qué le agrega nxy a tu sesión](#qué-le-agrega-nxy-a-tu-sesión)
- [Estado del proyecto](#estado-del-proyecto)
- [Desarrollo](#desarrollo)

Cómo funciona cada pieza por dentro: [`docs/como-funciona.md`](docs/como-funciona.md). Todas las opciones: [`docs/configuracion.md`](docs/configuracion.md).

## Instalación

### 1. Requisitos

Lo único obligatorio es **Node 22.13 o más nuevo** en el PATH (`node -v`). Con eso ya funcionan las métricas, la statusline, la memoria, el plan y la review. (La memoria, el handoff y el plan usan la base SQLite que trae Node desde 22.13; con una versión anterior esas partes no hacen nada, sin romper el resto.)

Opcionales, que suman ahorro:

| Herramienta | Para qué | Sin ella |
| --- | --- | --- |
| [rtk](https://github.com/rtk-ai/rtk) | Filtra la salida de los comandos (tests, builds, git, logs) | Los comandos pasan enteros; nada se rompe |
| [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) | Búsqueda de texto del scout; rtk también lo usa | El scout busca más lento y rtk recorta menos |
| [codegraph](https://github.com/colbymchenry/codegraph) | "Quién llama a qué" para el scout | El scout usa el índice propio de nxy y `rg` |

```
# Windows
winget install rtk-ai.rtk
winget install BurntSushi.ripgrep.MSVC

# Linux / WSL (Debian/Ubuntu)
sudo apt install ripgrep
curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh

# macOS
brew install ripgrep rtk
```

En Linux, rtk queda en `~/.local/bin`; si no está en el PATH, nxy igual lo encuentra y te lo avisa. Verificá con `rtk --version` y `rg --version`.

codegraph (opcional) se instala así, y después se inicializa una vez en cada repo donde lo quieras:

```
# Windows (PowerShell)
irm https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.ps1 | iex

# Linux / WSL / macOS
curl -fsSL https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.sh | sh

# en la raíz de cada repo
codegraph init
```

Si preferís no pegar comandos, `/nxy:setup` (o los botones de Config › Herramientas) detecta qué te falta, te muestra el comando exacto para tu sistema y lo corre sólo si le decís que sí.

> **No corras `rtk init -g` ni `codegraph install`.** nxy ya instala su propio hook y usa codegraph por CLI desde adentro de un subagente. Si los conectás por su lado, rtk y nxy se pisan, y codegraph le suma su descripción a *cada* turno de Claude.

### 2. El plugin

Desde GitHub (el repo es a la vez un marketplace de Claude Code):

```
/plugin marketplace add mananos/nxy.dev
/plugin install nxy@nxy-dev
```

Desde un clon local (para probar una rama que todavía no está publicada):

```
/plugin marketplace add /ruta/al/clon/nxy.dev
/plugin install nxy@nxy-dev
```

o sin instalar, sólo para esa sesión: `claude --plugin-dir /ruta/al/clon/nxy.dev`.

Queda instalado para tu usuario en todos los proyectos. `/plugin` lo lista y lo deshabilita. Para actualizar: `claude plugin marketplace update nxy-dev` y reinstalar.

### 3. La statusline (recomendado)

```
/nxy:statusline --apply
```

La escribe en `~/.claude/settings.json` (con backup) y se actualiza sola con cada versión nueva del plugin. Reiniciá Claude Code para verla.

### 4. Verificá

| Comando | Tiene que decir |
| --- | --- |
| `/nxy:filter status` | `engine: rtk` (o por qué está apagado y cómo instalarlo) |
| `/nxy:locate --status` | Qué buscadores tenés: índice, `rg`, codegraph |
| `/nxy:mem status` | Dónde está la base y cómo identificó este proyecto |
| `/nxy:setup` | Por herramienta (rtk, rg, codegraph): versión, si está en el PATH, si hay un hook que choca, y el comando exacto para instalar o reinstalar; pregunta antes de correr |

## Cómo se usa

### Lo que pasa solo

No tenés que aprender nada para aprovechar la mayor parte. Con nxy instalado:

- **Los comandos salen filtrados.** Un `./mvnw test` que devolvía 84 líneas le llega a Claude en 8. Si Claude necesita la salida completa, sabe pedirla.
- **La statusline te avisa cuándo cortar.** Naranja o rojo en `turno` o `ctx` quiere decir que cada mensaje ya cuesta caro (ver [La statusline](#la-statusline)).
- **Con la sesión cargada, Claude delega.** Pasados los 100k tokens de contexto, Claude deja de editar él mismo y le pasa el cambio a un subagente (el *implementer*), que lee lo justo, edita, corre el test y se descarta. Así los archivos no se quedan en el contexto caro de la sesión principal.
- **Aparecen tus decisiones.** Si guardaste una decisión que tiene que ver con lo que pedís, Claude recibe un puntero (una línea) y la carga si le sirve.
- **Claude sabe dónde quedaste.** Al abrir una sesión en una rama con trabajo a medias, Claude recibe una línea con el *handoff* de esa rama.

### Según el tamaño de la tarea

| Tarea | Qué hacés | Qué pasa |
| --- | --- | --- |
| **Chica**: uno o dos archivos, está claro qué cambiar | La pedís como siempre | Claude edita directo mientras la sesión esté liviana |
| **Mediana**: algunos archivos, nada que decidir | La pedís como siempre. Si no sabés dónde está el código: `/nxy:locate <pregunta>` | Un scout (Haiku) encuentra el `path:line` y un implementer por cambio escribe |
| **Grande**: varios archivos, una decisión de diseño, algo nuevo o más de un repo | `/nxy:feature <pedido>` | Plan → tu aprobación → lotes verificados → suite → review → arreglos elegidos |

Si no estás seguro, usá `/nxy:feature`: Claude arranca diciendo qué tamaño le asignó y por qué, y sigue la receta de ese tamaño.

### Una tarea grande, paso a paso

```
/nxy:feature alta de clientes corporativos con validación de CUIT
```

1. **Claude decide el tamaño** y, si es grande, despacha al *planner* (Sonnet, effort high), que busca en el repo, reutiliza lo que existe y arma un plan por lotes, cada uno con su comando de verificación (`Accept:`).
2. **Preguntas (a veces).** Si el plan depende de algo que sólo vos sabés ("¿endpoint nuevo o extender el existente?"), te lo pregunta con opciones (`Plan Q1`, `Plan Q2`…). La primera es la recomendada.
3. **Aprobás el plan.** Claude te lo muestra y te pregunta **Approve** o **Change**. Hasta que elijas Approve no se escribe una línea de código. Si hubo preguntas, también te ofrece guardar tus respuestas como **convenciones del repo**, para que la próxima vez no pregunte.
4. **Lotes verificados.** Un implementer por lote. Después de cada uno ves `nxy verify: batch 1 ✔` o `✘`. Con un ✘ elegís **Retry**, **Continue anyway** o **Stop**. Los lotes que no dependen entre sí (un repo de back y uno de front) corren en paralelo. Si un agente corre en segundo plano, al lanzarlo sólo ves "lanzado en segundo plano": el veredicto llega **cuando el agente termina**, no al lanzarlo, y hasta entonces nxy no deja despachar lo que depende de él. En segundo plano no hay vuelta atrás: si el lote no quedó probado ves un ✘ con el motivo y el paso siguiente. Si no es un ✘, no tenés que hacer nada: esperá el aviso.
5. **Suite completa.** Con todo en verde, un *tester* (Haiku) corre una vez la suite de cada repo tocado y te dice sólo lo que falló. Si la suite sale en rojo, un único implementer la arregla en un lugar propio (`Suite fix — <qué falló>`); nxy lo verifica volviendo a correr los comandos que fallaron y recién después pasa a la review. La review no arranca mientras la suite sigue corriendo.
6. **Review.** Un *reviewer* (Sonnet) mira únicamente lo que cambió el plan, con lentes elegidas por el tipo de archivo (persistencia, API, frontend, base de datos, seguridad). Ves los hallazgos numerados (`R1 high …`) y **elegís cuáles se arreglan**. Si el reviewer vuelve sin registrar la review, nxy lo manda de vuelta una vez (si corre en primer plano) y, si falla otra vez o corre en segundo plano, te lo muestra como una falla. Para leer el cambio, el reviewer lo recibe como un archivo (`.nxy/local/baseline/<rama>/review-packet.md`, unos 130 KB para un diff de 2.000 líneas; se reemplaza en cada review y se borra con `mem handoff done`) y lo lee por partes; vos no tenés que abrirlo (`review.mjs packet --full` lo imprime entero si lo querés ver). Un archivo marcado `(git)` lo cambió el plan sin que nxy tuviera una copia: se compara contra HEAD, en modo lectura (nxy nunca escribe dentro de `.git`). La sección `## Not reviewed` nombra los archivos que ya tenías modificados antes de aprobar y no tienen copia: nxy no puede separar tu parte de la del plan, así que los nombra en vez de adivinar. Si te importan, mirá ese diff vos. Si algún documento para usuarios (README, un `.md` en la raíz, o lo que esté en `docs/`, `doc/`, `documentation/` o `wiki/`) nombra lo que cambió, te ofrece **Update docs**, con los 3 primeros y "+N más"; los prompts de agentes y skills nunca se ofrecen.
7. **Cierre.** Claude te resume qué hizo, qué se verificó y qué quedó. Vos mirás el diff, commiteás y abrís el PR: **nxy nunca commitea ni pushea**.
8. **Cuando la rama se mergea**, decile a Claude "cerrá el handoff" (`mem handoff done`): deja de aparecer al abrir sesiones, pero sigue buscable.

### Con Mods: el orquestador corre el plan

Si tu Claude Code soporta Mods (verificado en 2.1.292), nxy puede correr el plan aprobado sin pasar por la conversación. Viene prendido por defecto.

- **Qué ves.** El panel `nxy` se abre solo al empezar la sesión en terminales anchas (desde 144 columnas; 110 si ya lo abriste antes y no lo cerraste a mano). En una terminal angosta queda sin dibujar hasta que ensanchás la terminal o escribís `/nxy-panel` (se abre a cualquier ancho). Se adapta al ancho: en una terminal angosta las filas pasan a dos líneas (la etiqueta arriba, el valor abajo) y la barra se acorta. Abre ya lleno: lee el plan en el mismo proceso que limpia el inicio de sesión, así que no hay que apretar nada para ver el estado. Si nxy toma un plan recién aprobado, abre en la pestaña Plan; si no, en Inicio. Un plan que ya venía en curso sigue por el camino de siempre.
- **El encabezado.** `nxy` y, a la derecha, el estado: `● ready` (nada corriendo, todo en orden), `▶ batch 2/5` (corre el lote 2 de 5), `▶ tester` o `▶ review` (corre ese paso), `⏸ paused after batch N` (pausa entre tandas, te espera) y `✘ batch N needs you` (un lote falló y hay que decidir). Arriba están las siete pestañas: **Inicio** (`1`), **Plan** (`2`), **Agentes** (`3`), **Stats** (`4`), **Config** (`5`), **Memoria** (`6`) y **Features** (`7`). La misma línea de estado, sin el `nxy`, queda fija bajo el prompt (`● ready`, `▶ batch 2/5`, etc.); Claude Code la muestra junto al nombre del plugin.
- **Inicio.** Arriba, «Lote N de M» con su título, cuánto lleva y el ciclo (plan › aprobado › lotes › tester › review › cierre: ✔ hecho, ◉ en curso, ○ pendiente, ✘ en rojo); debajo, el hash del plan, si está aprobado y el handoff (`hace N min`; si dice `desactualizado` hubo lotes después de guardarlo: guardalo antes de cerrar). Sin plan dice «Sin plan en esta rama». Después, tres tarjetas. **Contexto**: una barra contra el umbral del gate (ej. `46k / 100k`, con `gate on` y `54k libres`); verde bajo el 70 %, amarillo hasta el 90 %, rojo después: si está en rojo, guardá el handoff o usá Gate once; con el gate apagado la barra es contra la ventana del modelo y dice `gate off`. **Costo**: lo gastado en la sesión y el modelo. **Cache**: el hit % del último turno del hilo principal (cuánto de lo enviado ya estaba en cache y se pagó a un décimo), con una barra, `vence en 3m 05s` (la cuenta regresiva baja de a un segundo mientras estás en Inicio) y `en frío: $3.86`, lo que costaría reescribirla. Cuando venció dice `❄ fría` y debajo `el próximo mensaje reescribe 386k ≈ $3.86`: ese es el costo de seguir en esta sesión después de la pausa. Un `*` pegado al monto significa que el precio del modelo se estimó por familia (no está en la tabla). Dice `desconocido` hasta el primer turno de la sesión; no es un error. Bajo las tarjetas hay una franja de **segmentos** con lo mismo que la statusline (modelo · effort, ventana, contexto, turno, sesión, cache y vida, y las ventanas 5h/7d); el que no tiene dato se omite, nunca se inventa. «Ahora» lista los agentes que están corriendo (rol, lote, qué hacen y desde cuándo; es la misma fuente que la pestaña Agentes) o `nada corriendo`.
- **Plan.** Muestra el objetivo y un renglón por lote con su título completo y ✔ (hecho), ● (corriendo), ○ (pendiente) o ✘ (falló). Cada lote es desplegable (clic en su título, o con el teclado: Tab o las flechas hasta el título y Enter): al abrirlo ves los archivos que toca, el comando de aceptación y el veredicto (verificado por nxy, trabajando, error o esperando al lote anterior). El que corre viene abierto. Siguen **Suite completa** y **Review**. En el checkpoint 2 aparecen los hallazgos de la review (id, severidad, `archivo:línea`, causa): marcá los que querés arreglar (clic en la casilla ◻, o Tab/flechas hasta ella y Enter) y apretá **Arreglar N** (`z`); nxy le pide a Claude arreglar solo esos. Si no marcaste ninguno, el botón no aparece. Sin plan dice «Sin plan en esta rama». Si hay una pregunta (reintentar, seguir, pausa) aparece arriba, en un recuadro, en todas las pestañas.
- **Stats.** (`4`) La consola de costo, dibujada. De arriba abajo: **Esta sesión** (costo, tokens de entrada → salida, cache hit y cuántas veces se enfrió, llamadas y agentes); **Costo por rol** (el principal y cada tipo de agente, con barras, de más a menos caro) y **Por modelo**; **Si la cache durara 1 h** (cuánto habrías ahorrado con una cache de 1 hora y cuánto del gasto era evitable); las ventanas **5h** y **7d** de tu suscripción (sólo aparecen con suscripción; sin ella dice «sin suscripción, no hay datos»); y la **Tendencia**, con selectores de métrica (`$`, tokens, ctx, rtk), rango (7 o 30 días), agrupación (por día, por semana, por modelo) y **sólo este repo**. Abajo, una línea de estado: `actualizado hace 12s · r actualiza`, `cargando…` o el error. Qué hacer: si «con cache 1 h» dice que conviene, poné `"promptCacheTtl": "1h"` en `~/.claude/settings.json` (Claude Code 2.1.242 o más nuevo); si la porción evitable es alta, tus pausas o subagentes largos están enfriando la cache. Al cerrar un feature (con **Terminar plan** o con `handoff done`) aparece arriba su **resumen**: costo contra el de la sesión (%), tokens, cache, costo por rol y tiempo por lote (también en flujo manual; un lote despachado con una versión anterior muestra «—»). Se oculta con su botón **Ocultar** y avisa con un toast cuando está listo.
- **Avisos de cache.** Dos toasts, sin que hagas nada: «La cache vence en 1 min: …» (un minuto antes de que venza, con lo que costaría reescribirla) y otro cuando venció; y al cambiar de modelo con la cache tibia, «Cambiar a <modelo> reescribe la cache: ≈ $X» (sólo si son $0.01 o más). Se cancelan solos si mandás otro mensaje antes. La vida es una estimación: hora de la última respuesta del hilo principal + TTL; el TTL se lee de la transcripción (si tu cuenta escribe 5 min o 1 h) y, hasta que haya un dato, de `promptCacheTtl` o se infiere. Hasta el primer snapshot de la sesión (una pregunta del plan o abrir Stats) el aviso puede adelantarse o atrasarse.
- **Costo de Stats.** Un node al abrir o actualizar (`r`) la pestaña (~0,3 s por 6 MB de transcripción), ~1 s la tendencia de 30 días (se guarda por combinación: cambiar de métrica no corre nada), ~0,5 s el resumen una vez por plan cerrado, y cero node por turno (los avisos son dos temporizadores del Mod).
- **Agentes.** (`3`) Todos los agentes de la sesión, los de nxy y los que lanza Claude Code por su cuenta. Qué ves: una fila por agente con su rol, `modelo · effort` en gris (el effort sólo existe para los roles de nxy), qué está haciendo (`Edit core/panel.mjs`, `Bash: npm test`; mientras no hay una herramienta visible dice `trabajando… 12s`), el estado (spinner giratorio = corre, ✔ terminó, ✘ falló, ■ detenido o interrumpido; en el modo texto el que corre es ●), el tiempo y el costo. El costo dice `…` hasta que el agente termina su turno (se sabe recién ahí) y `~` pegado al monto significa que el precio se estimó. Arriba, la cuenta «N corriendo · N terminaron · N fallaron», y «· N detenidos» cuando hay alguno; se muestran 10 filas y «Ver todos (+N)» abre el resto. Qué significa: la animación (el spinner) sólo corre mientras algún agente trabaja; y el agente que abrís en la lista de tareas de Claude Code aparece también en el panel como una tarjeta propia. Qué hacer: elegí un agente con Tab o las flechas y Enter, o con un clic; ves su detalle (prompt, herramientas, resultado o error, lote del plan). Para mandarle un mensaje escribí en el campo y apretá Enter; si el agente ya había terminado, lo retoma con ese mensaje. El mensaje queda `en cola` y pasa a `recibido` cuando el agente hace lo siguiente, o a `no entregado: motivo` si no llegó. Mientras escribís los números no cambian de pestaña: apretá Esc para salir del campo. Si el agente es un implementer de un plan que corre el orquestador, nxy pregunta «Hablarle al implementer del lote N puede romper el lote. ¿Mandar igual?» (`y` manda, `n` descarta). `b` vuelve a la lista. Costo: cero node por turno; una consulta de la lista cada 2 s sólo con el panel abierto y algo corriendo; si Claude Code no avisa qué herramienta usa un agente, una lectura de su sesión como mucho cada 2 s por agente vivo sin actividad visible; memoria: ~140 KB como tope (40 agentes, los terminados más viejos se descartan primero).
- **Config.** (`5`) Cambiás la configuración de nxy desde el panel, sin editar archivos. Qué ves: arriba **Guardar en** con **Usuario** (activo por defecto) y **Repo**, y debajo la ruta donde se escribe. Usuario guarda en `~/.nxy/config.json` y vale para todos tus repos; Repo guarda en `.nxy/config.json`, que se commitea y lo hereda el equipo. Si el repo ya define esa clave y guardás en Usuario, el cambio no se nota y el panel avisa **el repo manda** (gana el repo); pasá a Repo para cambiarla ahí. Secciones: **Roles** (una fila por rol: planner, implementer, tester, reviewer, documenter, scout y librarian, con el modelo y el effort), **Flujo** (orquestador auto/off y pausa entre lotes), **Gate** (on/off y umbral), **Cache** (el TTL observado y su fuente, el valor de tu `settings.json`, el «si la cache durara 1 h» de Stats al lado y el botón), **Interfaz** (filtro on/off y panel auto/off), **Statusline** (si es la de nxy, otra o ninguna), **Herramientas** (rtk, rg y codegraph: una fila por herramienta con su estado, por ejemplo `encontrada 0.48.0` o `no encontrada`, la ruta en la ayuda de la fila y un botón **Instalar** o **Reinstalar**) y **nxy** (versión instalada). Qué hace cada ‹ ›: cicla al valor anterior o siguiente (modelo: haiku, sonnet, opus, fable; effort: low, medium, high o predeterminado, que quita la clave; el umbral del gate ofrece valores ya elegidos: 50k, 75k, 100k, 150k, 200k y 300k). El orquestador no se puede apagar mientras corre un plan (para no dejar su marca huérfana). Qué hacer con los botones, que piden confirmación: **cache 1 h** (`t`) escribe `promptCacheTtl: "1h"` en `~/.claude/settings.json` (o lo quita; Claude Code lo toma en la próxima sesión); **Statusline** (`l`) instala la de nxy o la quita, y sólo quita la de nxy: si tenés otra, se niega y lo dice; **Actualizar nxy** (`u`) corre `claude plugin update nxy@nxy-dev` y después hay que reiniciar Claude Code (sólo se conoce la versión instalada, no se consulta la red). **Instalar / Reinstalar** de Herramientas (`i` rtk, `p` rg, `k` codegraph): antes de correr nada, la confirmación te muestra el comando exacto de tu sistema operativo y de dónde sale; recién con el sí corre, y al terminar con éxito la fila se actualiza sola, sin apretar `r`. Si la herramienta aparece en ámbar es que corriste `rtk init -g` o `codegraph install` por tu cuenta y choca con nxy, y el texto dice cuál: `choca: hook de rtk` (el hook de rtk pisa al del filtro: **Reinstalar** corre `rtk init -g --uninstall` si el hook es global; si está en el proyecto te deja una nota para sacarlo a mano), `choca: servidor MCP de codegraph` (**Reinstalar** corre `claude mcp remove codegraph -s <ámbito donde se detectó>` y después actualiza) o `choca: hook en settings` (un hook de tu `settings.json` nombra codegraph: sólo es un aviso, nxy no edita settings ajenos y te lo lista para que lo saques vos). Nunca corre solo ni en SessionStart. En Linux/WSL los pasos que piden permisos usan `sudo -n`: si sudo pide contraseña el paso se corta (el panel no tiene terminal) y te muestra el mismo comando sin `-n` para pegarlo en una terminal. Costo: rtk 0.48.0 pesa 9,2 MB y ripgrep 15.2.0 4,1 MB (winget); codegraph no medido. Abrir Config corre 1 node para las herramientas y cada instalación 1 más; detectar el gestor de paquetes lanza hasta 4 sondas cortas `--version` (winget en Windows; apt-get, dnf, pacman y brew en Linux, y se frena en el primero que existe). Costo del resto: cada cambio escribe ~1 KB y corre un node; abrir Config la primera vez corre 2 (herramientas y stats, para el what-if); cero node por turno. La escritura es atómica (archivo temporal y renombrado). Cache 1 h y quitar la statusline guardan un backup `settings.json.bak-nxy-<fecha>` y se conservan los 3 más nuevos de ese tipo (los más viejos se borran solos; los backups de `/nxy:statusline --apply` y de instalar la statusline no se tocan nunca). El shim de la statusline pesa ~3 KB. Si un archivo de configuración está roto, nxy no lo pisa: lo dice en el panel y no escribe nada.
- **Memoria.** (`6`; las teclas 1-5 no cambian) Qué ves, de arriba abajo: un campo de **búsqueda** de memoria con las notas encontradas (id, tipo, título), y **Recientes** cuando está vacío; el **handoff de la rama** (estado y cuerpo, máximo 20 líneas, con su botón); y **Locate**, un campo donde escribís una pregunta y ves archivos `ruta:línea` con su símbolo. Qué significa: la búsqueda es léxica, por las palabras que usaría la nota, no por significado (para eso está el librarian). Locate no usa modelo: 0 tokens, milisegundos; si no hay resultados lo dice. Qué hacer: escribí y Enter; elegí una nota (clic, o Tab/flechas y Enter) para abrirla entera con sus relaciones y archivos, `b` vuelve a la lista; **Recientes** vuelve a las últimas 10; `r` recarga. Mirar una nota desde el panel no cuenta como recuerdo: no mueve los números de recall. **Preguntarle al scout** (debajo de los resultados de locate) lanza un scout con esa misma pregunta: es una corrida de Haiku y su costo aparece en la fila de Agentes cuando termina. Costo: cada búsqueda, locate o vista corre un node (~100-300 ms) y no escribe nada, salvo una fila de ~150 bytes en `metrics/locate.jsonl` por cada locate; la primera visita crea el archivo SQLite de memoria si no existía, y el primer locate de un repo grande arma el mapa del repo (el `ms` del panel lo muestra).
- **Features.** (`7`, la última pestaña) Para trabajar dos features a la vez: una sesión de Claude por `git worktree`, cada una con su propio plan y handoff (`.nxy/local` es por worktree). El panel de cualquiera de las sesiones ve y maneja todos. Qué ves: la lista **Worktrees**, una fila por worktree con su rama y etiquetas: **principal** (el checkout original), **esta sesión** (el de la terminal donde estás), **pausado hace X** y **plan corriendo** (otra sesión está ejecutando un plan ahí). Debajo, los datos: `N archivos sin commitear` o `limpio`, el progreso nxy (`lote 2/3 · review hecha`), `handoff hace 2 h` o `sin handoff`, `N commits sin pushear` (sólo si el repo tiene un remoto) y, si hay `package.json` y falta `node_modules`, el aviso `falta node_modules`. El progreso de otro worktree sale de su `.nxy/local`: muestra lotes, suite y review, pero no «aprobado». Qué hacer:
  - **Nuevo.** Primero elegís **Rama nueva** o **Rama existente**. *Rama nueva*: el panel pide el n.º de issue (Enter vacío = sin issue; acepta `14` o `#14`), después el nombre, y por último la **rama base**: una lista con la rama principal del repo primero y ya elegida (la que apunta `origin/HEAD`; si no hay, la del worktree principal; si no, `main` o `master`) y después tus demás ramas locales. La rama nueva sale de la que elijas, no de la rama en la que estás parado. La rama es `feature/#14_algo` (con issue) o `feature/algo` (sin issue). «Cancelar» sirve en cualquier paso. La base es la rama **local**: nxy no hace fetch ni pull, así que si tu `main` está atrasada respecto del remoto conviene un `git pull` en el worktree principal antes de crear. *Rama existente*: elegís de la lista de tus ramas locales; las que ya están abiertas en otro worktree no se pueden elegir (git no permite la misma rama en dos) y el panel las lista aparte con dónde están; como base, en cambio, sí se puede usar una ocupada. La carpeta es `../<repo>.worktrees/<rama sin feature/>` (`feature/#14_algo` queda en `14_algo`, `fix/login` en `fix_login`: lo que no es a-z ni 0-9 pasa a `_`) y es la misma si después reabrís esa rama como existente. Antes de correr nada, el panel muestra el `git worktree add` exacto, con `-b <rama> <ruta> <base>` o sin `-b`, y pide confirmación. No instala dependencias: si falta `node_modules`, corré `npm install` ahí.
  - **Abrir.** Copia al portapapeles `cd <ruta>; claude` (PowerShell) o `cd <ruta> && claude` (bash) y muestra también el del otro shell; pegalo en una terminal nueva. Se copia y no se abre porque un Mod no puede abrir terminales ni sesiones.
  - **Pausar.** Escribe una marca de ~100 bytes (`.nxy/local/features/paused.json` de ese worktree) y muestra el handoff que ya existe. No se lo pide al modelo ni lo genera: si falta o es anterior a los últimos cambios, avisa y dice qué correr en la terminal de ese worktree. **Reanudar** borra la marca.
  - **Cerrar.** Corre `git worktree remove`. Un worktree limpio se cierra; con cambios, el panel pregunta y dice en mayúsculas que se pierden, también lo ignorado por git como `node_modules` y `.nxy/local`. Si hay una terminal con Claude abierta ahí, cerrala antes. Nunca cierra el principal, el de esta sesión ni uno con plan corriendo. La rama no se borra: vuelve a la lista de ramas libres. nxy nunca commitea ni pushea.
  - **Costo.** Abrir la pestaña o refrescarla (`r`) corre 1 node con 4 comandos git de lista (worktrees, ramas, `origin/HEAD` y si hay remotos) más hasta 2 por worktree y unas lecturas de archivos: ~0,9 s con 2 worktrees (medido en Windows). Nuevo corre 1 node y 1 `git worktree add` (más 2 chequeos de rama, la nueva y la base): ~0,6 a 0,7 s. Cerrar corre 2 node (recarga la lista para mostrar cifras frescas, y cierra): ~0,5 s. Pausar, 1 node y ~100 bytes. Cero node por turno y nada en SessionStart. Un worktree de este repo pesa 201 archivos, 2,7 MB, sin `node_modules` (26 MB acá), y se borra al cerrarlo.
- **Lanzar scout o librarian.** En **Agentes** (lista), un bloque **Preguntarle a un agente**: elegí **Scout** (dónde está algo en el código) o **Librarian** (qué recuerda la memoria sobre un tema), escribí la pregunta y Enter. Qué ves: el agente como una fila más de la lista (estado, tiempo, costo) y, al terminar, su respuesta: para el scout, los `ruta:línea` con su símbolo; para el librarian, las notas (clic para abrirlas en Memoria); si nada se pudo leer, la respuesta tal cual (12 líneas). **Ver agente** abre su detalle. Se lanza uno a la vez: mientras corre el botón dice «en curso». Ojo con el modelo: un agente lanzado desde el panel usa el **modelo** de su rol en Config pero **no el effort configurado** (usa el del propio agente), porque `$.agent.spawn` acepta `model` y no `effort`; cuando Claude los despacha por su cuenta, el hook aplica los dos. Los agentes viven en el Mod hasta que termina la sesión (tope de 40).
- **Botones y teclas.** En Inicio: **Gate once** (`g`: deja pasar una edición aunque el gate la rechace: plan, umbral de contexto o falta de handoff), **Handoff** (`h`: muestra el handoff de la rama), **Filter ● on** / **Filter ○ off** (`f`: alterna el filtro de salida y lo guarda donde diga **Guardar en** de Config, por defecto tu usuario; el botón muestra el estado actual, y dice sólo **Filter** mientras el panel todavía no leyó la configuración). Las pestañas se eligen con las teclas `1` a `7`. En Memoria: **Recientes**, **Handoff** y **Preguntarle al scout**. En Config: **cache 1 h** (`t`), **Statusline** (`l`) y **Actualizar nxy** (`u`). En Plan: **Terminar plan** (`e`: archiva el handoff, pide confirmación con `y` / `n`; sólo aparece si hay un plan o handoff y el orquestador no lo está corriendo; si el orquestador toma el plan mientras la pregunta está abierta, la pregunta se cierra y no se archiva nada) y **Arreglar N** (`z`, ya explicado arriba). En Stats, los selectores de la tendencia (clic, o Tab/flechas y Enter). `/nxy:stats` y `/nxy:trend` ya no tienen botón, pero siguen andando para quien no usa Mods. En cualquier pestaña, `r` refresca el panel. La salida de un botón se ve en un recuadro con borde, con el comando como título y una `x` (o la tecla `d`) para cerrarla; no pasa por la conversación. Si la cerrás o apretás otro botón mientras corre, la salida del primero no reaparece al terminar. Un botón lento no frena al orquestador.
- **`/nxy-panel`.** Abre el panel a cualquier ancho. En una terminal sin interfaz (`claude -p "/nxy-panel"`) no hay panel: imprime el mismo contenido como texto.
- **Apagar el panel.** `{ "ui": { "panel": "off" } }` en `.nxy/config.json` (o `~/.nxy/config.json`): no se abre solo, y `/nxy-panel` sigue andando.
- **Qué significa.** nxy corre los lotes (en paralelo los que no dependen entre sí), el tester y el reviewer por su cuenta, y te devuelve la conversación en el checkpoint 2, o antes si hace falta una decisión (suite en rojo, review que no quedó registrada, Stop). Mientras tanto Claude espera y nxy le rechaza despachar a mano.
- **Qué hacés.** Con un ✘ el panel te ofrece **Retry**, **Continue anyway** o **Stop**. Con `flow.pauseAfterBatch: true` también pausa entre tandas de lotes: **Continue**, **Adjust** o **Stop**; Adjust le devuelve el plan a Claude para que te pregunte qué cambiar. En una terminal sin interfaz (por ejemplo `claude -p`) no hay botones: un ✘ o una pausa vuelven a Claude con el texto de siempre. Si el orquestador falla en el medio, también te devuelve la conversación: Claude recibe qué lotes terminaron y cuáles faltan, y sigue el plan a mano como antes del orquestador.
- **Costo.** El hilo principal queda inactivo: en las transcripciones locales, orquestar era ~78 % de lo que gastaba el hilo principal mientras corría un plan. Se paga una reescritura de caché por plan. Sin plan, nxy no corre node por turno: sólo después de una pregunta (AskUserQuestion), mientras corre un plan, o cuando apretás un botón del panel, lo refrescás con `r` o usás `/nxy-panel`.
- **Cómo probarlo.** Un `.nxy/config.json` con `{"flow":{"plan":"always"}}` fuerza un plan hasta para una tarea chica; aprobalo y mirá la línea de estado y el panel.
- **Apagarlo.** `{ "flow": { "orchestrator": "off" } }`. Sin Mods, o con una versión de Claude Code que no los soporta, el módulo no carga y todo funciona como antes.

Los subagentes no pueden escribir archivos del proyecto por consola: un hook les deniega `sed -i`, las redirecciones, `tee` y los heredocs hacia archivos del proyecto, y el agente reintenta con Edit/Write. No ves nada ni tenés que hacer algo. El hilo principal no se ve afectado, y `cp`/`mv` y los archivos temporales siguen permitidos.

Qué te toca a vos en todo el proceso: contestar las preguntas, leer el plan como un diseño en un PR (¿reusa lo que existe?, ¿cada `Accept:` prueba lo correcto?) y elegir qué hallazgos arreglar.

### Una tarea en varios repos

Abrí Claude Code en un repo y nombrá en el pedido las rutas de los otros. El plan trae una sección `### Repos` con cada uno y su rol (api, web, lib compartida).

- **Qué ves.** Al aprobar, nxy anota **una sola vez por tarea** qué archivos ya estaban modificados en cada repo. Un plan revisado no repite la foto; sólo toma la de un repo que el plan nuevo suma. Se borra con `mem handoff done`. La review muestra los archivos como `<repo>/<ruta>`; si dos repos se llaman igual, nxy agrega carpetas hasta distinguirlos (`clientX/api`, `clientY/api`), sin configurar nada. Las convenciones, lentes y docs de cada repo tocado se aplican.
- **Qué significa.** Si se edita un repo que el plan no declaró, sale el aviso `<repo> is not in the plan; its prior uncommitted changes cannot be told apart` y sus cambios previos quedan en `## Not reviewed`.
- **Qué hacés.** Declaralo en `### Repos` y volvé a aprobar.

Regla del gate: sólo quedan libres los archivos fuera del proyecto y de todo repo git (el scratchpad, la memoria de Claude). Los de `.nxy/` siguen con gate porque son archivos tuyos que se commitean. `/nxy:gate once` ahora también deja pasar una edición después de `mem handoff done`.

### Cambios hechos fuera de Claude (scripts, generadores, proyectos sin git)

- **Qué ves.** Al aprobar, nxy copia los archivos que el plan nombra (hasta 200) y, en carpetas sin git, anota una lista liviana: ruta, tamaño, fecha y huella del contenido de cada archivo. Saltea `node_modules`, `target`, `dist`, `build`, `.git`, `.nxy` y similares; los archivos por encima del límite de copia sólo llevan tamaño y fecha. La lista se toma una vez por tarea, al primer plan aprobado; un plan revisado no la rehace. Con git no se hace inventario: git ya lo cubre.
- **Qué significa.** La review marca esos archivos `(inventory)`: los nuevos se revisan enteros; los modificados dicen "changed outside the plan's copies; no before" (no hay un antes: el revisor los lee y avisa si parecen fuera del plan). Un archivo que sólo cambió de fecha o se reescribió igual (`touch`, guardar sin cambios) no aparece. La sección `## Coverage` dice qué cubrió cada repo y qué no: carpetas salteadas, archivos grandes que cambian sin tocar tamaño ni fecha, más de 20.000 archivos (`too many files`, sólo copias).
- **Qué hacés.** Si algo importante queda sin cubrir, miralo a mano o nombrá el archivo en el plan.
- **Costo.** Al aprobar se lee cada archivo una vez (0,3 a 1 s para 5.000 archivos / ~30 MB); al revisar sólo se recorre y se lee lo que cambió de tamaño o fecha (0,1 a 0,3 s para 5.000 archivos). Unos 110 bytes por archivo. Se borra con `mem handoff done`.

### Cuando la sesión se pone cara

Cada mensaje reenvía todo el contexto acumulado: con 200k encima, cada paso cuesta el doble que con 100k. Cuando la statusline se pone roja:

1. Decile a Claude **"guardá el handoff"**: escribe en ~20 líneas qué se hizo, qué falta, qué archivos importan y qué se decidió.
2. `/clear`.
3. En la sesión nueva decí **"seguimos"**. Claude ya tiene la línea del handoff y lo carga.

La sesión nueva arranca con unos miles de tokens en vez de cientos de miles, sin perder el hilo. Si hay un plan en curso, el plan y los lotes verificados también pasan.

nxy te ayuda a no olvidarte: si la rama tiene handoff y Claude editó algo después de la última vez que se guardó, no termina el turno sin actualizarlo. Y en las pausas naturales (un lote en verde, la review hecha) te sugiere una vez cortar si el contexto pasó los 100k. Nunca corta solo.

### Buscar sin leer medio repo

```
/nxy:locate ¿dónde se decide si un comando pasa por rtk?
```

Un scout (Haiku) busca primero con herramientas que no gastan tokens (el índice de símbolos de nxy, `rg` y, si está, codegraph), confirma con dos o tres lecturas y devuelve hasta 5 `path:line` con tres líneas de código cada uno. Todo lo que leyó muere con él.

Usá el `path:line` directamente: si volvés a abrir los archivos enteros "para verificar", pagás lo que el comando evitó. Si no encuentra nada, te dice qué términos probó; suele significar que el código usa otras palabras que el dominio ("anular turno" vs. `cancelBooking`).

El índice se arma solo la primera vez, se actualiza sólo con lo que cambió y entiende JS/TS, Java, Scala, Python, Go y C#.

## Mensajes de nxy y qué hacer con cada uno

Cuando nxy frena algo, el mensaje le dice a Claude exactamente qué hacer, así que casi siempre se resuelve solo. Esto es lo que vas a ver y cuándo te toca actuar:

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `nxy: main thread context is 150k > 100k — do not edit … yourself` | La sesión está cargada; esa edición la hace un implementer | Nada. Si de verdad conviene editar ahí (una línea, sesión por cerrar): `/nxy:gate once` deja pasar la próxima edición |
| `… has no handoff yet. Save one before starting write work` | Sesión cargada y la rama no tiene handoff: Claude lo guarda antes de seguir | Nada; pasa una vez por rama |
| Pregunta `Plan Q1` / `Plan Q2` | El plan necesita una decisión tuya | Contestá. Si te da lo mismo, la recomendada |
| Pregunta **Approve nxy plan …?** | Hay un plan esperando tu aprobación | Leelo y elegí Approve o Change |
| **Save as repo conventions?** | Tus respuestas pueden quedar como reglas del repo. Con una sola decisión la pregunta es "Save as convention / Don't save"; con 2 a 4 se marcan las que valen; con más de 4 se reparten en varias preguntas de hasta 4 | Guardá sólo las que valen para todo el repo ("DTOs como records"), no las de esta tarea |
| `<repo> is not in the plan; its prior uncommitted changes cannot be told apart` | Se editó un repo que el plan no declaró: sus cambios previos quedan en `## Not reviewed` | Declaralo en `### Repos` del plan y volvé a aprobar |
| `warning: <ruta> does not exist` / `is not a git repo; its changes are not reviewed through git` | Un repo declarado en el plan no existe o no tiene git | Corregí la ruta; sin git, la revisión se apoya en copias e inventario |
| `copied the "before" of N file(s) the plan names` | Al aprobar, nxy copió los archivos que el plan nombra para poder compararlos después | Nada |
| Sección `## Coverage` en la review | Dice qué cubrió cada repo sin git y qué no (`too many files`: más de 20.000 archivos, sólo copias) | Si algo importante queda sin cubrir, miralo a mano o nombrá el archivo en el plan |
| `nxy verify: batch 2 ✘ … failed after the last edit` | El test del lote falló después del último cambio | **Retry** si es del cambio; **Continue anyway** si ya fallaba antes; **Stop** para mirarlo vos |
| `nxy: the orchestrator is running plan … so this dispatch is refused` | El orquestador (panel `nxy`) despacha lotes, tester y reviewer por su cuenta; Claude intentó hacerlo a mano | Nada: Claude termina el turno y nxy te devuelve la conversación en el checkpoint 2. Si no corre nada y quedó trabado: `node <plugin>/hosts/claude-code/entries/orch.mjs release --cwd <repo>` |
| `nxy verify: batch 3 – manual` | Ningún comando prueba ese lote (algo visual) | Miralo vos; el plan dice qué mirar |
| Pregunta **Review** con `R1`, `R2`… | Hallazgos de la review de lo que cambió el plan | Marcá los que querés arreglar antes del PR. Los *preexisting* son informativos |
| Pregunta **Docs** | Hay `.md` en el repo que nombran lo que cambió | **Update docs** si querés que viajen en el mismo PR |
| `plan … is approved, so its work goes through implementers` | Con plan aprobado, Claude no edita directo | Nada. Si querés algo fuera del plan ahí mismo: `/nxy:gate once` |
| `this session edited files after the handoff … was last saved` | Claude actualiza el handoff antes de terminar el turno | Nada; es una vez por cambio |
| `the main thread carries 160k tokens… a good point to cut` | Pausa natural con la sesión cara | Si te parece, "guardá el handoff" → `/clear` → "seguimos" |
| `nxy memory — possibly relevant to this message` | Hay decisiones guardadas que pueden aplicar | Nada; Claude carga la que le sirva |
| `this branch's plan was already reviewed` | Estás corrigiendo algo en una rama ya revisada | Si es algo que la review debió ver, dejá que Claude lo guarde como convención o lente (`/nxy:review escape`) |

## Comandos

| Comando | Para qué |
| --- | --- |
| `/nxy:feature <pedido>` | Arranca un cambio con la receta por tamaño; si es grande, con plan |
| `/nxy:locate <pregunta>` | Encuentra dónde está algo y devuelve `path:line`, sin llenar tu sesión de archivos. `--status`: qué buscadores hay |
| `/nxy:stats` | La sesión actual al detalle: tokens, costo, forma, por modelo, subagente y skill. `last`, `<id>`, `--all`, `--json` |
| `/nxy:trend` | Evolución por día, semana, sesión o modelo, de todos tus proyectos. `--since 30d`, `--by week\|session\|model`, `--here`, `--json` |
| `/nxy:mem …` | La memoria: `save`, `search`, `get`, `list`, `delete`, `export`, `import`, `status`, y `handoff` (`show`, `done`). Ver [La memoria](#la-memoria) |
| `/nxy:gate` | El freno de escritura: `status`, `once` (dejar pasar la próxima edición), `on`, `off` (por repo) |
| `/nxy:review` | `status` (hallazgos por lente en 30 días), `show` (la última review), `escape "<regla>" --as convention\|lens` (algo que la review no vio) |
| `/nxy:filter` | El filtro de comandos: `status`, `on`, `off` (por repo) |
| `/nxy:statusline` | Instala la statusline (`--apply`) o imprime el snippet para pegarlo a mano |

Para ver un reporte tal cual, sin que Claude lo retipee ni gaste tokens, usá el prefijo `!` de Claude Code: `! node "<ruta-al-plugin>/hosts/claude-code/entries/stats.mjs"`. También corren fuera de Claude: `node hosts/claude-code/entries/trend.mjs --by week --since 90d`.

## La statusline

Una línea siempre visible que responde tres preguntas: **¿cuánto me cuesta lo que Claude hace ahora?**, **¿cuándo conviene cortar?** y **¿qué pasa si dejo la sesión un rato?**

```
◆ nxy ⟡ my.app main ⟡ Opus 5 · high ⟡ ctx ▰▰▰▱▱▱▱▱ 41% · 82k ⟡ turno $0.42~ ⟡ sesión $34.74~ · 44M→241k · +4 agents ⟡ cache 98% · 52m ⟡ 5h 62% · 7d 31%
```

| Segmento | Qué es | Qué hacer con eso |
| --- | --- | --- |
| `my.app main` | Carpeta y rama | Confirmar que estás donde creés, sobre todo con varias sesiones abiertas |
| `Opus 5 · high` | Modelo y effort | Si es una tarea chica con Opus/high, cambialo al principio de la sesión: cambiar de modelo a mitad de camino reescribe toda la cache |
| `ctx … 41% · 82k` | Contexto que se reenvía en cada llamada, en **tokens reales**. Naranja desde 100k, rojo desde 200k | Naranja: no arranques nada grande. Rojo: handoff + `/clear`. Mirá los tokens, no el porcentaje: 41 % de 1M son 410k |
| `turno $0.42~` | Lo que va costando el turno actual, subagentes incluidos. Naranja desde $1, rojo desde $3 | **La señal más importante.** Un turno de $3 no es "trabajó mucho": es que cada llamada arrastra demasiado contexto |
| `sesión $34.74~ · 44M→241k · +4 agents` | El odómetro: costo total, tokens de entrada → salida, subagentes | Comparar con lo que hiciste. El detalle, en `/nxy:stats` |
| `cache 98% · 52m` | Cuánto de lo enviado ya estaba en cache (se paga a un décimo) y cuánto le queda de vida | Si baja de golpe, algo cambió en el contexto. Los minutos dicen cuánto podés ausentarte sin costo |
| `❄386k $3.86~` | La cache **venció**: el próximo mensaje reescribe 386k tokens y eso cuesta $3.86 antes de hacer nada | Buen momento para preguntarte si seguís o arrancás una sesión nueva con el handoff: cuesta lo mismo y arranca liviana |
| `5h 62% · 7d 31%` | Consumo de las ventanas de tu suscripción | Si vas al 90 % de la de 5 h, lo pesado puede esperar |

El `~` quiere decir *equivalente en USD*: con suscripción no pagás por token, pero es lo que ese uso costaría con API key.

Un `*` después de un monto (en la statusline, `/nxy:stats` y `/nxy:trend`) quiere decir que un modelo todavía no está en la tabla de precios de nxy y se calculó con la versión más cercana de la misma familia (por ejemplo, un futuro `claude-sonnet-5-6` con las tarifas de `claude-sonnet-5-5`); una línea de nota nombra el modelo y la base usada. Un `+?` quiere decir que el modelo es de una familia desconocida y no se pudo calcular. No tenés que hacer nada: el número se vuelve exacto cuando un release de nxy actualiza la tabla. Sonnet 5.5 ya tiene precio exacto.

Los umbrales son en tokens y no en porcentaje porque lo que encarece cada paso es cuánto se reenvía, no qué fracción de la ventana ocupa. Colores, presets (`vivid`, `classic`, `powerline`) y layout (`line`, `two-line`) se cambian en la [configuración](docs/configuracion.md#statusline) y se ven al instante.

### Si venís de nxy 0.1.x

Las versiones 0.1.x dejaron en tu `settings.json` un comando que apunta a una versión vieja del plugin, y seguía mostrando la statusline de entonces.

| Qué ves | Qué significa | Qué hacer |
| --- | --- | --- |
| Un mensaje, una sola vez, al abrir la sesión | nxy movió el comando de tu statusline a `~/.nxy/statusline.mjs` y guardó una copia del `settings.json` anterior (`settings.json.bak-<número>`, al lado del original; el mensaje dice la ruta) | Nada: reiniciá Claude Code y vas a ver la statusline actual |

Una statusline que no es de nxy no se toca nunca. El lanzador `~/.nxy/statusline.mjs` se actualiza solo en cada arranque de sesión, así que no hay que volver a correr `--apply` con cada versión.

## Ver en qué se gasta

`/nxy:stats` explica una sesión y `/nxy:trend` muestra cómo evolucionás. Tres cosas para mirar:

**1. La forma de la sesión.** El costo se explica con dos números: cuánto contexto lleva cada llamada y cuántas llamadas hace cada turno.

```
shape: 21 calls/turn (main 21) · main context avg 111k/call · peak 158k · subagents 0% of tokens
```

| Dato | Qué hacer si está alto |
| --- | --- |
| `calls/turn` | 20–30 es Claude explorando o editando solo. No es malo, pero 30 llamadas × 130k son 4M de tokens en un turno: pedí tareas más acotadas o delegá la exploración |
| `main context avg` | Por encima de ~100k cada paso cuesta el doble que al principio: es la señal de cortar y seguir |
| `subagents %` | En 0 % en sesiones grandes, el principal hace todo con el contexto a cuestas. Con nxy este número tiene que subir |

**2. Por qué se enfrió la cache.** Casi todo lo que pagás es contexto leído de la cache con 90 % de descuento. Lo caro es cuando vence y hay que reescribirlo:

```
cache rebuilds by cause: idle 6 · 1.8M · $17.33  |  subagent 2 · 420k · $4.10  |  start 1 · 182k · $1.14
cache TTL written: 5m ×3 · 1h ×6  ·  avoidable by warming (idle + subagent): $21.43 (6.4% of cost)
```

`idle` es una pausa más larga que la vida de la cache; `subagent`, el principal esperando a un subagente; `compact` y `start` son esperables. `cache TTL written` te dice si tu cuenta usa 5 minutos o 1 hora.

Si tu cuenta usa 5 minutos (API key, Bedrock, créditos extra), aparece una línea más con la cuenta hecha sobre tus propias sesiones:

```
cache TTL 1h what-if (main thread): $3.02 → $1.65 (-45.3%), 5 rebuilds would have been reads · worth it: set "promptCacheTtl": "1h" in ~/.claude/settings.json
```

Con una cache de 1 hora cada escritura cuesta más (2× el input en vez de 1,25×), pero las pausas de 5 a 60 minutos dejan de reescribir todo. nxy hace las dos cuentas y te dice cuál conviene: **worth it** (poné esa línea en `~/.claude/settings.json`; necesita Claude Code 2.1.242 o más nuevo), **about the same** o **not worth it here**. Con suscripción la sesión principal ya usa 1 hora y la línea no aparece.

**3. Cuánto ahorró el filtro.** `rtk saved` son los tokens que rtk recortó de las salidas de Bash. El ahorro grande está en tests, builds, `git`, `grep` y `ls`; leer código con `cat` no se recorta, a propósito.

## La memoria

Lo que decidiste en una sesión se va con el `/clear`. La memoria de nxy es una base local (SQLite, sin servidor) con búsqueda de texto completo, que se comparte por git.

```
/nxy:mem save "Los montos son BigDecimal" --body "Nunca double para dinero; ver PR #120." --type convention --keywords "plata importe precio"
/nxy:mem search "cómo guardamos los montos"
/nxy:mem list --type convention
/nxy:mem get <id>
/nxy:mem delete <id>
```

Guardá la decisión cuando la tomás: *qué se decidió y por qué*, no *qué hace el código* (eso ya está en el código). `--keywords` es para los sinónimos que vos usarías y el texto no tiene. Tipos: `decision`, `bug`, `convention`, `preference`, `handoff`.

**Tres ámbitos:**

| Ámbito | Para qué | Viaja |
| --- | --- | --- |
| `global` (`--scope global`) | Vos, en todos tus proyectos ("no commiteo desde el agente") | Nunca sale a un repo |
| `project` (default) | Este repo | Se commitea y lo hereda el equipo |
| `area` (`--area api/auth`) | Un subárbol del repo | Igual que `project`, pero sólo aparece cuando trabajás ahí |

El proyecto se identifica por el remote de git, así que el mismo repo es el mismo proyecto en otra carpeta, otra máquina o un worktree.

**Compartir con el equipo:**

```
/nxy:mem export            # escribe <repo>/.nxy/memory/*.md
git add .nxy/memory        # y commiteás como cualquier cambio
/nxy:mem import            # del otro lado, después de un pull
```

Son archivos Markdown, uno por memoria, para que una convención se pueda discutir en un PR. El import se puede correr siempre (no duplica), gana la versión más nueva y nunca borra nada tuyo. Lo `global` y lo marcado `--private` no se exportan.

**El handoff** es la memoria de la tarea: una por rama, ~20 líneas con qué se hizo, qué falta, archivos clave y decisiones. Lo guarda Claude (se lo pedís, o nxy se lo pide cuando la sesión ya está cargada), cada guardado reemplaza al anterior y no se exporta salvo que lo pidas. `/nxy:mem handoff` lo muestra; `/nxy:mem handoff done` lo archiva.

**Los punteros.** Antes de cada mensaje tuyo, nxy busca memorias relacionadas (por los archivos que nombrás o que se vienen editando, por keywords, por palabras compartidas) y le pasa a Claude hasta tres punteros de una línea. Decidir qué apuntar no gasta tokens; cada puntero cuesta ~30. Si una memoria reemplaza a otra, decilo al guardar (`--supersedes <id>`) o dejá que lo decida el *librarian*, un subagente Haiku que relaciona memorias nuevas con las parecidas y busca por significado cuando las palabras no coinciden.

`/nxy:mem status` muestra cuántos punteros se mostraron, cuántos usó Claude y cuánto costaron.

## Configuración

Casi nadie necesita tocar nada: los defaults están elegidos para que funcione sin calibrar. Hay tres niveles, y el de más abajo gana:

| Archivo | Alcance |
| --- | --- |
| `nxy.config.json` (en el plugin) | Defaults |
| `~/.nxy/config.json` | Vos, en todos tus proyectos |
| `<repo>/.nxy/config.json` | El repo; **se commitea** y lo hereda quien lo clone |

Lo que se cambia más seguido:

| Querés | Poné en `.nxy/config.json` |
| --- | --- |
| Plan obligatorio para todo cambio de más de un archivo | `{ "flow": { "plan": "always" } }` |
| Que el planner de este repo use Opus | `{ "roles": { "planner": { "model": "opus" } } }` |
| Apagar el freno de escritura | `{ "gate": { "enabled": false } }` (o `/nxy:gate off`) |
| Apagar el filtro | `{ "modules": { "filter": false } }` (o `/nxy:filter off`) |
| Que nxy no inyecte nada de memoria solo | `{ "memory": { "mode": "manual" } }` |
| Que el documenter busque docs en otra carpeta | `{ "docs": { "paths": ["wiki/"] } }` |
| Lentes de review propias del repo | Un `.md` en `.nxy/lenses/` (se commitea) |
| Que el orquestador espere tu OK entre tandas de lotes | `{ "flow": { "pauseAfterBatch": true } }` (default `false`) |
| Apagar el orquestador de planes (con Mods) | `{ "flow": { "orchestrator": "off" } }` (default `"auto"`) |

### Modelo y effort de cada agente

| Agente | Qué hace | Modelo | Effort |
| --- | --- | --- | --- |
| `scout` | Encuentra dónde está algo (`/nxy:locate`) | Haiku | — |
| `planner` | Arma el plan por lotes | Sonnet | high |
| `implementer` | Escribe un cambio ya decidido y corre su test | Sonnet | medium |
| `tester` | Corre la suite completa una vez al final del plan | Haiku | — |
| `reviewer` | Revisa lo que cambió el plan | Sonnet | high |
| `documenter` | Actualiza los `.md` que nombran lo cambiado | Sonnet | low |
| `librarian` | Relaciona memorias y busca por significado | Haiku | — |

**El modelo se cambia por config**, para vos (`~/.nxy/config.json`) o para un repo (`.nxy/config.json`): `{ "roles": { "reviewer": { "model": "opus" } } }`. Valores: `haiku`, `sonnet`, `opus`, `fable`. nxy lo aplica en cada despacho. Lo mismo vale para el effort: `{ "roles": { "reviewer": { "effort": "high" } } }` con `roles.<rol>.effort` (`low`, `medium`, `high`), también aplicado en cada despacho, con o sin el Mod; si coincide con el del agente, o el despacho ya trae uno, no se toca. Si tu sesión principal usa un modelo de esa familia (por ejemplo Sonnet 4.6 1M), el agente corre en ese mismo modelo.

**El effort no se puede cambiar hoy.** Claude Code no permite elegirlo por despacho, así que vale el que trae cada agente en `agents/*.md`. Editar esos archivos funciona, pero se pisa al actualizar el plugin.

### Dónde guarda nxy sus cosas

Igual que `.claude`, hay una carpeta tuya y una por repo:

| Carpeta | Qué tiene | ¿Se commitea? |
| --- | --- | --- |
| `~/.nxy/` | Tu config global, la base de memoria (`memory/memory.db`), la caché del filtro y de la statusline, y el lanzador `statusline.mjs` | No: es de tu máquina |
| `<repo>/.nxy/` | `config.json`, `memory/*.md` exportadas y `lenses/`: lo que el equipo comparte | **Sí** |
| `<repo>/.nxy/local/` | El estado de tu tarea en este checkout: handoff, plan, ledgers, índice, las copias para la review y `features/paused.json` (la marca de pausa de la pestaña Features, ~100 bytes) | No: va en `.gitignore` |

Borrar `.nxy/local/` de un repo descarta la tarea en curso de ese checkout y nada más; el índice se rehace solo. La referencia completa, con statusline y temas: [`docs/configuracion.md`](docs/configuracion.md).

## Si algo no anda

| Síntoma | Qué pasa | Qué hacer |
| --- | --- | --- |
| Los comandos no salen filtrados | rtk no está, está fuera del PATH, o tiene su propio hook | `/nxy:filter status` te dice cuál y cómo arreglarlo. Si lo instalaste con la terminal abierta, abrí una nueva |
| Aviso de que el hook de rtk está instalado | Corriste `rtk init -g` | Desinstalá el hook de rtk; nxy usa el suyo |
| Claude necesita la salida completa de un comando | El filtro recortó algo que hacía falta | Claude lo resuelve solo con `rtk recall <hash>`; como último recurso, `NXY_RAW=1 <comando>` |
| El freno de escritura frena todo | La sesión pasó los 100k de contexto; es lo esperado | Para una edición puntual, `/nxy:gate once`; para ese repo, `/nxy:gate off` |
| Mucho costo en `idle` o `subagent` | Tu cuenta usa cache de 5 minutos y cada pausa o subagente largo la enfría | Mirá la línea `cache TTL 1h what-if` de `/nxy:stats` o `/nxy:trend`: si dice **worth it**, poné `"promptCacheTtl": "1h"` en `~/.claude/settings.json` |
| Algún hook se comporta raro | Los hooks de nxy fallan abiertos: ante cualquier error dejan pasar | `NXY_DEBUG=1` muestra la causa en stderr (`claude --debug`) |
| Checkout compartido entre Windows y WSL con codegraph | El lock de SQLite no cruza filesystems | `CODEGRAPH_DIR=.codegraph-win` en Windows y el default en WSL |

## Qué le agrega nxy a tu sesión

nxy también cuesta algo, y está medido:

- **Contexto fijo:** ~1.000 tokens en cada llamada: las descripciones de 9 comandos, 7 agentes y 2 skills. A precio de cache es menos de un centavo cada 30 llamadas.
- **Tiempo por hook** (medido en Windows, Node 22): 40–50 ms típico; ~105 ms antes de cada comando Bash con rtk (incluye preguntarle a rtk) y ~40 ms después; ~130 ms al abrir la sesión.
- **Tokens de los hooks:** cero cuando no frenan nada. Cuando frenan, el mensaje. Los punteros de memoria, ~30 tokens cada uno, hasta tres por mensaje y cada memoria una vez por sesión.
- **Disco:** la base de memoria en `~/.nxy/memory/`, y por repo `.nxy/local/` (una línea por comando Bash, el índice, y durante un plan una copia de cada archivo que toca y una lista corta de los archivos que ya estaban modificados al aprobar, que se borran con `handoff done`).

## Estado del proyecto

| Parte | Estado |
| --- | --- |
| Versión actual | `1.0.3` |
| Métricas, statusline, filtro con rtk | Publicado desde v0.1.x y usado a diario |
| Scout, freno de escritura, memoria, handoff, plan, verificación, review | Publicado en `1.0.1`, probado en sesiones reales (Linux/WSL y Windows) durante las rc |
| Después | CLI `nxy` (`doctor`, `stats`, `trend`, `mem`) para operarlo sin abrir Claude Code |

## Desarrollo

```
npm install        # sólo devDependencies (typescript)
npm test           # node --test
npm run typecheck  # tsc sobre JSDoc (// @ts-check)
npm run check      # las dos
```

JavaScript ESM con JSDoc, sin build ni dependencias de runtime. El CI corre tests y typecheck en Ubuntu y Windows.

```
core/                # lo que no sabe nada de Claude Code: filtro, métricas, memoria, plan, verify, review, índice
hosts/claude-code/   # lo específico de Claude Code: transcripts, hooks, statusline, entries (los scripts de cada comando)
agents/              # scout, planner, implementer, tester, reviewer, documenter, librarian
commands/ skills/ hooks/ lenses/ .claude-plugin/
```

Regla: **`core/` nunca importa de `hosts/`.** Soportar otro agente es escribir un `hosts/<nombre>/`. Detalle en [`docs/como-funciona.md`](docs/como-funciona.md#estructura-del-código).

## Créditos

`hosts/claude-code/transcripts.mjs` deriva del plugin `session-report` de Anthropic (Apache-2.0). El filtrado lo hace [RTK](https://github.com/rtk-ai/rtk) (Apache-2.0), binario externo. Ver `NOTICE`.
