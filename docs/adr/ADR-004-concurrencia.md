# ADR-004: Concurrencia y consistencia de las reglas de negocio

- **Estado:** Aceptada (implementada y probada en planner-service, forum-service y users-service)
- **Fecha:** 2026-10-29
- **Decisión del enunciado:** D4
- **Reemplaza a:** —

## Contexto

El enunciado (D4) pide decidir cómo se mantienen las reglas de negocio cuando varios pedidos modifican los mismos datos al mismo tiempo, y demostrarlo. En Estudiario eso pasa en tres lugares:

- **Plan de estudio (acción principal).** Un estudiante puede confirmar el mismo plan desde dos dispositivos, reintentar una confirmación por un corte de red, o confirmar dos planes de distintas materias a la vez. Las reglas son: ninguna sesión se superpone con otra ya reservada (RN-12), no se supera la capacidad diaria (RN-13), una confirmación no se procesa dos veces (RN-14) y un examen tiene como máximo un plan confirmado (RN-15). CA-08 a CA-11 son críticos.
- **Moderación del foro.** Dos administradores pueden aprobar o rechazar la misma publicación a la vez (RN-23, CA-18), y cada decisión tiene que generar exactamente un evento (RN-32).
- **Registros únicos.** Un mail no se registra dos veces (RN-01), un estudiante califica una publicación una sola vez (RN-30) y un sistema externo no registra dos veces el mismo documento (RN-40).

Lo que ya está decidido y este ADR toma como base:

- **[ADR-001](ADR-001-limites-de-servicios.md):** cada servicio es dueño de sus datos y no hay transacciones entre servicios.
- **[ADR-003](ADR-003-persistencia.md):** planner-service usa PostgreSQL porque puede hacer cumplir las reglas por sí mismo, y forum-service usa MongoDB con actualizaciones atómicas sobre un documento.
- **[ADR-012](ADR-012-balanceo-de-carga.md):** forum-service corre en dos instancias, y cualquier servicio puede escalar a más. Un *lock* en la memoria de un proceso no protege nada si el otro pedido llega a otra instancia.

## Opciones consideradas

**Dónde se controla la concurrencia**

1. **En la aplicación, con *locks* en memoria** (`sync.Mutex` por estudiante o por publicación). Es simple, pero deja de funcionar apenas hay dos instancias, y un reinicio en el medio de una operación no deja rastro.
2. **Con un *lock* distribuido** (por ejemplo, en Redis o con una tabla de *locks*). Funciona con varias instancias, pero suma un componente, tiempos de expiración que ajustar y el riesgo de que el *lock* venza mientras la operación sigue.
3. **En la base de datos de cada servicio (elegida).** Transacciones, bloqueos de fila, restricciones e índices únicos y actualizaciones condicionales. La base ya es el único lugar por donde pasan todas las instancias, y una regla que cumple el motor se cumple aunque la aplicación tenga un error.

**Cómo se trata un conflicto en planner-service**

1. **Concurrencia optimista** (versión en el plan y reintento si cambió). Sirve para un registro, pero RN-12 y RN-13 dependen de **todas** las sesiones del estudiante, no de una fila con versión.
2. **Aislamiento `SERIALIZABLE`.** PostgreSQL detecta solo los conflictos, pero aborta transacciones que hay que reintentar, y bajo carga los abortos crecen.
3. **Bloqueo pesimista del estudiante más restricciones del motor (elegida).** La confirmación bloquea la fila del estudiante (`SELECT … FOR UPDATE`), así las confirmaciones del mismo estudiante se procesan de a una y la validación ve siempre el calendario real. Las restricciones de la base son la última barrera.

**Cómo se trata un conflicto en forum-service**

1. **Leer, validar y escribir** el documento entero. Entre la lectura y la escritura otro administrador puede moderar, y gana el último en escribir.
2. **Transacciones multi-documento de MongoDB.** Necesitan un *replica set* y no hacen falta, porque cada regla afecta a un solo documento.
3. **Actualizaciones condicionales sobre un documento e índices únicos (elegida).** MongoDB garantiza que una actualización sobre un documento es atómica: el filtro y el cambio se aplican juntos.

## Decisión

El control de la concurrencia está **siempre en la base de datos del servicio dueño del dato**. Ningún servicio usa *locks* en memoria para proteger una regla de negocio, así que todo vale igual con una instancia o con varias.

### 1. Confirmar un plan (planner-service, PostgreSQL)

La confirmación es **una sola transacción** (`Repository.Confirm`):

