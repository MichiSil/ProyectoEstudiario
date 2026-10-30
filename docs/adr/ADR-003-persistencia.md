# ADR-003: Persistencia

- **Estado:** Aceptada (validada para planner-service en la Entrega 2)
- **Fecha:** 2026-10-07
- **Decisión del enunciado:** D3
- **Reemplaza a:** —

## Contexto

Según el [ADR-001](ADR-001-limites-de-servicios.md), cada servicio es dueño de sus datos y ningún servicio accede a la base de otro. El enunciado pide que cada servicio elija el almacenamiento que mejor responda a sus patrones de acceso y que el sistema use **al menos uno relacional y uno no relacional**.

Los patrones de acceso de cada servicio son muy distintos:

| Servicio | Patrón de acceso dominante | Garantía que necesita |
|---|---|---|
| users-service | Pocas escrituras (registro), lecturas por mail en cada login | Mail único |
| planner-service | Escrituras con validaciones cruzadas entre sesiones de un mismo estudiante; consultas por rango de fechas | Transacciones ACID, sin superposiciones ni duplicados |
| forum-service | Muchas lecturas (listado y detalle de resúmenes aprobados), escrituras moderadas, búsqueda por texto | Transiciones de estado atómicas, una calificación por usuario |
| notification-service | Una escritura por mail enviado, consulta por id de evento | Que el mismo evento no genere dos mails |

## Opciones consideradas

1. **Una única base relacional compartida.** Es simple, pero rompe la propiedad de los datos por servicio, acopla los esquemas y no cumple el requisito de dos tipos de almacenamiento.
2. **Todo en MongoDB.** Sirve para el foro, pero las restricciones de la confirmación del plan (no superposición, capacidad diaria, idempotencia) quedarían en código de aplicación, sin garantías del motor y con riesgo ante confirmaciones concurrentes.
3. **Todo en PostgreSQL.** Cumple las garantías, pero el contenido del foro (texto largo, etiquetas y metadatos variables) encaja peor en un esquema rígido, y no cumple el requisito de almacenamiento no relacional.
4. **Almacenamiento por servicio según su patrón (elegida).** Relacional donde hay restricciones e integridad; documental donde predomina la lectura de contenido; motor de búsqueda y caché como almacenes derivados.

## Decisión

| Servicio | Almacenamiento principal | Almacenes auxiliares |
|---|---|---|
| users-service | **PostgreSQL** (base `users`) | — |
| planner-service | **PostgreSQL** (base `planner`) | — |
| forum-service | **MongoDB** (base `forum`) | **MinIO** (archivos PDF), **Apache Solr** (índice de resúmenes aprobados) y **Memcached** (caché de lectura) |
| notification-service | **MongoDB** (base `notifications`) | — |

En el entorno local, PostgreSQL y MongoDB corren una sola vez cada uno en `docker compose`, pero **cada servicio tiene su propia base y su propio usuario**: ningún servicio tiene credenciales para la base de otro. Así se mantiene la propiedad de los datos sin multiplicar contenedores.

### users-service: PostgreSQL

- Tabla `users` con `email` único (restricción `UNIQUE`), hash de la contraseña y rol.
- El esquema es estable y chico, y la unicidad del mail la garantiza el motor, no el código.

### planner-service: PostgreSQL

Es donde vive la acción principal (confirmar el plan), así que se elige el motor que puede **hacer cumplir las reglas de negocio por sí mismo**:

- **No superposición de sesiones:** una restricción de exclusión sobre el rango horario de cada sesión de un mismo estudiante (`EXCLUDE USING gist (student_id WITH =, tstzrange(start_at, end_at) WITH &&)`). Dos confirmaciones concurrentes no pueden reservar el mismo horario aunque las validaciones de la aplicación pasen al mismo tiempo.
- **Confirmación sin duplicados:** tabla de confirmaciones con la clave de idempotencia como `UNIQUE`. Un reintento devuelve el resultado ya guardado.
- **Capacidad diaria:** la suma de minutos por día se valida dentro de la misma transacción que inserta las sesiones, bloqueando las filas del estudiante para esos días.
- Las consultas habituales son por estudiante y rango de fechas (calendario semanal y mensual), que se resuelven con índices compuestos `(student_id, date)`.

El detalle del tratamiento de la concurrencia se documenta en el ADR-004 (D4).

### forum-service: MongoDB + MinIO + Apache Solr + Memcached

