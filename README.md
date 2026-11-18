<p align="center">
  <img src="resources/nxydev.png" alt="nxy.dev" width="320">
</p>

<p align="center">Plugin de Claude Code para gastar menos tokens sin perder calidad, y para ver en qué se va cada dólar.</p>

nxy hace cuatro cosas:

- **Te muestra lo que gastás.** Una statusline con el costo del turno y de la sesión, y reportes por sesión, modelo, subagente y día. Los números salen del `usage` real que Claude Code guarda en sus transcripts: no son estimaciones.
- **Achica lo que Claude lee.** Las salidas de tests, builds y `git` pasan filtradas por [RTK](https://github.com/rtk-ai/rtk). Las búsquedas las hace un scout barato que se descarta al terminar, y con la sesión cargada las ediciones las hace un subagente.
- **Le pone método a los cambios.** Una *feature* tiene un plan que aprobás vos, lotes que nxy verifica con un test, tests afectados, una review y los arreglos que vos elijas. Si querés decidir el diseño antes de planificar, el modo ingeniero.
- **Recuerda.** El estado de cada feature, las reglas del repo y tus reglas globales viven en una base local y sobreviven al `/clear`. Una consola en tu terminal (la TUI) te deja ver todo eso y contestar las preguntas de nxy desde ahí.

Funciona con cualquier lenguaje, en Windows, Linux y macOS. nxy es un solo binario escrito en Go: no necesitás Node ni nada más instalado.

## Contenido

- [Instalación](#instalación)
- [Cómo se usa](#cómo-se-usa)
- [Mensajes de nxy y qué hacer con cada uno](#mensajes-de-nxy-y-qué-hacer-con-cada-uno)
- [Comandos](#comandos)
- [La statusline](#la-statusline)
- [Si venís de la 1.x](#si-venís-de-la-1x)
- [Windows Defender](#windows-defender)
- [Ver en qué se gasta](#ver-en-qué-se-gasta)
- [La memoria](#la-memoria)
- [Configuración](#configuración)
- [Dónde guarda nxy sus cosas](#dónde-guarda-nxy-sus-cosas)
- [Si algo no anda](#si-algo-no-anda)
- [Qué le agrega nxy a tu sesión](#qué-le-agrega-nxy-a-tu-sesión)
- [Estado del proyecto](#estado-del-proyecto)
- [Desarrollo](#desarrollo)
- [Créditos](#créditos)

Cómo funciona cada pieza por dentro: [`docs/como-funciona.md`](docs/como-funciona.md). Todas las opciones: [`docs/configuracion.md`](docs/configuracion.md).

## Instalación

### 1. Requisitos

No hay nada obligatorio salvo **`curl`** (viene con Windows 10 y 11 y con macOS; en Linux, instalalo con tu gestor de paquetes si falta): la primera vez que Claude Code corre un hook, el lanzador de nxy (`bin/nxy.cmd`) baja el binario de la versión desde el release de GitHub, verifica su SHA-256 y lo deja en `~/.nxy/bin/<versión>/`. Tarda unos segundos, una sola vez por versión. Si no tenés `curl`, o no hay red, ves un mensaje que empieza con `nxy:` y los hooks no hacen nada hasta que lo resuelvas (ver [Si algo no anda](#si-algo-no-anda)). Hay binarios para Windows, Linux y macOS, en amd64 y arm64.

Opcionales, que suman ahorro:

| Herramienta | Para qué | Sin ella |
| --- | --- | --- |
| [rtk](https://github.com/rtk-ai/rtk) | Filtra la salida de los comandos (tests, builds, git, logs) | Los comandos pasan enteros; nada se rompe |
| [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) | Búsqueda de texto del scout; rtk también lo usa | El scout busca más lento y rtk recorta menos |
| [codegraph](https://github.com/colbymchenry/codegraph) | "Quién llama a qué" para el scout, y qué tests afecta un cambio | El scout usa el índice propio de nxy y `rg`; los tests afectados se buscan por nombre |
| [d2](https://d2lang.com) | Dibuja los diagramas del modo ingeniero en la TUI | El diagrama se muestra como texto fuente |

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

En Linux, rtk queda en `~/.local/bin`; si no está en el PATH, nxy igual lo encuentra. Verificá con `rtk --version` y `rg --version`.

codegraph (opcional) se instala así, y después se inicializa una vez en cada repo donde lo quieras (nxy también lo crea solo cuando lo necesita para los tests afectados):

```
# Windows (PowerShell)
irm https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.ps1 | iex

# Linux / WSL / macOS
curl -fsSL https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.sh | sh

# en la raíz de cada repo
codegraph init
```

Si preferís no pegar comandos, `/nxy:setup` detecta qué te falta (rtk, rg, codegraph y d2), te muestra el comando exacto para tu sistema y lo corre sólo si le decís que sí.

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
/nxy:statusline install
```

La escribe en `~/.claude/settings.json` (con un backup `settings.json.bak-nxy-<fecha>` al lado; se conservan los 3 últimos) y te pide reiniciar Claude Code para verla. Sin argumentos sólo te muestra el bloque que se escribiría y si ya está instalada; `/nxy:statusline remove` la quita, y sólo si es la de nxy. Una statusline que no es de nxy no se toca nunca.

### 4. La TUI (opcional)

La TUI es una consola de control que abrís en otra terminal, al lado de Claude Code: tus features, el plan, los agentes, la memoria, el gasto y la config. `/nxy:tui` te muestra el comando exacto para abrirla (es lo mismo que `nxy tui where`); nxy no abre terminales solo. Para que alcance con escribir `nxy tui`, corré `/nxy:setup` y aceptá la fila «nxy en el PATH»: en Windows suma `~/.nxy/bin` al PATH del usuario; en Linux y macOS crea `~/.local/bin/nxy`, un script de 2 líneas (esa carpeta tiene que estar en tu PATH). Para sacarlo, quitá esa carpeta del PATH o borrá ese archivo. `nxy tui --demo` abre el mismo programa con datos de ejemplo, sin tocar tu repo ni tu base. Qué ves en cada pantalla y qué teclas usar: [La TUI](docs/como-funciona.md#la-tui).

### 5. Verificá

| Comando | Tiene que decir |
| --- | --- |
| `/nxy:filter status` | El motor del filtro: `rtk` (o por qué está apagado y cómo instalarlo) |
| `/nxy:locate --status` | Qué buscadores tenés: índice, `rg`, codegraph |
| `/nxy:mem show` | Las reglas y aprendizajes de este repo (vacío si todavía no hay) |
| `/nxy:gate status` | Si el freno de escritura y el de lectura están prendidos, y desde qué umbral frenan |
| `/nxy:setup` | Por herramienta (rtk, rg, codegraph, d2): si está, de dónde, si hay un hook que choca, y el comando exacto para instalarla; no instala nada solo |

## Cómo se usa

### Lo que pasa solo

No tenés que aprender nada para aprovechar la mayor parte. Con nxy instalado:

- **Los comandos salen filtrados.** Un `./mvnw test` que devolvía 84 líneas le llega a Claude en 8. Si Claude necesita la salida completa, sabe pedirla. En auto mode se hace distinto (ver [rtk en auto mode](#rtk-en-auto-mode)).
- **La statusline te avisa cuándo cortar.** Naranja o rojo en `turno` o `ctx` quiere decir que cada mensaje ya cuesta caro (ver [La statusline](#la-statusline)). Cortar es barato: el estado de la feature vive en la base de nxy, no en el chat.
- **El hilo principal no edita código por su cuenta.** Sin una feature activa, Claude no escribe archivos de código en el hilo principal: o arrancás una feature, o delega el cambio en un subagente (el *implementer*), que lee lo justo, edita, prueba y se descarta. Así los archivos no se quedan en el contexto caro de la sesión principal. Docs y texto pasan.
- **Las búsquedas amplias se delegan.** Con el contexto del hilo principal en 100k tokens o más, un `Grep`, `Glob` o `rg` sin apuntar a un archivo se frena y Claude manda un scout.
- **Las reglas llegan solas.** Las reglas de tu repo y tus reglas globales se le entregan a cada agente que las necesita, sin que hagas nada.
- **Claude sabe dónde quedaste.** Al abrir una sesión (también después de `/clear`) con una feature sin terminar, Claude recibe el «para retomar»: el pedido, el paso, «lote N de M», lo último que falló y las decisiones.

### Según el tamaño de la tarea

| Tarea | Qué hacés | Qué pasa |
| --- | --- | --- |
| **Puntual**: un cambio que ya está claro, en uno o dos archivos | La pedís como siempre | El hilo principal no edita código solo: Claude despacha un implementer con el `path:line` y el criterio de aceptación. Si de verdad conviene que edite él, `/nxy:gate once` deja pasar una edición |
| **Buscar algo**: no sabés dónde está el código | `/nxy:locate <pregunta>` | Un scout encuentra el `path:line` sin llenar tu sesión de archivos |
| **Con forma**: varios archivos, algo nuevo, o más de un repo | `/nxy:feature <pedido>` | Plan → tu aprobación → lotes verificados → tests afectados → review → arreglos elegidos |
| **Con decisiones de diseño**: querés decidir vos antes de que nadie planifique | `/nxy:engineer <pedido>` | Debatís cada bifurcación con Claude, guardás cada decisión, y recién ahí se planifica |

### Una feature, paso a paso

```
/nxy:feature alta de clientes corporativos con validación de CUIT
```

1. **Planner.** nxy guarda tu pedido tal cual y despacha al *planner* (Opus, effort high). Busca en el repo con `nxy locate`, lee las reglas del repo y arma un plan por lotes: criterios `R1…Rn` (lo que tiene que cumplir esta feature), y para cada lote qué cambia, con qué test se prueba (el `Accept:`) y qué riesgo tiene. nxy valida el plan: si está mal, el planner ve los problemas y lo corrige. Si el planner tiene preguntas que sólo vos sabés contestar, te las hace antes del plan.
2. **Aprobás el plan.** Claude te muestra el plan y te pregunta **«¿Aprobás el plan `<id>` r`<n>` de nxy?»** con **Aprobar**, **Cambiar** o **Ver completo**. Hasta que aprobás no se escribe una línea de código. **Ver completo** abre el plan entero en tu editor. **Cambiar** te pide una nota y el planner replanifica; cada replanificación sube la revisión (`r<n>`) y hay que aprobarla de nuevo. nxy verifica que la aprobación sea tuya: Claude puede hacer la pregunta, no contestarla por vos. **Los `Accept:` son comandos que nxy va a correr**: leelos antes de aprobar.
3. **Lotes verificados.** nxy despacha un implementer por lote y le escribe el prompt. El implementer prueba con `nxy batch red` y `nxy batch green`, que corren el `Accept:` por él: el veredicto lo decide el código, no el modelo. Si un lote queda en rojo, nxy lo reintenta una vez solo; si falla de nuevo ves **«Lote n fallido»** con la causa y elegís **Reintentar**, **Seguir** (aceptarlo así) o **Parar**. Los lotes que no dependen entre sí (un repo de back y uno de front) corren en paralelo.
4. **Tests afectados.** Con todos los lotes en verde, nxy calcula qué cambió desde que aprobaste, elige los tests afectados (con codegraph si está, y si no por nombre, o la `Suite:` del plan) y los corre. Si salen en rojo, un implementer los arregla una vez; si sigue rojo (o no hay nada que correr) ves **Afectados fallidos** o **Afectados sin verificar**, con **Reintentar**, **Seguir** o **Parar**. «Sin verificar» nunca cuenta como verde.
5. **Review.** La profundidad depende del riesgo: bajo, un `reviewer-quick`; medio, un `reviewer` por lente (persistencia, API, frontend, base de datos, seguridad, contrato entre repos); alto, además un `refuter` que intenta refutar los hallazgos graves. nxy sube el riesgo que puso el planner si ve archivos de seguridad, base de datos o migraciones; nunca lo baja. Ves los hallazgos numerados con su escenario y su arreglo, y **elegís cuáles se arreglan**. Un único implementer arregla los elegidos y nxy vuelve a correr los afectados.
6. **Docs.** Si algún documento para usuarios (README, un `.md` en la raíz, o lo que esté en `docs/`, `doc/`, `documentation/` o `wiki/`) nombra lo que cambió, te ofrece **Actualizar docs** o **Dejar**.
7. **Cierre.** Ves `Listo · R1 ✔ <test> · …`: qué criterio quedó probado con qué test, y qué arreglos se aplicaron. Si algo quedó «sin verificar», lo dice: corré tus tests a mano. Después te ofrece guardar como reglas del repo las decisiones marcadas `[repo]` (de a 4 por pregunta); si no elegís nada, no se guarda nada. Vos mirás el diff, commiteás y abrís el PR: **nxy nunca commitea ni pushea**.

Qué te toca a vos en todo el proceso: contestar las preguntas, leer el plan como un diseño en un PR (¿reusa lo que existe?, ¿cada `Accept:` prueba lo correcto?) y elegir qué hallazgos arreglar. Por dentro: [El plan](docs/como-funciona.md#el-plan), [Los lotes y su verificación](docs/como-funciona.md#los-lotes-y-su-verificación), [Los tests afectados](docs/como-funciona.md#los-tests-afectados) y [La review](docs/como-funciona.md#la-review).

Qué modelo corre cada agente (se cambia por config, ver [Configuración](#configuración)):

| Agente | Qué hace | Modelo | Effort |
| --- | --- | --- | --- |
| `planner` | Arma el plan por lotes | Opus | high |
| `implementer` | Escribe un lote o un arreglo y prueba con `nxy batch` | Sonnet | medium |
| `reviewer` | Revisa el cambio con una lente | Sonnet | high |
| `reviewer-quick` | La review completa de un cambio de riesgo bajo | Sonnet | medium |
| `refuter` | Intenta refutar los hallazgos graves de la review | Sonnet | high |
| `scout` | Encuentra dónde está algo (`/nxy:locate`) y explora para el diseño | Sonnet | medium |
| `documenter` | Actualiza los docs que nombran lo cambiado | Sonnet | low |

Mientras hay una feature sin terminar, el hilo principal tampoco escribe el repo: ni con `Edit` ni por consola (`echo > archivo`, `sed -i`, `tee`, `cp`, `mv`); escribe el implementer. No ves nada ni tenés que hacer algo. Fuera del repo y sobre `.git` o `.nxy` no pasa nada.

### Una feature y su estado

La unidad de trabajo es la **feature**: nace con `/nxy:feature` o `/nxy:engineer`, y vive en `~/.nxy/nxy.db`, no en el chat. Guarda el pedido, el plan, los lotes con su veredicto, la review y el paso en que está.

- **Qué ves.** `nxy status` te dice dónde está en cuatro estados: **Trabajando** (nxy o un subagente hace algo), **Verificando** (se corre una prueba), **Necesita tu decisión** (te toca a vos: responder, aprobar, reintentar o parar) y **Listo**. Las features de varios repos y de todos tus checkouts se ven juntas en la TUI.
- **Qué significa.** Un `/clear`, una compactación o abrir otra sesión no pierden nada: Claude recibe el «para retomar» armado desde la base. Hay **una sola feature sin terminar por checkout**; si ya hay una, `/nxy:feature` te dice cuál.
- **Qué hacés.** Para terminarla en cualquier punto: `nxy feature stop` (libera las ediciones y borra el plan `.md` y la review `.md`). Si reabrís una sesión en una carpeta cuya feature no tiene otra sesión viva, `/nxy:feature` responde «ya hay una feature en curso».
- **Notas.** Una nota (`N3`) es algo que dejaste a medias en una carpeta, para retomar en otra sesión sin releer el chat: `nxy note save "<título>"` (cuerpo por stdin, hasta 2 KB), `nxy note list`, `nxy note done <id>`. Sin feature activa y con notas en la carpeta, tu primer mensaje dispara la pregunta «¿Con cuál seguimos?»; al pausar un worktree, Claude te pregunta «¿Algo para la próxima sesión?». Vencen a los 30 días.
- **«Stop hook error».** Si un paso pendiente llega justo al terminar el turno, Claude Code lo rotula así. El rótulo es de Claude Code, no es un error: nxy usa ese camino para que Claude siga.

Detalle: [Features y notas](docs/como-funciona.md#features-y-notas).

### Decidir desde la TUI

Cuando Claude te hace una pregunta de nxy (aprobar el plan, el resultado de una review, qué lote reintentar…) y tenés la TUI abierta, no hace falta volver a la terminal de Claude Code.

- **Qué ves.** La terminal de Claude Code dice «nxy: si tenés la TUI abierta, contestá ahí» y el título pasa a «● nxy: decisión pendiente». En la TUI aparece «● nxy: N decisiones» y una ventana de decisión con la pregunta y sus opciones.
- **Qué significa.** nxy retiene la pregunta en vez de mostrar el diálogo, y lo que elegís en la TUI vuelve a Claude como si lo hubieras contestado ahí. Sin TUI abierta no cambia nada: el diálogo es el de siempre.
- **Qué hacés.** Elegí con `↑↓` y `enter`. Con `t` (o cerrando la TUI, o pasados 25 minutos) la pregunta vuelve al diálogo normal de la terminal. En el plan por aprobar, `a` aprueba, `c` pide un cambio y `x` para la feature. El modelo no puede contestarse solo: `nxy action decide` sólo lo corre la TUI y los hooks lo rechazan si lo intenta otro.

Qué ves en cada pantalla, las teclas y cómo funciona por dentro: [La TUI](docs/como-funciona.md#la-tui). Para ver una pantalla como JSON sin abrir la TUI: `nxy view <cockpit|agents|decision|memory|stats|trend|config|plan|file|engineer> --json`.

### Modo ingeniero

`/nxy:engineer <pedido>` (o la oferta **Pasar a engineer** que aparece entre las preguntas de `/nxy:feature`) es para cambios donde querés decidir vos el diseño.

- **Qué ves.** `nxy status` dice «Diseñando» y el chat muestra la agenda (`3 de 5 decididas`) con cada bifurcación B1, B2… y su estado. Antes, Claude hace un reconocimiento con un scout y guarda un diagrama chico del estado actual.
- **Qué significa.** Todavía no hay plan ni se toca código: se debate. Claude te refuta o te da 2–3 opciones con su trade-off, y su opinión es condicionada («B, salvo que X»). No cede por insistencia: cambia de opinión sólo con un argumento, y si cerrás con tu opción igual, queda registrado que no estaba de acuerdo.
- **Qué hacés.** Respondé en el chat o elegí en la TUI (pantalla Ingeniero: ves las opciones como tarjetas; la opinión de Claude queda detrás de una tecla para no condicionarte). Cada decisión se guarda con una pregunta «¿guardar la decisión D<n>?». Cuando están todas, **Cerrar y planificar** aprueba los criterios y arranca el planner. Si decís «decidí vos el resto», el planner elige y lo marca. Un `/clear` es seguro: las decisiones ya están en la base.

Detalle: [El modo ingeniero](docs/como-funciona.md#el-modo-ingeniero). Los diagramas los dibuja `d2` si lo instalaste (`/nxy:setup`); sin d2 ves la fuente como texto.

### Una tarea en varios repos y worktrees

Abrí Claude Code en un repo, o en una carpeta padre que no es repo, y nombrá en el pedido los otros repos. El plan trae una línea de repos con su rol (api, web, lib compartida) y cada lote toca uno solo.

- **Qué ves.** Al aprobar, Claude te pregunta la rama y la base de cada repo. Con un repo, la rama se crea en tu carpeta; con dos o más, o si el checkout está ocupado por otra feature, nxy crea un worktree por repo (`<repo>.worktrees/<slug>`) y te dice cómo abrirlos. La review suma la lente de **contrato entre repos**, que cita ambos lados.
- **Qué significa.** Los lotes de repos distintos corren en paralelo cuando no dependen entre sí. Editar el checkout original de un repo que ya tiene worktree se rechaza («edit in `<worktree>`»). Un worktree nuevo no trae dependencias (`node_modules`, build): nxy avisa cuáles faltan y no instala nada.
- **Qué hacés.** `/nxy:worktree list` muestra las features activas y pausadas con sus repos, ramas y worktrees. `new`, `pause`, `resume` y `close` los manejan (`close` con cambios sin guardar pide `--force`). nxy nunca hace stash ni force, y nunca commitea ni pushea.

Detalle: [Varios repos y worktrees](docs/como-funciona.md#varios-repos-y-worktrees).

### Buscar sin leer medio repo

```
/nxy:locate ¿dónde se decide si un comando pasa por rtk?
```

Un scout busca primero con herramientas que no gastan tokens (el índice de símbolos de nxy, `rg` y, si está, codegraph), confirma con dos o tres lecturas y devuelve hasta 5 `path:line`, con lo que miró y descartó. Todo lo que leyó muere con él. Usá el `path:line` directamente: si volvés a abrir los archivos enteros «para verificar», pagás lo que el comando evitó.

También podés correr la búsqueda vos, sin modelo: `nxy locate "<consulta>"` (0 tokens, milisegundos). `--callers X`, `--callees X` y `--impact X` piden relaciones (quién llama a X, qué llama X, qué se rompe si cambio X): con codegraph instalado y el repo indexado usa sus relaciones; sin él cae a `rg` y dice qué degradó. `--status` muestra qué buscadores hay y `--index` reconstruye el índice. Una consulta `/regex/i` se toma como regex. El índice se arma solo la primera vez, se actualiza con lo que cambió y entiende JS/TS, Java, Scala, Python, Go y C#. `scout.codegraph: false` en la config desactiva codegraph.

Si no encuentra nada, te dice qué términos probó; suele significar que el código usa otras palabras que el dominio («anular turno» vs. `cancelBooking`).

### rtk en auto mode

- **Qué ves.** En una sesión con `permission_mode` auto y rtk activo, Claude escribe él mismo `rtk git status` en vez de `git status` (o la ruta de rtk que fijaste en `NXY_RTK_PATH`). Si se olvida, ves como mucho un intento denegado que nombra el comando exacto, y Claude lo repite con `rtk`.
- **Qué significa.** Claude Code rechaza un comando cuyo input cambió sin una decisión de permiso, y nxy no aprueba nada por vos: por eso en auto mode no reescribe, le pide a Claude que anteponga `rtk`. Un comando que ya matchea una regla `allow` tuya se reescribe y permite como en los otros modos.
- **Qué hacés.** Nada.

### Cuando la sesión se pone cara

Cada mensaje reenvía todo el contexto acumulado: con 200k encima, cada paso cuesta el doble que con 100k. Cuando la statusline se pone roja, hacé `/clear` y seguí. Si había una feature en curso, no perdés nada: al abrir la sesión nueva Claude recibe el «para retomar» (el pedido, el paso, el lote, lo último que falló y las decisiones) y escribir cualquier cosa alcanza para continuar. Si ves `nxy: feature <id> pendiente (…) — escribí cualquier cosa para seguir`, es eso. La sesión nueva arranca con unos miles de tokens en vez de cientos de miles.

## Mensajes de nxy y qué hacer con cada uno

Cuando nxy frena algo, el mensaje le dice a Claude exactamente qué hacer, así que casi siempre se resuelve solo. Esto es lo que vas a ver y cuándo te toca actuar:

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `nxy: no feature is active — the main thread does not edit code …` | Sin una feature activa, el hilo principal no edita código | Nada: Claude arranca una feature o despacha un implementer. Si de verdad conviene editar ahí: `/nxy:gate once` deja pasar una edición |
| `nxy: feature <id> is in progress — the main thread does not write code; …` | Hay una feature sin terminar: escribe el implementer, no el hilo principal | Nada. Para terminarla: `nxy feature stop` |
| `nxy read gate: main thread context is 150k > 100k — …` | Una búsqueda amplia con la sesión cargada: la hace un scout | Nada. Para dejar pasar una: `/nxy:gate once search`; para apagar el freno en este repo: `/nxy:gate reads off` |
| `nxy: feature <id> works in a worktree — edit in <ruta>` | La feature trabaja en un worktree y se intentó editar el checkout original | Nada: Claude edita en el worktree |
| Pregunta **«¿Aprobás el plan `<id>` r`<n>` de nxy?»** | Hay un plan esperando tu aprobación | Leelo (con **Ver completo** lo abrís entero) y elegí **Aprobar** o **Cambiar** |
| **«Lote n fallido»** | El lote falló después del reintento automático | **Reintentar** si es del cambio; **Seguir** si ya fallaba antes; **Parar** para mirarlo vos |
| **Afectados fallidos** / **Afectados sin verificar** | Los tests afectados fallaron, o no hay ninguno que correr | **Reintentar**; **Seguir** (queda marcado «sin verificar», nunca como verde); **Parar** |
| Pregunta de review con hallazgos `H1`, `H2`… | Hallazgos de la review de lo que cambió el plan | Marcá los que querés arreglar antes del PR, o «Ninguno». Los preexistentes son informativos |
| **Arreglos fallidos** | Los arreglos elegidos no quedaron en verde | **Reintentar**, **Seguir** o **Parar** |
| **Actualizar docs** / **Dejar** | Hay docs de usuario que nombran lo que cambió | **Actualizar docs** si querés que viajen en el mismo PR |
| «¿Guardar la regla …?» al cerrar | Las decisiones `[repo]` del plan o del diseño pueden quedar como reglas del repo | Guardá sólo las que valen para todo el repo; «Ninguna» no guarda nada |
| «Este repo no tiene reglas guardadas. ¿Las propongo ahora?» | Es la primera feature de un repo sin reglas | **Sí**: un scout lee el build, los linters y el CI, y te propone hasta 10 reglas para elegir (30–60k tokens, una sola vez por repo). **No** arranca la feature |
| «¿Con cuál seguimos?» | Dejaste notas a medias en esta carpeta | Elegí una, o «Ninguna, es otra cosa» |
| `nxy: feature <id> pendiente (…) — escribí cualquier cosa para seguir` | Hiciste `/clear` con una feature a medias | Escribí cualquier cosa |
| `nxy: feature <id> — … corriendo; su resultado llega cuando terminan` | Un agente corre en segundo plano | Nada: el resultado llega cuando termina |
| `nxy: si tenés la TUI abierta, contestá ahí` | Hay una pregunta de nxy esperándote | Contestá en la TUI, o `t` para que vuelva al diálogo normal |
| `nxy: the user stopped feature <id> from the TUI` | Paraste la feature con `x` en la TUI | Nada |
| `nxy: datos de la 1.x importados — …` | Primera sesión de un repo con datos de la 1.x: nxy los copió | Ver [Si venís de la 1.x](#si-venís-de-la-1x) |
| `nxy: la importación de la 1.x falló (…); probá \`nxy legacy import\`` | La copia de los datos de la 1.x falló | `nxy legacy import` la reintenta; `nxy legacy status` muestra el último error |
| `nxy: quedó a medias una feature de la 1.x en este repo: «<objetivo>» (…)` | La 1.x dejó un plan aprobado sin terminar | Elegí **Planificarla de nuevo** o **Ignorarla** |
| `nxy: la 1.x de nxy sigue activada: desactivala mientras probás la v2 (las dos responden a los mismos hooks)` | Hay otra instalación de nxy 1.x habilitada además de esta | Desactivala con `/plugin` |
| `nxy: <causa> - nxy hooks do nothing until this is fixed` | El lanzador no pudo bajar o correr el binario (sin `curl`, sin red, checksum que no coincide, binario no encontrado) | Ver [Si algo no anda](#si-algo-no-anda); si el binario desaparece o no corre en Windows, [Windows Defender](#windows-defender) |

## Comandos

| Comando | Para qué |
| --- | --- |
| `/nxy:feature <pedido>` | Arranca una feature: plan, aprobación, lotes, tests afectados, review |
| `/nxy:engineer <pedido>` | Lo mismo, pero decidís el diseño con Claude antes de planificar |
| `/nxy:locate <pregunta>` | Encuentra dónde está algo y devuelve `path:line`, sin llenar tu sesión de archivos. `--status`: qué buscadores hay |
| `/nxy:worktree …` | `list`, `new`, `pause`, `resume`, `close`: features en paralelo como worktrees de git |
| `/nxy:mem …` | La memoria: `show`, `rule`, `note`, `promote`, `edit`, `rm`, `restore`, `keep`, `search`, `related`, `export`, `import`, `bootstrap`. Ver [La memoria](#la-memoria) |
| `/nxy:review` | `show` (la última review), `status` (hallazgos por lente en 30 días), `escape "<texto>" --question "<q>"` (algo que la review no vio) |
| `/nxy:gate` | El freno: `status`, `once` (dejar pasar la próxima edición), `once search` (la próxima búsqueda amplia), `reads off\|on` (el freno de búsquedas; con `--repo`, sólo en este repo) |
| `/nxy:filter` | El filtro de comandos: `status`, `on`, `off` |
| `/nxy:stats` | La sesión actual al detalle: tokens, costo, forma, por modelo y subagente, y el tiempo de cada subagente. `last`, `<id>`, `--all`, `--json` |
| `/nxy:trend` | Evolución por día, semana, sesión, modelo o subagente, de todos tus proyectos. `--since 30d`, `--by week\|session\|model\|agent`, `--here`, `--json` |
| `/nxy:statusline` | Muestra el snippet; `install` la instala y `remove` la quita |
| `/nxy:setup` | Detecta rtk, rg, codegraph y d2 y los instala si le decís que sí |
| `/nxy:config` | Ver y cambiar la config: `show`, `set <clave> <valor\|default> [--repo]`, `cache-ttl 1h\|off`. Ver [Configuración](#configuración) |
| `/nxy:tui` | Muestra el comando para abrir la TUI en otra terminal |

Estos comandos corren en la terminal de tu sistema, sin Claude, con `nxy <comando>` (si `nxy` no está en el PATH, ver [La TUI](#4-la-tui-opcional)):

| Comando | Para qué |
| --- | --- |
| `nxy tui` | Abre la consola de control. `--demo` con datos de ejemplo; `--dump <pantalla> --width 80 --plain` imprime una pantalla como texto |
| `nxy view <pantalla> --json` | Lo que mostraría esa pantalla de la TUI con tus datos reales, como JSON. Sólo lee |
| `nxy status` | Dónde está la feature de esta carpeta |
| `nxy feature stop` | Termina la feature en cualquier punto |
| `nxy note save\|list\|done` | Notas para retomar en otra sesión |
| `nxy mem edit\|rm\|restore\|keep` | Cambiar, borrar, traer de vuelta o «dejar» una regla o un aprendizaje |
| `nxy legacy status\|import` | Qué entró de la 1.x y reintentar la importación |
| `nxy stats`, `nxy trend` | Los mismos reportes que `/nxy:stats` y `/nxy:trend`, sin que Claude los retipee ni gaste tokens |
| `nxy bench hook` | Mide cuánto tarda el hook real (ver [Desarrollo](#desarrollo)) |

Para ver un reporte tal cual, sin que Claude lo retipee, usá el prefijo `!` de Claude Code o corré el comando en una terminal.

## La statusline

Una línea siempre visible que responde tres preguntas: **¿cuánto me cuesta lo que Claude hace ahora?**, **¿cuándo conviene cortar?** y **¿qué pasa si dejo la sesión un rato?**

```
◆ nxy ⟡ my.app main ⟡ Opus 5 · high ⟡ ctx ▰▰▰▱▱▱▱▱ 41% · 82k ⟡ turno $0.42~ ⟡ sesión $34.74~ · 44M→241k · +4 agents ⟡ cache 98% · 52m ⟡ 5h 62% · 7d 31%
```

| Segmento | Qué es | Qué hacer con eso |
| --- | --- | --- |
| `my.app main` | Carpeta y rama | Confirmar que estás donde creés, sobre todo con varias sesiones abiertas |
| `Opus 5 · high` | Modelo y effort | Si es una tarea chica con Opus/high, cambialo al principio de la sesión: cambiar de modelo a mitad de camino reescribe toda la cache |
| `ctx … 41% · 82k` | Contexto que se reenvía en cada llamada, en **tokens reales**. Naranja desde 100k, rojo desde 200k | Naranja: no arranques nada grande. Rojo: `/clear` (la feature sigue en la base). Mirá los tokens, no el porcentaje: 41 % de 1M son 410k |
| `turno $0.42~` | Lo que va costando el turno actual, subagentes incluidos. Naranja desde $1, rojo desde $3 | **La señal más importante.** Un turno de $3 no es «trabajó mucho»: es que cada llamada arrastra demasiado contexto |
| `sesión $34.74~ · 44M→241k · +4 agents` | El odómetro: costo total, tokens de entrada → salida, subagentes | Comparar con lo que hiciste. El detalle, en `/nxy:stats` |
| `cache 98% · 52m` | Cuánto de lo enviado ya estaba en cache (se paga a un décimo) y cuánto le queda de vida | Si baja de golpe, algo cambió en el contexto. Los minutos dicen cuánto podés ausentarte sin costo |
| `❄386k $3.86~` | La cache **venció**: el próximo mensaje reescribe 386k tokens y eso cuesta $3.86 antes de hacer nada | Buen momento para preguntarte si seguís o arrancás una sesión nueva: cuesta lo mismo y arranca liviana |
| `5h 62% · 7d 31%` | Consumo de las ventanas de tu suscripción | Si vas al 90 % de la de 5 h, lo pesado puede esperar |

El `~` quiere decir *equivalente en USD*: con suscripción no pagás por token, pero es lo que ese uso costaría con API key. Un `*` después de un monto (en la statusline, `/nxy:stats` y `/nxy:trend`) quiere decir que un modelo todavía no está en la tabla de precios de nxy y se calculó con la versión más cercana de la misma familia (por ejemplo, un futuro `claude-sonnet-5-6` con las tarifas de `claude-sonnet-5-5`). Un `+?` quiere decir que el modelo es de una familia desconocida y no se pudo calcular. No tenés que hacer nada: el número se vuelve exacto cuando un release de nxy actualiza la tabla (la tabla va dentro del binario; nxy nunca sale a internet para actualizarla).

**Haiku 5.5 se cobra según el largo del prompt de cada pedido.** Hasta 100.000 tokens: $0,10 de entrada y $0,50 de salida por millón; si el prompt pasa de 100.000: $0,50 y $2,50. La cache cuenta en el largo (entrada + escrituras + lecturas de cache de ese pedido). nxy lo calcula pedido por pedido, así que un agente en Haiku (por ejemplo, si le cambiás el modelo a un rol) con un prompt largo cuesta ~5x por token.

Los umbrales son en tokens y no en porcentaje porque lo que encarece cada paso es cuánto se reenvía, no qué fracción de la ventana ocupa. Colores, presets (`vivid`, `classic`, `powerline`) y layout (`line`, `two-line`) se cambian en la [configuración](docs/configuracion.md#metricsstatusline) y se ven al instante. La statusline la dibuja `nxy statusline`, el mismo binario: lee sólo lo nuevo del transcript en cada refresco y, si algo falla, imprime `nxy ?` (con `NXY_DEBUG=1` ves la causa por stderr).

## Si venís de la 1.x

La v2 reemplaza a la 1.x: es otro programa (un binario en Go en vez de Node) con otro flujo (features con plan aprobado y lotes verificados por nxy, en vez del handoff y el orquestador). Tus datos de la 1.x no se pierden.

**Actualizar.** Con el mismo comando de siempre: `claude plugin marketplace update nxy-dev` y reinstalar. **Terminá las features de la 1.x antes de actualizar**: un plan de la 1.x a medias no se retoma tal cual (ver abajo).

**Qué se importa, y cuándo.** La primera vez que abrís Claude Code en cada repo con la v2, nxy copia lo que la 1.x sabía de ese repo y te lo cuenta en un aviso único:

```
nxy: datos de la 1.x importados — 12 convenciones → aprendizajes A1..A12 · 3 handoffs vivos → notas · 41 archivados → features cerradas · 30 sesiones del filtro · 26 reviews, 88 elegidos
```

- Las **convenciones, decisiones, bugs y preferencias** pasan a **aprendizajes `A<n>`**, con los archivos a los que estaban atadas. No son reglas todavía: `nxy mem promote A<n> --question "<qué resuelve>"` vuelve regla las que sigan valiendo.
- Los **handoffs vivos** pasan a **notas**; los **archivados**, a **features cerradas** que el planner encuentra por archivo.
- Las **memorias globales** entran una sola vez por máquina, como **aprendizajes globales** `A<n>` (el aviso dice `3 globales → aprendizajes globales A13..A15`). No se le entregan a ningún agente hasta que las promovés a reglas globales.
- Las **sesiones del filtro** y las **reviews** pasan al resumen que leen `nxy stats` y `nxy trend`. Lo que la 1.x siga escribiendo del filtro entra solo en el próximo arranque.
- Lo que no entra se cuenta en el mismo aviso (por ejemplo «N privadas sin importar»: las memorias marcadas `private` nunca se importan). Si el repo no tiene datos de la 1.x, no sale ningún aviso.

Es una copia: **nxy nunca escribe un archivo de la 1.x**. Si algo falla ves `nxy: la importación de la 1.x falló (…); probá \`nxy legacy import\``. Con `nxy legacy status` ves qué entró en cada repo y qué contestaste de las features a medias; `nxy legacy import` lo reintenta a mano (lo ya importado dice «ya importado el `<fecha>`»).

**Una feature de la 1.x a medias.** Si la 1.x dejó en ese repo un plan aprobado con lotes sin cerrar, ves un aviso con el objetivo del plan («faltan los lotes 2 y 3» o «aprobada, sin lotes corridos») y la pregunta «¿La planifico de nuevo con la v2?». **Planificarla de nuevo** arranca una feature de la v2 cuyo pedido es el objetivo de la 1.x, con su plan como referencia; **Ignorarla** la descarta. Si no contestás, se vuelve a preguntar en el próximo arranque; cada plan se pregunta una sola vez.

**El aviso de 1.x activada.** Si además de esta instalación hay otra instalación de nxy 1.x habilitada (por ejemplo, una de proyecto), ves `nxy: la 1.x de nxy sigue activada: desactivala mientras probás la v2 (las dos responden a los mismos hooks)`. Desactivala con `/plugin`: si no, las dos reaccionan al mismo comando.

**Tu config.** Los `config.json` de la 1.x siguen sirviendo: las claves que la v2 no usa se ignoran sin dar error (la lista, en [`docs/configuracion.md`](docs/configuracion.md#claves-de-la-1x-que-la-v2-ignora)).

**Volver a la 1.x.** La 1.x queda en la rama `v1` del repo. Para volver: clonala, sacá el marketplace actual y agregá el del clon.

```
git clone --branch v1 https://github.com/mananos/nxy.dev
```

```
/plugin marketplace remove nxy-dev
/plugin marketplace add <carpeta del clon>
/plugin install nxy@nxy-dev
```

Lo que hiciste en la v2 (features, reglas, aprendizajes) no vuelve a la 1.x, y la 1.x encuentra sus datos como los dejó. Detalle de la importación: [Datos de la 1.x](docs/como-funciona.md#datos-de-la-1x).

## Windows Defender

El binario de nxy no está firmado, y en Windows el antivirus puede ponerlo en cuarentena (por ejemplo con una detección genérica de aprendizaje automático).

- **Qué ves.** Un mensaje de nxy con `nxy: could not run <ruta> (exit …)` o `nxy: binary not found at <ruta>`, seguido de «nxy hooks do nothing until this is fixed» y de «Si en Windows el binario no corre o desaparece, puede que un antivirus (Windows Defender) lo haya puesto en cuarentena: volvé a compilarlo o bajarlo, o excluí su carpeta del antivirus». Claude también se entera y frena en vez de diagnosticar pasos de nxy.
- **Qué significa.** El binario no está o no se puede ejecutar, así que los hooks no hacen nada: nxy no filtra, no frena y no conduce features hasta que lo arregles. Tu código no corre ningún riesgo.
- **Qué hacés.** Una de estas tres:
  1. **Restauralo.** En Seguridad de Windows › Protección antivirus y contra amenazas › Historial de protección, buscá el elemento de nxy y restauralo (permitilo).
  2. **Excluí la carpeta** `%USERPROFILE%\.nxy\bin` (o `%NXY_HOME%\bin` si moviste nxy) en Seguridad de Windows › Protección antivirus y contra amenazas › Administrar la configuración › Exclusiones, y dejá que nxy baje el binario de nuevo. Desde un PowerShell de administrador: `Add-MpPreference -ExclusionPath "$env:USERPROFILE\.nxy\bin"`.
  3. **Compilalo vos.** Hace falta [Go](https://go.dev/dl/) (la versión que dice `go.mod`: 1.27). Hay dos formas.

     **Instalación remota** (sin clonar; funciona una vez que el tag `v2.0.0-rc.1` está publicado). Con Go 1.27, o uno más nuevo que baje solo esa versión:

     ```
     go install github.com/mananos/nxy.dev/v2/cmd/nxy@v2.0.0-rc.1
     ```

     El binario queda en `GOBIN` si la tenés definida, y si no en `$(go env GOPATH)/bin` (`nxy.exe` en Windows). Para que el lanzador lo use, apuntá `NXY_BIN` a ese archivo (en PowerShell: `$env:NXY_BIN = "…\nxy.exe"`), o copialo a `$HOME/.nxy/bin/2.0.0-rc.1/nxy.exe` (`$NXY_HOME/bin/…` si moviste nxy), la carpeta donde lo busca el lanzador. Ojo: un `NXY_BIN` olvidado corre un build viejo sin avisar.

     **Desde el clon.** Clonás el repo en el tag de la versión y lo compilás en la carpeta exacta donde lo busca el lanzador, así no baja nada:

     ```
     git clone --branch v2.0.0-rc.1 https://github.com/mananos/nxy.dev
     cd nxy.dev
     mkdir -p "$HOME/.nxy/bin/2.0.0-rc.1"
     go build -o "$HOME/.nxy/bin/2.0.0-rc.1/nxy.exe" ./cmd/nxy
     ```

     (En PowerShell, `mkdir -p` se reemplaza por `New-Item -ItemType Directory -Force "$HOME/.nxy/bin/2.0.0-rc.1"`.) La carpeta lleva la versión del plugin que instalaste; si moviste nxy con `NXY_HOME`, usá `$NXY_HOME/bin/…` en lugar de `$HOME/.nxy/bin/…`.
## Ver en qué se gasta

`/nxy:stats` explica una sesión y `/nxy:trend` muestra cómo evolucionás; la statusline lo resume en una línea y la pantalla **Gasto** de la TUI (con su Tendencia) lo muestra con barras. Cinco cosas para mirar:

**1. La forma de la sesión.** El costo se explica con dos números: cuánto contexto lleva cada llamada y cuántas llamadas hace cada turno.

| Dato | Qué hacer si está alto |
| --- | --- |
| `calls/turn` | 20–30 es Claude explorando o editando solo. No es malo, pero 30 llamadas × 130k son 4M de tokens en un turno: pedí tareas más acotadas o delegá la exploración |
| `main context avg` | Por encima de ~100k cada paso cuesta el doble que al principio: es la señal de cortar y seguir |
| `subagents %` | En 0 % en sesiones grandes, el principal hace todo con el contexto a cuestas. Con nxy este número tiene que subir |

**2. Por qué se enfrió la cache.** Casi todo lo que pagás es contexto leído de la cache con 90 % de descuento. Lo caro es cuando vence y hay que reescribirlo. nxy clasifica cada reconstrucción por causa: `idle` (una pausa más larga que la vida de la cache), `subagent` (el principal esperando a un subagente), `compact` y `start` (esperables) y `other` (cambió el prefijo con la cache viva, por ejemplo al cambiar de modelo). `avoidable by warming` suma `idle` y `subagent`: es el techo de lo que ahorraría una cache que no vence.

Si tu cuenta usa 5 minutos (API key, Bedrock, créditos extra), aparece una línea más con la cuenta hecha sobre tus propias sesiones:

```
cache TTL 1h, ¿y si…? (hilo principal): $3.02 → $1.65 (-45.3%), 5 reconstrucción(es) habrían sido lecturas · conviene: poné "promptCacheTtl": "1h" en ~/.claude/settings.json (Claude Code ≥ 2.1.242)
```

Con una cache de 1 hora cada escritura cuesta más (1,6× la de 5 minutos), pero las pausas de 5 a 60 minutos dejan de reescribir todo. nxy hace las dos cuentas y te dice cuál conviene: **conviene** (si la hora ahorra 1 % o más; `nxy config cache-ttl 1h` escribe la clave en `~/.claude/settings.json` por vos), **da casi igual** o **acá no conviene**. Con suscripción la sesión principal ya usa 1 hora y la línea no aparece.

**3. Cuánto ahorró el filtro.** `rtk saved` son los tokens que rtk recortó de las salidas de Bash. El ahorro grande está en tests, builds, `git`, `grep` y `ls`; leer código con `cat` no se recorta, a propósito.

**4. En qué se va el tiempo de cada subagente.** `/nxy:stats` imprime `SUBAGENT TIME` (y `/nxy:trend --by agent` lo hace sobre todo el período): para cada tipo de subagente, cuántas corridas hubo, la mediana, el p90 y el máximo, y cómo se repartió el tiempo de pared.

| Columna | Qué es |
| --- | --- |
| `model` | El modelo generando (porcentaje del tiempo) |
| `bash` | Comandos de Bash/PowerShell (porcentaje) |
| `tools` | Las demás herramientas: Read, Grep, Edit… (porcentaje) |
| `waiting` | Tiempo sin nada en curso (porcentaje) |
| `locate` | `X/N`: en cuántas de las N corridas se usó `nxy locate` |

Debajo van los comandos más lentos. Si el servicio frenó por límite de uso, esa pausa no se cuenta como trabajo del agente: aparece aparte como `usage-limit stops excluded: N` (sólo si N > 0). Qué hacer: si domina `bash`, mirá los comandos lentos (un `./mvnw test` de 4 minutos); si domina `waiting`, hay agentes esperando algo ajeno al modelo.

**5. Búsquedas amplias del principal (el freno de lectura).** Cuando el hilo principal, con el contexto en el umbral del gate (`gate.contextTokens`, 100k) o por encima, hace una búsqueda amplia (un `Grep` o `Glob` sin apuntar a un archivo, o `rg`, `grep -r`, `find`, `fd`, `Get-ChildItem -Recurse`, `Select-String` por Bash sin ruta de archivo), el hook la **frena**. Nunca frena un `Read` (ni con `offset`/`limit` ni entero), una búsqueda sobre un archivo, ni a un subagente.

- **Qué ves.** Un rechazo que empieza con `nxy read gate:`: manda un scout con la pregunta, o acotá la búsqueda a un archivo o a un `Read` con `offset`/`limit`. Claude suele mandar los scouts solo. La primera vez de cada sesión el mensaje suma una última línea: `To stop this in this repo: /nxy:gate reads off`.
- **Qué significa.** Con 100k encima cada búsqueda amplia arrastra todo el contexto y se queda con el resultado; un scout la hace en su propio contexto y devuelve sólo `path:line`.
- **Qué hacés.** Nada. Para dejar pasar **una** búsqueda: `/nxy:gate once search` (vale 5 minutos o hasta que se usa; no toca el permiso de edición de `/nxy:gate once`, ni al revés). Para apagarlo en este repo: `/nxy:gate reads off` (`reads on` lo vuelve a prender). Para ver cuántas frenó y cuántas dejó pasar: `/nxy:gate status`. Si `gate.enabled` o `gate.reads` es `false`, o no se puede leer el contexto, no frena.
- **Límite conocido.** Una carpeta con un punto en el nombre (por ejemplo `v1.2`) el chequeo rápido la toma por un archivo, así que una búsqueda sobre ella no se frena.

Además, `/nxy:gate status` y `/nxy:stats` cuentan desde los transcripts las búsquedas amplias hechas con el contexto en el umbral o por encima (incluidas sesiones pasadas): una cota superior de lo que costaron esas llamadas. Las que el freno rechazó no se cuentan ahí, porque no corrieron. Más sobre las métricas: [Las métricas](docs/como-funciona.md#las-métricas).

## La memoria

Lo que decidiste en una sesión se va con el `/clear`. La memoria de nxy es una base local (SQLite, sin servidor), en `~/.nxy/nxy.db`, que identifica cada repo por su remote de git (sin remote, por la ruta): el mismo repo es el mismo proyecto en otra carpeta, otra máquina o un worktree. Tiene tres cosas distintas:

| Qué | Para qué | Cómo se numera |
| --- | --- | --- |
| **Regla del repo** | Una decisión que las tareas futuras de este repo tienen que seguir. Siempre dice la pregunta que resuelve | `K3` |
| **Regla global** | Tuya, para todos tus repos («no commiteo desde el agente») | `G2` |
| **Aprendizaje** | Algo que notaste y todavía no es regla. Repetirlo suma «visto N veces», no crea otro | `A7` |

```
/nxy:mem rule "Los montos son BigDecimal" --question "¿Qué tipo se usa para dinero?" --paths "src/billing/**"
/nxy:mem rule "No commiteo desde el agente" --question "¿Quién commitea?" --global --roles implementer,reviewer
/nxy:mem note "El importador de CSV falla con BOM" --files src/import/csv.go
/nxy:mem promote A7 --question "¿Qué hacemos con el BOM del CSV?"
/nxy:mem show
/nxy:mem search "cómo guardamos los montos"
```

- **`show`** lista las reglas con `delivered N` (cuántas veces se entregó a un agente) y `cited M` (cuántas veces un reviewer la citó al encontrar que el cambio la rompía); `to review` marca las que llevan 90 días sin citarse. Debajo, las globales y los aprendizajes, con `seen N times`: los más vistos son candidatos a promover.
- **`edit <K3|G2|A7>`** cambia lo que pasás (`--text`, `--question`, `--paths`, `--roles`, `--files`) y deja todo lo demás: el número, las entregas y las citas. **`rm <ref>`** borra, **`restore <ref>`** lo trae de vuelta con el mismo número (que nunca se reutiliza), y **`keep <ref>`** marca una propuesta como «dejarla»: nxy no la vuelve a proponer.
- **Las globales** (`--global`, con `--roles planner,implementer,reviewer,main` para elegir quién las recibe; sin `--roles` las reciben todos). Llegan después de las `K` y, si hay una contradicción, la `K` gana. Nunca le llegan al scout, al refuter ni al documenter.
- **Qué recibe cada agente.** El planner, todas las reglas; el implementer, las del área que toca el lote y los aprendizajes de esos archivos; el reviewer, las reglas del diff. Con un tope de 3.000 caracteres (unos 750 tokens): lo que no entra queda en una línea «not shown (budget)». Un hallazgo que cita una regla sube a medium como mínimo.
- **Cómo se ofrecen.** Al cerrar una feature, nxy te propone guardar las decisiones `[repo]`. Y si con una feature activa escribís «siempre», «nunca» o «de ahora en adelante», nxy le pide a Claude que te pregunte si es una regla del repo: nunca la guarda sin que lo confirmes. En un repo sin reglas, la primera feature ofrece proponerlas (`/nxy:mem bootstrap` también). Los aprendizajes los puede sumar un implementer o un reviewer al terminar; uno que se comprueba falso, o cuyos archivos ya no existen, queda obsoleto (se marca, no se borra).
- **En la TUI**, la pantalla Memoria marca sólo lo raro (una regla entregada 10 veces o más y nunca citada, un aprendizaje visto 2 veces o más, uno obsoleto) y deja editar (`e`), borrar (`x`, con `u` para deshacer 5 segundos) y crear (`+`) sin escribir comandos.
- **Compartir con el equipo:** `/nxy:mem export` escribe un JSON versionado (`--out archivo`) que se commitea como cualquier cambio, y `/nxy:mem import <archivo>` sin `--apply` sólo muestra qué cambiaría (nuevo, cambiado, borrado, repetido); con `--apply` lo aplica en una sola transacción. Gana el más nuevo y los borrados viajan. Las notas temporales sólo viajan con `--notes`.
- **Tareas cerradas.** `/nxy:mem related <archivos>` lista las features cerradas y los aprendizajes que tocaron esos archivos; el planner lo corre antes de armar un plan.

Cuánto pesa: una regla ~0,5 KB y un aprendizaje ~0,3 KB; 100 reglas y 500 aprendizajes caben en ~1 MB de la base. Detalle: [La memoria](docs/como-funciona.md#la-memoria).

## Configuración

Casi nadie necesita tocar nada: los defaults están elegidos para que funcione sin calibrar. Hay tres niveles, y el de más abajo gana:

| Archivo | Alcance |
| --- | --- |
| Los defaults (van dentro del binario) | No se editan |
| `~/.nxy/config.json` | Vos, en todos tus proyectos. Es donde guarda `nxy config set` si no le pedís otra cosa |
| `<repo>/.nxy/config.json` | El repo; **se commitea** y lo hereda quien lo clone |

Lo que se cambia más seguido:

| Querés | Hacé |
| --- | --- |
| Ver cada clave, su valor y de dónde viene | `/nxy:config` (o `nxy config show`) |
| Que el planner de este repo use Sonnet (más barato) | `nxy config set roles.planner.model sonnet --repo` |
| Apagar el freno de escritura y el de búsquedas | `nxy config set gate.enabled false` |
| Apagar sólo el freno de búsquedas amplias | `/nxy:gate reads off` (o `"gate": { "reads": false }` en el JSON) |
| Apagar el filtro | `/nxy:filter off` (o `nxy config set modules.filter false`) |
| Probar la cache de 1 hora | `nxy config cache-ttl 1h` |
| Lentes de review propias del repo | Un `.md` en `.nxy/lenses/` (se commitea) |
| Tests afectados con tu propio runner | Un `.nxy/runners.json` en el repo |

La pestaña **Config** de la TUI muestra lo mismo con botones. **El modelo y el effort de cada agente** se cambian con `roles.<rol>.model` (`haiku`, `sonnet`, `opus`, `fable`) y `roles.<rol>.effort` (`low`, `medium`, `high`) para los 7 roles de la tabla de [Una feature, paso a paso](#una-feature-paso-a-paso); nxy lo aplica en cada despacho, y si el despacho ya trae un modelo o un effort, gana ese. Sin config valen los del encabezado de cada `agents/*.md`, que nxy manda explícitos (desde Claude Code 2.1.300 los subagentes ignoran el `model:` de su `.md` y usan el de la sesión principal). Editar los `.md` del plugin se pisa al actualizar: usá la config. Todas las claves, las variables de entorno y los comandos de instalación: [`docs/configuracion.md`](docs/configuracion.md) (los [roles](docs/configuracion.md#roles) en particular).

## Dónde guarda nxy sus cosas

Igual que `.claude`, hay una carpeta tuya y una por repo. `NXY_HOME` mueve `~/.nxy` a otra carpeta.

| Carpeta o archivo | Qué tiene | ¿Se commitea? |
| --- | --- | --- |
| `~/.nxy/nxy.db` | La base: features, memoria, notas, resumen del filtro, costos por agente | No: es de tu máquina |
| `~/.nxy/config.json` | Tu config global | No |
| `~/.nxy/bin/<versión>/` | El binario de esa versión (el lanzador lo baja la primera vez) y, si instalaste la statusline o la fila del PATH, una copia estable del lanzador (`~/.nxy/bin/nxy.cmd`, ~13 KB) | No |
| `~/.nxy/plans/` y `~/.nxy/reviews/` | El plan y la review de cada feature (`<id>.md`, 5 a 15 KB el plan), para **Ver completo**. Se borran al cerrar con éxito o al parar la feature | No |
| `~/.nxy/cache/` | Lo que nxy puede rehacer: el motor del filtro (`engine-v2.json`), la cache de reescrituras de rtk (`rewrite-v2.json`, hasta 500 comandos, unos 75 KB) y el estado de la statusline (`statusline-v2/`, ~7 KB por sesión, los de más de 7 días se borran) | No |
| `~/.nxy/run/` | Una señal `tui-<pid>` (~50 bytes) mientras una TUI está abierta; se borra al cerrarla | No |
| `~/.nxy/lenses/` | Tus lentes de review | No |
| `<repo>/.nxy/config.json`, `<repo>/.nxy/lenses/`, `<repo>/.nxy/runners.json` | Lo que el equipo comparte: config, lentes y runners de tests | **Sí** |
| `<repo>/.nxy/local/` | Lo de tu checkout: el índice de búsqueda (`index/`), el ledger del freno (`metrics/gate.jsonl`, tope 256 KB) y la marca de pausa de un worktree (`features/paused.json`) | No: va en `.gitignore` |

Borrar `.nxy/local/` de un repo descarta sólo esos datos del checkout (el índice se rehace solo). El estado de las features vive en `nxy.db`, no ahí. nxy también crea en cada repo donde lo necesita un `.codegraph/` (el índice de codegraph, ignorado en `.git/info/exclude`; no toca tu `.gitignore`). La referencia completa: [`docs/configuracion.md`](docs/configuracion.md).

## Si algo no anda

| Síntoma | Qué pasa | Qué hacer |
| --- | --- | --- |
| Ves `nxy: curl not found, cannot download the binary` o `nxy: download of nxy_… failed` | El lanzador no pudo bajar el binario (sin `curl`, sin red, o el release todavía no existe) | Instalá `curl` o revisá la red, y abrí una sesión nueva; o compilalo vos (ver [Windows Defender](#windows-defender), pasos de compilación) |
| Ves `nxy: checksum mismatch for …` | El archivo bajado no coincide con el SHA-256 del release | Probá de nuevo; si sigue, compilalo vos |
| Ves `nxy: downloading its binary in another hook - this hook was skipped` | Otro hook se está bajando el binario; sólo uno descarga a la vez | Nada: es de una sola vez |
| Los comandos no salen filtrados | rtk no está, está fuera del PATH, o tiene su propio hook | `/nxy:filter status` te dice cuál y cómo arreglarlo. Si lo instalaste con la terminal abierta, abrí una nueva |
| Aviso de que el hook de rtk está instalado | Corriste `rtk init -g` | Desinstalá el hook de rtk (`/nxy:setup` te dice cómo); nxy usa el suyo |
| Claude necesita la salida completa de un comando | El filtro recortó algo que hacía falta | Claude lo resuelve solo con `rtk recall <hash>`; como último recurso, `NXY_RAW=1 <comando>` |
| El freno de escritura frena todo | No hay feature activa, o la sesión pasó los 100k de contexto; es lo esperado | Para una edición puntual, `/nxy:gate once`; para ese repo, `nxy config set gate.enabled false --repo` |
| Un `Accept:` de un lote falla en Windows con `test -f …` | El planner escribe los `Accept:` para `sh`; en Windows sin `sh` se usa `cmd /c`, y ese comando no existe ahí | Lo ves como una falla con la salida, no escondida: usá Git Bash (con `sh` en el PATH) o pedí el cambio del `Accept:` |
| Mucho costo en `idle` o `subagent` | Tu cuenta usa cache de 5 minutos y cada pausa o subagente largo la enfría | Mirá la línea `cache TTL 1h, ¿y si…?` de `/nxy:stats`: si dice **conviene**, `nxy config cache-ttl 1h` |
| Algún hook se comporta raro | Los hooks de nxy fallan abiertos: ante cualquier error dejan pasar | `NXY_DEBUG=1` muestra la causa en stderr (`claude --debug`) |
| Checkout compartido entre Windows y WSL con codegraph | El lock de SQLite no cruza filesystems | `CODEGRAPH_DIR=.codegraph-win` en Windows y el default en WSL |

## Qué le agrega nxy a tu sesión

nxy también cuesta algo:

- **Contexto fijo:** las descripciones de 14 comandos, 7 agentes y 1 skill se suman al contexto de cada llamada. Su peso en tokens no está medido para la v2.
- **Tiempo por hook:** cada evento de Claude Code corre el lanzador y el binario. `nxy bench hook` mide cuánto tarda el hook real en tu máquina (mínimo, mediana, p90 y máximo, contra un umbral de 50 ms) con el filtro apagado, prendido sin cache y prendido con cache; no hay números medidos para otros sistemas. El filtro recuerda la respuesta de rtk por comando, así que un comando repetido no arranca rtk.
- **Tokens de los hooks:** cero cuando no frenan nada. Cuando frenan, el mensaje.
- **Disco:** ver [Dónde guarda nxy sus cosas](#dónde-guarda-nxy-sus-cosas). Por feature, unos pocos KB en la base; por cada subagente una fila de costos que se borra a los 90 días.

## Estado del proyecto

| Parte | Estado |
| --- | --- |
| Versión actual | `2.0.0-rc.1` (candidata a 2.0.0: pensada para probarla en uso real) |
| Métricas, statusline, filtro con rtk, scout y freno | Reescritos en Go con los mismos resultados que la 1.x (probados contra los golden de la 1.x) |
| Features con plan, lotes, tests afectados, review, modo ingeniero, memoria con reglas, varios repos y worktrees | Nuevo en la v2 |
| La TUI y contestar desde ella | Nueva en la v2 |
| Importación de los datos de la 1.x | Nueva en la v2 |

## Desarrollo

nxy es Go (la versión de `go.mod`) sin dependencias de runtime. El plugin vive en la raíz del repo.

```
go build -o <carpeta>/nxy.exe ./cmd/nxy        # sin el .exe en Linux y macOS
NXY_BIN=<carpeta>/nxy.exe claude --plugin-dir .
go vet ./... && go test ./...
```

`NXY_BIN` le dice al lanzador que corra ese binario en lugar de bajar el de la versión (en PowerShell: `$env:NXY_BIN = "<carpeta>/nxy.exe"`). Ojo: un `NXY_BIN` olvidado corre un build viejo sin avisar; para probar el camino de la descarga, sacalo. Con la 1.x instalada, desactivala mientras cargás el repo con `--plugin-dir`: las dos responderían a los mismos hooks.

- **Golden files.** Muchos tests comparan contra archivos de `testdata/golden/`. `NXY_WRITE_GOLDEN=1 go test ./...` reescribe los que cambiaron; revisá el diff antes de commitear.
- **El hook.** `nxy bench hook [-n 20] [-command "git status"] [-shim bin/nxy.cmd] [-bin <nxy>]` corre el hook real y muestra mínimo, mediana, p90 y máximo en ms contra el umbral de 50 ms. Siempre sale con 0; el CI imprime la tabla en el resumen del job en Linux y Windows.
- **El lanzador.** `bin/nxy.cmd` es un archivo que sirve a la vez para PowerShell/cmd y para sh. Baja el binario con un tope de 8 s en total (6 s el binario, 2 s los checksums), por debajo del timeout de 10 s de cada hook. Sólo un hook descarga a la vez (una carpeta `nxy.exe.lock` al lado del destino); los otros imprimen una línea `nxy:` y no hacen nada. Un lock de más de un minuto y los temporales de más de un día, de corridas muertas, se borran al próximo arranque. **Límite conocido:** el SHA-256 de `checksums.txt` sale del mismo release que el binario: protege de corrupción y truncado, no de un release comprometido. Una firma verificada contra una clave fija (cosign o minisign) es un paso posterior.
- **Releases.** Un tag `v2.*` dispara el workflow de release: verifica que el tag coincida con la versión fijada en `bin/nxy.cmd`, corre `go vet` y `go test` y publica con GoReleaser los binarios `nxy_<versión>_<os>_<arch>[.exe]` (Windows, Linux y macOS, amd64 y arm64) y `checksums.txt`. La versión del binario sale del tag (`internal/version`).
- **El CI** corre `go vet ./...` y `go test ./...` en Ubuntu y Windows.

Cómo está organizado el código, y las reglas de capas que un test hace cumplir: [Estructura del código](docs/como-funciona.md#estructura-del-código). Soportar otro agente de código es escribir otro adaptador en `internal/host/`, sin tocar los casos de uso.

## Créditos

El código que lee los transcripts (`internal/host/claudecode/transcript`) deriva del plugin `session-report` de Anthropic (Apache-2.0; análisis de transcripts, deduplicación de pedidos, atribución de subagentes y detección de reconstrucciones de cache). El filtrado lo hace [RTK](https://github.com/rtk-ai/rtk) (Apache-2.0), un binario externo que nxy no incluye. Ver `NOTICE`.