```mermaid
sequenceDiagram
    participant A as Pedido A
    participant B as Pedido B (mismo estudiante)
    participant DB as PostgreSQL
    A->>DB: BEGIN; SELECT … FROM students WHERE id = $1 FOR UPDATE
    B->>DB: BEGIN; SELECT … FROM students WHERE id = $1 FOR UPDATE
    Note over B,DB: B espera: la fila está bloqueada por A
    A->>DB: ¿ya existe la confirmación con esta Idempotency-Key?
    A->>DB: el plan está "propuesto" y el examen no tiene otro confirmado
    A->>DB: lee las sesiones reservadas y valida RN-10 a RN-13
    A->>DB: INSERT sesiones, UPDATE plan, INSERT confirmación
    A->>DB: COMMIT (libera la fila)
    Note over B,DB: B sigue y ve lo que A guardó
    B->>DB: misma clave → devuelve el resultado de A<br/>otra clave → 409, el plan ya está confirmado
```

| Regla | Qué la garantiza |
|---|---|
| **RN-12** (sin superposición) | La validación de la aplicación dentro de la transacción y, como última barrera, la restricción `EXCLUDE USING gist (student_id WITH =, tstzrange(starts_at, ends_at) WITH &&)` sobre las sesiones que ocupan el calendario. Si la aplicación fallara, la base rechaza la sesión (`23P01`) y la transacción entera vuelve atrás. |
| **RN-13** (capacidad diaria) | La validación ve todas las sesiones reservadas del estudiante, y nadie más puede reservar mientras tanto porque la fila del estudiante está bloqueada. |
| **RN-14** (sin duplicar) | La tabla `confirmations` con clave primaria `(student_id, idempotency_key)` y el resultado guardado. Un reintento con la misma clave devuelve el mismo resultado sin tocar las sesiones. Con la misma clave para **otro** plan responde `422`. |
| **RN-15** (un plan confirmado por examen) | El índice único parcial `plans_one_confirmed_per_exam ON plans (exam_id) WHERE status = 'confirmado'`. |
| **Todo o nada** (CA-07) | Si cualquier sesión falla, `ROLLBACK`: no queda ninguna sesión reservada a medias. |

Las violaciones que detecta el motor se traducen a errores del dominio y no a `500`: `23P01` es la regla RN-12 incumplida (`422`), y `23505` sobre el índice del plan o la confirmación es `409`.

- **Por qué se bloquea al estudiante y no "las filas de esos días"**, como anticipaba el ADR-003: las sesiones nuevas todavía no existen, así que no hay filas que bloquear para los días vacíos. Bloquear al estudiante (una fila en `students`, creada con `INSERT … ON CONFLICT DO NOTHING`) serializa sus confirmaciones sin frenar a los demás estudiantes. Un estudiante confirma pocas veces, así que la espera es despreciable.
- **Costo medido:** con 100 usuarios simultáneos, la confirmación tuvo un p95 de 99,8 ms y la base terminó sin ningún examen con dos planes confirmados ni sesiones de más ([`tests/load/RESULTADOS.md`](../../tests/load/RESULTADOS.md), [ADR-013](ADR-013-capacidad-y-costos.md)).

### 2. Moderar (forum-service, MongoDB)

Aprobar o rechazar es **una única actualización condicional**:

```text
updateOne(
  { _id: <id>, status: "pendiente" },
  { $set: { status: "aprobado", moderatedBy, moderatedAt, publishedAt, updatedAt },
    $push: { outbox: <evento summary.approved> } }
)
```

- Si dos administradores moderan a la vez, MongoDB aplica una sola: la otra ya no encuentra el documento en `pendiente` y responde `409 ALREADY_MODERATED` (RN-23, CA-18).
- El cambio de estado y el evento se guardan en la **misma** escritura, así que no puede haber una moderación sin evento ni un evento sin moderación (RN-32, *outbox* del [ADR-005](ADR-005-comunicacion.md)).
- Eliminar sigue el mismo patrón, con el filtro "no eliminado".

### 3. Registros únicos