- **MongoDB** guarda cada resumen como un documento: título, materia (etiqueta), descripción, referencia al archivo PDF (clave en MinIO, tamaño y tipo), autor (id, nombre y mail copiados al publicar), estado de moderación, motivo de rechazo y el promedio y la cantidad de calificaciones. Un resumen se lee casi siempre entero, y su forma puede variar (con distintas etiquetas o metadatos), lo que encaja con el modelo documental.
- **MinIO** guarda el archivo PDF de cada resumen (máximo 10 MB, según RN-20 del [SPEC](../../SPEC.md)). Es un almacenamiento de objetos compatible con la API de S3, que corre en `docker compose` y permite pasar a un servicio en la nube (por ejemplo, S3) sin cambiar el código. Los binarios no se guardan en MongoDB para no agrandar los documentos que se leen en cada listado. Al publicar, forum-service valida el tipo y el tamaño, sube el archivo y recién después crea el documento; si la creación falla, el archivo queda huérfano y se elimina con una limpieza periódica. La descarga pasa por forum-service, que sólo entrega archivos de resúmenes aprobados (o al autor y al administrador).
- **Transiciones de estado atómicas:** moderar es una actualización condicional sobre un único documento (por ejemplo, "pasar a `aprobada` sólo si está en `pendiente`"). Si dos administradores moderan a la vez, sólo una de las dos actualizaciones tiene efecto.
- **Una calificación por usuario:** colección `ratings` con índice único `(summaryId, userId)`. El promedio del resumen se recalcula al calificar.
- **Apache Solr** indexa sólo los resúmenes aprobados para la búsqueda por texto, con filtros por materia, paginación y orden por fecha o por calificación. Es un **almacén derivado**: si se pierde, se reconstruye desde MongoDB. La sincronización y el retraso tolerable se definen en el ADR-006 (D6).
- **Memcached** guarda temporalmente las lecturas más frecuentes (listado de resúmenes aprobados y detalle de un resumen), reutilizando la implementación de caché trabajada en clase (interfaz `Cache` con versión local, Memcached y nula). Tampoco es fuente de verdad. Su vigencia e invalidación se definen en el ADR-007 (D7).

### notification-service: MongoDB

- Colección `sent_notifications` con índice único sobre el `eventId`. Antes de enviar un mail se registra el evento; si ya existe, el mensaje se descarta. Esto hace que el consumidor sea idempotente frente a reentregas de RabbitMQ.
- Se elige MongoDB porque es un registro simple de documentos sin relaciones, y la instancia ya existe en el sistema.

### Lo que no se persiste en este hito

- **Imágenes u otros formatos de archivo** en los resúmenes: en esta versión sólo se aceptan PDF.
- **RabbitMQ** no se considera almacenamiento de datos de negocio: los mensajes son transitorios y la fuente de verdad siempre es la base del servicio que publica.

## Consecuencias

**Positivas**

- Las reglas críticas de la confirmación del plan quedan garantizadas por PostgreSQL y no dependen sólo del código.
- El foro escala en lecturas con MongoDB, Solr y Memcached, sin afectar al planificador.
- Se cumple el requisito de un almacenamiento relacional (PostgreSQL) y uno no relacional (MongoDB), y cada elección tiene un motivo propio del servicio.

**Negativas / limitaciones aceptadas**

- Hay dos motores de base de datos, un almacenamiento de objetos, un índice y una caché para operar, respaldar y observar.
- La creación de un resumen escribe en dos almacenamientos (MinIO y MongoDB) sin una transacción común. Se acepta que pueda quedar un archivo huérfano, que se limpia periódicamente; nunca queda un resumen sin archivo.
- Solr y Memcached pueden quedar momentáneamente desactualizados respecto de MongoDB (consistencia eventual en la búsqueda y en la caché).
- En MongoDB, la actualización de un resumen y la de su promedio de calificaciones no están en la misma transacción. Se acepta que el promedio quede brevemente desfasado; el índice único sobre `ratings` sigue garantizando que nadie califique dos veces.
- Las restricciones de exclusión con `gist` atan el diseño a PostgreSQL. Cambiar de motor relacional implicaría reimplementarlas.

**A validar en la Entrega 2**

- Con datos reales, que los índices definidos cubran las consultas del calendario y del foro.
- Confirmar que el *outbox* dentro del mismo documento, definido en el ADR-005 (D5), alcanza para publicar eventos sin perderlos y sin necesitar transacciones de varios documentos (que en MongoDB exigen un *replica set*).

## Validación (Entrega 2, 2026-10-19)

**Resultado: la decisión se mantiene para planner-service, con un ajuste en el bloqueo.** El resto de los almacenamientos se valida cuando sus servicios estén operativos.

### planner-service: PostgreSQL

Implementado en `services/planner-service/internal/adapters/postgres/schema.sql`.

