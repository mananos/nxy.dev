# Ver en qué se gasta

La statusline, `/nxy:stats`, `/nxy:trend` y la pantalla Gasto de la TUI te muestran cuánto cuesta cada sesión, por qué, y cuándo conviene cortar.

## Para qué sirve

En Claude Code el costo no se ve y se acumula: cada mensaje reenvía todo el contexto, así que con 200k de contexto cada paso cuesta el doble que con 100k. nxy lee los transcripts de tus sesiones (sólo lee: nunca los modifica) y los convierte en números: cuánto cuesta lo que Claude hace ahora, cuánto se fue en lo que ya pasó y qué lo explica. Con esos números decidís cuándo cortar con `/clear`, cuándo delegar a un subagente y si te conviene la cache de 1 hora.

## Qué ves

**La statusline**, siempre visible, responde tres preguntas: ¿cuánto me cuesta lo que Claude hace ahora?, ¿cuándo conviene cortar? y ¿qué pasa si dejo la sesión un rato?

```
◆ nxy ⟡ my.app main ⟡ Opus 5 · high ⟡ ctx ▰▰▰▱▱▱▱▱ 41% · 82k ⟡ turno $0.42~ ⟡ sesión $34.74~ · 44M→241k · +4 agentes ⟡ cache 98% · 52m ⟡ 5h 62% · 7d 31%
```

**La pantalla Gasto de la TUI**, con la sesión y su tendencia:

![La pantalla Gasto de la TUI: la sesión y la tendencia](assets/gifs/tui-gasto.gif)

La pestaña **Sesión** trae cuatro tarjetas (gasto, cache, llamadas, turnos), una tabla por modelo (llamadas, entrada, salida, cache, costo), la «forma de la sesión» y lo que ahorró el filtro. La pestaña **Tendencia** agrupa los últimos días por día, semana, modelo o agente, con barras.

**`/nxy:stats`** explica una sesión al detalle y **`/nxy:trend`** muestra cómo evolucionás entre sesiones; los dos imprimen un informe de texto que Claude te copia tal cual.

## Qué significa

### La statusline, segmento por segmento

| Segmento | Qué es | Qué hacer con eso |
| --- | --- | --- |
| `my.app main` | Carpeta y rama | Confirmar que estás donde creés, sobre todo con varias sesiones abiertas |
| `Opus 5 · high` | Modelo y effort | Si es una tarea chica con Opus y high, cambialo al principio de la sesión: cambiar de modelo a mitad de camino reescribe toda la cache |
| `ctx … 41% · 82k` | El contexto que se reenvía en cada llamada, en **tokens reales**. Naranja desde 100k, rojo desde 200k | Naranja: no arranques nada grande. Rojo: `/clear` (la feature sigue en la base). Mirá los tokens, no el porcentaje: 41 % de 1M son 410k |
| `turno $0.42~` | Lo que va costando el turno actual, subagentes incluidos. Naranja desde $1, rojo desde $3 | **La señal más importante.** Un turno de $3 no es «trabajó mucho»: es que cada llamada arrastra demasiado contexto |
| `sesión $34.74~ · 44M→241k · +4 agentes` | El odómetro: costo total, tokens de entrada → salida y subagentes | Comparalo con lo que hiciste. El detalle, en `/nxy:stats` |
| `cache 98% · 52m` | Cuánto de lo enviado ya estaba en cache (se paga a un décimo) y cuánto le queda de vida | Si baja de golpe, algo cambió en el contexto. Los minutos dicen cuánto podés ausentarte sin costo |
| `❄386k $3.86~` | La cache **venció**: el próximo mensaje reescribe 386k tokens y eso cuesta $3.86 antes de hacer nada | Buen momento para preguntarte si seguís o arrancás una sesión nueva: cuesta lo mismo y arranca liviana |
| `5h 62% · 7d 31%` | Cuánto consumiste de las ventanas de tu suscripción | Si vas al 90 % de la de 5 horas, lo pesado puede esperar |

El `~` quiere decir **equivalente en USD**: con suscripción no pagás por token, pero es lo que ese uso costaría con API key. Un `*` después de un monto (en la statusline, `/nxy:stats` y `/nxy:trend`) quiere decir que un modelo todavía no está en la tabla de precios de nxy y se calculó con la versión más cercana de la misma familia. Un `+?` quiere decir que el modelo es de una familia desconocida y no se pudo calcular. No tenés que hacer nada: el número se vuelve exacto cuando un release de nxy actualiza la tabla (la tabla va dentro del binario; nxy nunca sale a internet para actualizarla).