| Regla | Mecanismo | Qué pasa si llegan dos a la vez |
|---|---|---|
| **RN-01** mail único (users-service) | Restricción `UNIQUE` sobre `email` en PostgreSQL | El segundo `INSERT` falla con `23505` y responde `409 EMAIL_IN_USE`. |
| **RN-30** una calificación por estudiante (forum-service) | Índice único `(summaryId, userId)` en `ratings` y *upsert* | Si dos *upserts* chocan, el que pierde reintenta como actualización: queda un único registro con el último puntaje. |
| **RN-40** registro idempotente de documentos externos (forum-service) | Colección `idempotency` con `_id` = API key + clave | Se reserva la clave **antes** de subir el PDF. El que no la consigue recibe el documento ya registrado, `422` si el cuerpo es otro o `409 REQUEST_IN_PROGRESS` si el primero sigue en curso. Una reserva colgada más de 1 minuto se puede tomar con una actualización condicional. |

### 4. Trabajos que corren en todas las instancias

Con dos instancias, los procesos periódicos corren dos veces a la vez. En lugar de elegir un líder, se diseñan para que **repetirlos no cambie el resultado**:

- **Indexador de Solr** ([ADR-006](ADR-006-busqueda.md)): siempre escribe el estado actual del resumen en MongoDB, así que dos instancias indexando lo mismo dejan el índice igual.
- **Limpieza de PDF huérfanos** ([ADR-003](ADR-003-persistencia.md)): borrar dos veces el mismo archivo da lo mismo.
- **Relay del outbox** ([ADR-005](ADR-005-comunicacion.md)): dos instancias pueden publicar el mismo evento. Se acepta porque los consumidores descartan duplicados por `eventId` (RN-32). El relay tiene que marcar el evento como publicado con una actualización condicional (`outbox.$.publishedAt: null`), para que el duplicado sea la excepción y no la regla.

## Consecuencias

**Positivas**

- Las reglas críticas las hace cumplir el motor: aunque haya un error en la validación de la aplicación, la base no deja guardar sesiones superpuestas, dos planes confirmados para un examen, dos mails iguales ni dos moderaciones.
- Funciona igual con una instancia o con varias, sin componentes extra.
- Un conflicto nunca es un `500`: cada caso tiene su respuesta (`409` o `422`) con un `code` estable.

**Negativas / limitaciones aceptadas**

- Las confirmaciones de un mismo estudiante se procesan de a una. Para el uso real (un estudiante, pocos dispositivos) no se nota.
- La disponibilidad del estudiante se lee antes de abrir la transacción de confirmación. Si el estudiante la cambia justo en ese momento, el plan se valida contra la anterior. Se acepta porque es el mismo estudiante el que hace las dos cosas, y la restricción de superposición sigue protegiendo el calendario.
- **El promedio de calificaciones puede quedar desactualizado.** Calificar guarda la calificación y después recalcula el promedio leyendo todas, en dos pasos. Si dos estudiantes califican a la vez, el recálculo del primero puede escribirse último con un promedio que no incluye al segundo, y queda así hasta la próxima calificación. La calificación de cada estudiante nunca se pierde (está en `ratings`), sólo el resumen del promedio. Se corrige guardando suma y cantidad con `$inc` (ajustando por el puntaje anterior si el estudiante ya había calificado), que es atómico. Queda pendiente para la presentación final.
- Dos instancias pueden publicar el mismo evento del outbox (ver punto 4). La idempotencia del consumidor lo absorbe.

**Cómo se prueba**

Los tests de integración de esta tabla se corrieron el 2026-10-29 contra PostgreSQL 16 y MongoDB 7 reales, y pasaron todos.

| Caso | Test |
|---|---|
| CA-11: 5 confirmaciones simultáneas del mismo plan con claves distintas reservan un único conjunto de sesiones | `TestIntegration_ConfirmacionesConcurrentes` (planner-service, contra PostgreSQL real) |
| RN-12: la base rechaza una sesión superpuesta aunque la aplicación no valide | `TestIntegration_RestriccionDeSuperposicion` (planner-service) |
| CA-10: el reintento con la misma clave devuelve el mismo resultado sin duplicar | Tests del servicio de planner, colección de Postman y la prueba de carga (cada iteración reintenta) |
| CA-18: 10 moderaciones simultáneas (aprobar y rechazar mezcladas), una sola tiene efecto y un solo evento | `TestDosModeracionesSimultaneasSoloGanaUna` (forum-service, contra MongoDB real) y `TestDosModeracionesSimultaneasSoloUnaTieneEfecto` (en memoria) |
| RN-01: mail duplicado | Test de integración de users-service contra PostgreSQL |
| Bajo carga: 46.387 confirmaciones con 100 usuarios simultáneos, 0 exámenes con más de un plan confirmado | k6, [`tests/load/RESULTADOS.md`](../../tests/load/RESULTADOS.md) |
