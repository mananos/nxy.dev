# Review

Cuando los lotes están en verde, nxy revisa lo que cambió, te muestra los hallazgos y vos elegís cuáles se arreglan antes del PR.

## Para qué sirve

Una segunda mirada, con la profundidad que pide el riesgo del cambio, sobre lo que realmente quedó en el diff (no sobre lo que el plan decía que iba a pasar). Los hallazgos llegan numerados, con su escenario y su arreglo, y la decisión de qué arreglar es tuya. También sirve para avisar de lo que la review no vio (`escape`) y para actualizar los docs que nombran lo cambiado.

## Qué ves

Después de los tests afectados, la feature pasa a **Revisando** y, si hay hallazgos, a **Necesita tu decisión**: una pregunta (hasta 4 grupos de 4 hallazgos) con `H1`, `H2`… y su severidad y ubicación. En la TUI se contesta desde la pantalla de decisión ([cómo se ve](decidir.md)).

Al final de la feature, `/nxy:review show` te muestra la última review completa, y `/nxy:review status` cuántos hallazgos eligió o ignoró cada lente en los últimos 30 días.

## Qué significa

### Cuánta review hay

La profundidad depende del riesgo del plan, que nxy vuelve a calcular con los archivos reales del diff (sube, nunca baja):

| Riesgo | Qué corre |
| --- | --- |
| **Bajo** | Un `reviewer-quick`: una pasada con la lente base |
| **Medio** | Un `reviewer` por cada lente que aplica a los archivos cambiados |
| **Alto** | Lo mismo, y además un `refuter`: intenta refutar con el código cada hallazgo de severidad alta antes de ofrecértelo |

Una lente es una lista de cosas a mirar que se activa según qué archivos cambiaron (por ruta o por contenido). Las que trae nxy:

| Lente | Se activa con |
| --- | --- |
| `base` | Siempre: que el cambio sirva al plan, que cada `Accept:` pruebe lo que dice, que no se reimplemente lo que ya existe, que se respeten las reglas del repo, que haya tests, que no queden restos |
| `persistence` | Repositorios y entidades (JPA, ORM, DAO) |
| `api` | Controllers, rutas, DTO |
| `frontend` | Componentes Angular, React, Solid |
| `database` | SQL y migraciones |
| `security` | Auth, tokens, pagos, criptografía, contraseñas |
| `contract` | Cuando la feature toca **dos o más repos**: compara lo que expone un lado con lo que consume el otro y cita ambos |

Las lentes `security` y `database` suben el riesgo a **alto**; `api` y `contract`, a **medio**. Si en el plan ves «subido por nxy», es esto.

### Los hallazgos

- Cada hallazgo trae severidad (alta, media o baja), lugar, escenario concreto y arreglo propuesto. Si cita una regla del repo (`K3`) o tuya (`G2`), sube a media como mínimo.
- Los que **no causó el cambio** (preexistentes) son informativos: no se ofrecen para arreglar.
- Los que el refuter refutó no se ofrecen. Si la refutación es dudosa, el hallazgo queda.
- Un hallazgo sin `path:línea` o sin un escenario «entrada → resultado» se descarta antes de que lo veas.
- «Lo causó el cambio» quiere decir que cae a 3 líneas o menos de un tramo modificado.
- Con más de 16 hallazgos, ves los más graves y nxy dice cuántos quedaron afuera.

### Docs

Al final, nxy busca (por código, sin gastar tokens) documentos para usuarios que nombren lo que cambió: un `.md` en la raíz, un README, o lo que esté en `docs/`, `doc/`, `documentation/` o `wiki/`. Nombrar significa que aparece el nombre de un archivo cambiado, sin extensión, como palabra entera; si el nombre es una sola palabra simple (`branch`, `prompt`), sólo cuenta entre backticks, con extensión (`branch.go`) o como llamada (`branch(`), y los compuestos (`ClienteService`, `cliente.component`) siguen contando como palabra entera (nombres genéricos como `index` o `utils` no cuentan). Si encuentra alguno, te ofrece **Actualizar docs** o **Dejar**; con lo primero, un `documenter` actualiza sólo esos archivos y te cuenta qué cambió y qué dejó igual. Si no hay ninguno, no te pregunta.

## Qué hacer

- **Elegí qué arreglar.** Marcá los hallazgos que querés antes del PR, o **Ninguno**. Un único implementer los arregla y nxy vuelve a correr los tests afectados. Si no quedan en verde ves **Arreglos fallidos**: **Reintentar**, **Seguir** o **Parar**.
- **Reportá lo que la review no vio.** Si después encontrás un problema que ningún lente marcó:

  ```
  /nxy:review escape "falta validar el CUIT vacío" --question "¿Qué pasa con un CUIT vacío?"
  ```

  Siempre lleva la pregunta que la regla resuelve (`--question`). Con una feature activa, queda como candidato para la pregunta de cierre; sin feature, se guarda ya como regla del repo. `--lens base` dice qué lente tendría que haberlo visto, y `/nxy:review status` lo cuenta como «escape» de esa lente.
- **Lentes propias.** Un `.md` en `.nxy/lenses/` del repo (se commitea) o en `~/.nxy/lenses/` suma o pisa una lente. Si un archivo es inválido, nxy te avisa y lo ignora.
- **Mirar la review completa:** **Ver completo** en la pregunta, o `/nxy:review show`.

## Comandos y config

| Comando | Qué hace |
| --- | --- |
| `/nxy:review show` | La última review; `todavía no hay review` si no hubo |
| `/nxy:review status` | Elegidos, ignorados y escapes por lente, últimos 30 días |
| `/nxy:review escape "<texto>" --question "<q>" [--lens base]` | Reporta algo que se le escapó |
| `nxy review show\|status\|escape` | Lo mismo desde la terminal |

Qué modelo usa cada reviewer se cambia con `roles.reviewer.model`, `roles.reviewer-quick.model`, `roles.refuter.model` y sus `effort` ([Configuración](configuracion.md)).

## Relacionados

- [Una feature, paso a paso](feature.md)
- [Plan y lotes](plan-y-lotes.md)
- [Varios repos y worktrees](varios-repos.md): la lente de contrato entre repos.
- [Memoria](memoria.md): las reglas que citan los hallazgos y los escapes que se vuelven reglas.