**Haiku 5.5 se cobra según el largo del prompt de cada pedido.** Hasta 100.000 tokens: $0,10 de entrada y $0,50 de salida por millón; si el prompt pasa de 100.000: $0,50 y $2,50. La cache cuenta en el largo. nxy lo calcula pedido por pedido, así que un agente en Haiku con un prompt largo cuesta unas 5 veces más por token.

Los umbrales son en tokens y no en porcentaje porque lo que encarece cada paso es cuánto se reenvía, no qué fracción de la ventana ocupa.

### Cuando la sesión se pone cara

Cuando el `ctx` se pone rojo o el turno pasa de $3, hacé `/clear` y seguí. Si había una feature en curso, no perdés nada: al abrir la sesión nueva Claude recibe el «para retomar» (el pedido, el paso, el lote, lo último que falló y las decisiones) y escribir cualquier cosa alcanza para continuar; vas a ver `nxy: feature <id> pendiente (…) — escribí cualquier cosa para seguir`. La sesión nueva arranca con unos miles de tokens en vez de cientos de miles.

### Lo que explica `/nxy:stats`

El informe es de texto y sigue este orden:

1. **Encabezado y `forma:`.** El costo se explica con dos números: las `llamadas/turno` y el `contexto promedio del principal` por llamada. 20 a 30 llamadas por turno es Claude explorando o editando solo; no es malo, pero 30 llamadas con 130k de contexto son 4M de tokens en un turno: pedí tareas más acotadas o delegá la exploración. Por encima de ~100k de contexto cada paso cuesta el doble que al principio: es la señal de cortar. El porcentaje de `subagentes` en 0 % en una sesión grande dice que el principal hace todo con el contexto a cuestas; con nxy tiene que subir.
2. **`TOKENS`.** Entrada sin cache, escrituras de cache (5 minutos y 1 hora), lecturas, salida, aciertos de cache, pico de contexto y costo, separados en total, principal y subagentes.
3. **Subagentes y `TIEMPO DE SUBAGENTES`.** Para cada tipo de agente: cuántas corridas, la mediana, el p90 y el máximo, y cómo se repartió el tiempo entre `modelo`, `bash`, `herram.` y `espera`, más en cuántas corridas usó `nxy locate`. Debajo, los comandos más lentos. Si dominan `bash`, mirá los comandos lentos (un `./mvnw test` de 4 minutos); si domina `espera`, hay agentes esperando algo ajeno al modelo. Un corte por límite de uso no cuenta como trabajo del agente: aparece como `cortes por límite de uso excluidos: N`.
4. **`FILTRO`.** Qué filtró nxy de las salidas de Bash y, con rtk, `rtk ahorró:` cuántos tokens recortó. El ahorro grande está en tests, builds, `git`, `grep` y `ls`; leer código con `cat` no se recorta, a propósito ([El filtro](rtk.md)).
5. **`QUIEBRES DE CACHE`.** Casi todo lo que pagás es contexto leído de la cache con 90 % de descuento. Lo caro es cuando vence y hay que reescribirlo. nxy clasifica cada reconstrucción por causa: `idle` (una pausa más larga que la vida de la cache), `subagent` (el principal esperando a un subagente), `compact` y `start` (esperables) y `other` (cambió el prefijo con la cache viva, por ejemplo al cambiar de modelo). `evitable calentando` suma `idle` y `subagent`: es el techo de lo que ahorraría una cache que no vence.
6. **Búsquedas amplias.** Cuántas búsquedas amplias hizo el hilo principal con el contexto por encima del umbral del [freno de lectura](gate.md), y cuánto contexto arrastraron (una cota superior).

### ¿Me conviene la cache de 1 hora?

Si tu cuenta usa cache de 5 minutos (API key, Bedrock, créditos extra), el informe agrega una línea con la cuenta hecha sobre tus propias sesiones:

```
cache TTL 1h, ¿y si…? (hilo principal): $3.02 → $1.65 (-45.3%), 5 reconstrucción(es) habrían sido lecturas · conviene: poné "promptCacheTtl": "1h" en ~/.claude/settings.json (Claude Code ≥ 2.1.242)
```

Con una cache de 1 hora cada escritura cuesta más (1,6 veces la de 5 minutos), pero las pausas de 5 a 60 minutos dejan de reescribir todo. nxy hace las dos cuentas y te dice cuál conviene: **conviene** (si la hora ahorra 1 % o más), **da casi igual** o **acá no conviene**. Con suscripción, la sesión principal ya usa 1 hora y la línea no aparece.