| Garantía | Cómo quedó | Evidencia |
|---|---|---|
| Sin sesiones superpuestas (RN-12) | Restricción `sessions_no_overlap EXCLUDE USING gist (student_id WITH =, tstzrange(starts_at, ends_at) WITH &&)`, que ignora las sesiones pospuestas. | `TestIntegration_RestriccionDeSuperposicion`: la base rechaza una sesión superpuesta aunque se inserte saltando la aplicación. |
| Confirmación sin duplicados (RN-14) | Tabla `confirmations` con clave primaria `(student_id, idempotency_key)` y el resultado guardado en la misma transacción. | `TestIntegration_ConfirmarYReintentar` y prueba de carga: 46.387 reintentos sin duplicar sesiones. |
| Un plan confirmado por examen (RN-15) | Índice único parcial `plans_one_confirmed_per_exam ON plans (exam_id) WHERE status = 'confirmado'`. | Prueba de carga: 0 exámenes con dos planes confirmados. |
| Capacidad diaria (RN-13) | Se valida dentro de la transacción, contra las sesiones del estudiante. | Tests unitarios de `domain.ValidatePlan` (CA-09). |

**Ajuste respecto de la decisión original.** Este ADR proponía bloquear *las filas del estudiante para esos días*. Se implementó algo más simple: una tabla `students` con una fila por estudiante, que la confirmación bloquea con `SELECT ... FOR UPDATE`. Así todas las confirmaciones del mismo estudiante se procesan de a una, sin riesgo de *deadlocks* por bloquear varias filas en distinto orden. El costo es que dos confirmaciones del mismo estudiante para días distintos también esperan una a la otra; se acepta porque es un caso raro (lo hace una misma persona) y entre estudiantes distintos no hay espera. `TestIntegration_ConfirmacionesConcurrentes` (5 confirmaciones simultáneas del mismo plan) termina con una sola confirmación exitosa.

**Índices.** Las consultas del calendario se resuelven con `sessions (student_id, starts_at)` y `exams (student_id, starts_at)`, en lugar del `(student_id, date)` previsto, porque las sesiones guardan fecha y hora.

**Rendimiento.** Con 1 vCPU, la base sostiene unos 250 flujos completos por segundo, y su CPU es el principal límite del sistema ([ADR-013](ADR-013-capacidad-y-costos.md)).

### Pendiente de validar

- **users-service** (PostgreSQL) y **forum-service** (MongoDB, MinIO, Solr y Memcached) siguen en esqueleto, y **notification-service** (MongoDB) todavía no consume eventos. Sus almacenamientos se validan para la presentación final.
- En el `docker-compose.yml` de esta entrega, el usuario de la base `planner` es también el usuario administrador del contenedor, porque es la única base. Cuando se sume la base `users`, cada servicio tiene que tener su propio usuario sin permisos sobre la otra. *(Resuelto el 2026-10-29: ver la actualización de abajo.)*

## Actualización (2026-10-29): cada servicio con su propio usuario de PostgreSQL

**Resultado: se cumple lo que pedía este ADR, que ningún servicio tenga credenciales para la base de otro.** Hasta ahora no era así: planner-service se conectaba con el administrador del contenedor, un superusuario, que podía leer cualquier base, incluida la de `users`.

| Usuario | Para qué | Puede entrar a |
|---|---|---|
| `POSTGRES_ADMIN_USER` (`planner_svc`) | Administrador del contenedor. Sólo lo usa el contenedor `db-init` para crear las bases y sus usuarios. **Ningún servicio se conecta con él.** | Todo (superusuario) |
| `PLANNER_DB_USER` (`planner_app`) | planner-service. Dueño de la base `planner`, de su esquema y de sus tablas. No es superusuario. | Sólo `planner` |
| `USERS_DB_USER` (`users_svc`) | users-service. Dueño de la base `users`. No es superusuario. | Sólo `users` |

**Cómo se hace sin borrar las bases que ya existían.** El contenedor `db-init` corre un script idempotente (`deploy/postgres/init-databases.sh`) en cada `docker compose up`: crea los usuarios si no existen, le pasa a cada uno la propiedad de su base, quita el permiso de conexión por defecto (`REVOKE CONNECT ... FROM PUBLIC`) y, en una base `planner` de antes, le pasa las tablas que había creado el administrador a `planner_app`. El administrador conserva el nombre `planner_svc` porque así lo crearon los volúmenes existentes, que no permiten cambiarlo sin recrear la base.

**Evidencia.**

| Prueba | Resultado |
|---|---|
| Volumen de antes, con tablas y datos creados por el administrador | Las tablas pasan a `planner_app`, los datos se conservan y el flujo completo (registro, login, propuesta y confirmación por el gateway) funciona |
| Volumen nuevo | `planner_app` crea las tablas, la extensión `btree_gist` (que PostgreSQL permite instalar a un no superusuario) y la restricción `sessions_no_overlap` |
| `planner_app` intenta entrar a `users` | `permission denied for database "users"` |
| `users_svc` intenta entrar a `planner` | `permission denied for database "planner"` |

**Limitación aceptada.** Las dos bases siguen en la misma instancia de PostgreSQL: una falla de esa instancia afecta a los dos servicios. Separarlas en instancias distintas es la alternativa si la carga o la disponibilidad lo pidieran ([ADR-013](ADR-013-capacidad-y-costos.md)).