### Qué le agrega nxy a tu sesión

nxy también cuesta algo, y es mejor que lo sepas:

- **Contexto fijo:** las descripciones de 14 comandos, 7 agentes y 1 skill se suman al contexto de cada llamada. Su peso en tokens no está medido para la v2.
- **Tiempo por hook:** cada evento de Claude Code corre el lanzador y el binario. `nxy bench hook` mide cuánto tarda en tu máquina (mínimo, mediana, p90 y máximo, contra un umbral de 50 ms) con el filtro apagado, prendido sin cache y prendido con cache; no hay números medidos para otros sistemas. El filtro recuerda la respuesta de rtk por comando, así que un comando repetido no arranca rtk.
- **Tokens de los hooks:** cero cuando no frenan nada; cuando frenan, el mensaje.
- **Disco:** unos pocos KB en la base por feature, y por cada subagente una fila de costos que se borra a los 90 días. Qué hay en cada carpeta: [Configuración](configuracion.md).

## Qué hacer

```
/nxy:stats
/nxy:stats last --all
/nxy:trend --since 14d --by model
/nxy:statusline install
```

- **`/nxy:stats [current|last|<id>] [--all] [--json]`**: sin argumentos, la sesión actual (la última, si nxy no sabe cuál es la actual). `--all` busca en todos los proyectos, no sólo en este; `--no-agents` deja afuera a los subagentes.
- **`/nxy:trend [--since <período>] [--by day|week|session|model|agent] [--here] [--json]`**: por defecto, los últimos 30 días agrupados por día. El período es `7d`, `24h`, `30m` o una fecha ISO. `--here` limita a este proyecto. `--by agent` te dice cuánto y cuándo gasta cada rol.
- **`/nxy:statusline install`** la instala (ver [Instalación](instalacion.md)); sin argumentos sólo muestra el bloque y si ya está instalada; `remove` la saca, y sólo si es la de nxy.
- **`nxy config cache-ttl 1h`** escribe `promptCacheTtl` en `~/.claude/settings.json` (con un backup al lado) cuando el informe dice que conviene; `nxy config cache-ttl off` lo quita. Claude Code lo toma en la próxima sesión.
- **En la TUI:** pestaña Gasto → `tab` pasa a Tendencia, `enter` ahí cambia el agrupado (día, semana, modelo, agente), `shift+tab` vuelve a la sesión y `s` lista las otras sesiones con su costo ([La TUI](tui.md)).

La statusline la dibuja `nxy statusline`, el mismo binario: lee sólo lo nuevo del transcript en cada refresco. Si algo falla, imprime `nxy ?`; con `NXY_DEBUG=1` ves la causa por stderr.

## Comandos y config

| Comando | Qué hace |
| --- | --- |
| `/nxy:stats [current\|last\|<id>] [--all] [--no-agents] [--json]` | Informe de una sesión |
| `/nxy:trend [--since 7d] [--by day\|week\|session\|model\|agent] [--here] [--json]` | Tendencia entre sesiones |
| `/nxy:statusline [install\|remove]` | Muestra, instala o saca la statusline |
| `nxy config cache-ttl 1h\|off` | Prende o apaga la cache de 1 hora en Claude Code |
| `nxy bench hook` | Mide cuánto tarda un hook en tu máquina |

Claves de `metrics` en tu config (`~/.nxy/config.json`); los colores, presets y layout se ven al instante:

| Clave | Qué hace |
| --- | --- |
| `metrics.subscription` | `true` muestra los montos como equivalente (`$1.20~`) |
| `metrics.cacheBreakThreshold` | Desde cuántos tokens una reescritura de cache cuenta como «reconstrucción» (por defecto 100000) |
| `metrics.statusline.preset` | `vivid`, `classic` o `powerline` |
| `metrics.statusline.layout` | `line` o `two-line` |
| `metrics.statusline.ctxWarnTokens` · `ctxCritTokens` | Naranja y rojo del contexto (100000 y 200000) |
| `metrics.statusline.turnWarnUsd` · `turnCritUsd` | Naranja y rojo del turno (1 y 3) |

Todas las claves y sus defaults están en [Configuración](configuracion.md).

## Relacionados

- [La TUI](tui.md): la pantalla Gasto y su Tendencia.
- [Buscar](buscar.md): una búsqueda con scout evita arrastrar archivos al contexto.
- [Instalación](instalacion.md): instalar la statusline.
- [Una feature, paso a paso](feature.md): `/clear` no pierde la feature.
- [Configuración](configuracion.md)
