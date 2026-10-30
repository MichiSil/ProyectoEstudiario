# Estudiario

Plataforma de estudio para estudiantes universitarios, construida como un sistema basado en microservicios para el Práctico Integrador de **Arquitectura de Software 2026** (Facultad de Ingeniería – UCC).

Combina dos partes:

- **Organización personal del estudio.** Cada estudiante carga sus materias, fechas de examen y horas disponibles, y un **agente de IA** le arma el plan de estudio.
- **Foro de resúmenes.** Los estudiantes comparten resúmenes, un administrador los modera antes de publicarlos y el resto de los usuarios los califica.

## Integrantes

| Legajo | Integrante |
|---|---|
| 2405404 | Acuña, Isabela |
| 2417714 | Ainete, Tobías |
| 2418092 | Chamaza, Florencia |
| 2411741 | Silvestrini, Mia |

## Dominio

### Roles

| Rol | Qué puede hacer |
|---|---|
| **Estudiante** | Gestionar sus materias, calendario, disponibilidad y plan de estudio; subir resúmenes al foro; calificar resúmenes publicados |
| **Administrador** | Moderar el foro: aprobar, rechazar o eliminar publicaciones |

### Organización del estudio

Cada estudiante tiene **sus propias materias**, a las que les asigna una **dificultad** (fácil, media, difícil o muy difícil). En su **calendario** carga las fechas importantes de cada materia: parciales, finales, recuperatorios y entregas, con los temas que entran. También declara sus **horas de estudio disponibles**: en qué franjas de la semana puede estudiar y cuántos minutos por día como máximo.

A partir de todo eso, un **agente de IA arma el plan de estudio**. Propone sesiones con fecha, horario, duración, tema y objetivo, y el estudiante las confirma para que queden reservadas en su calendario.

### Foro de resúmenes

Los estudiantes suben **resúmenes** de una materia al foro. Antes de hacerse visible, cada publicación pasa por la **moderación del administrador**:

```mermaid
stateDiagram-v2
    [*] --> Pendiente: el estudiante sube el resumen
    Pendiente --> Aprobada: el admin la aprueba
    Pendiente --> Rechazada: el admin la rechaza
    Aprobada --> Eliminada: el admin la elimina
    Rechazada --> Eliminada: el admin la elimina
    Pendiente --> Eliminada: el admin la elimina
```

- Cuando el administrador **aprueba o rechaza** una publicación, el sistema le **envía un mail al autor** avisándole el resultado (y el motivo, si fue rechazada).
- Sólo las publicaciones **aprobadas** son visibles en el foro.
- Los demás estudiantes pueden **calificar** cada resumen publicado. La calificación promedio ayuda a encontrar los mejores resúmenes.

### Acciones principales

| Dominio | Entidad principal | Acción principal | Característica relevante |
|---|---|---|---|
| Planificación de estudio | Examen / sesión de estudio | Confirmar el plan de estudio que arma el agente de IA (reservar sus sesiones) | Las sesiones no pueden superponerse ni superar las horas disponibles del estudiante; una misma confirmación no puede duplicarse; si el examen cambia de fecha, el plan queda desactualizado y hay que replanificarlo |
| Foro de resúmenes | Publicación (resumen) | Moderar una publicación (aprobar / rechazar / eliminar) | La publicación tiene estados con transiciones válidas; cada decisión debe notificarse por mail al autor exactamente una vez; un estudiante califica cada resumen una sola vez |

Ninguna de las dos es una simple alta de registros. La confirmación del plan tiene que validar varias restricciones a la vez, y puede llegar en paralelo desde dos dispositivos o repetirse por un reintento de red. La moderación cambia el estado de la publicación y dispara una notificación por mail que no se puede perder ni duplicar.

## Objetivo

- Que el estudiante no tenga que decidir a mano cuándo estudiar cada materia. El agente de IA propone un plan a partir de las fechas de examen, la dificultad de cada materia, los temas que entran y las horas disponibles. Al confirmarlo, el sistema garantiza que la agenda quede consistente.
- Que los estudiantes puedan compartir y encontrar resúmenes de calidad, con una moderación que evite publicaciones inadecuadas y calificaciones que destaquen los mejores.

## Funcionalidades

- Registro e inicio de sesión, con rol de estudiante o administrador.
- Materias propias de cada estudiante, con su dificultad.
- Calendario con las fechas de exámenes y entregas.
- Disponibilidad: franjas horarias semanales y máximo de minutos de estudio por día.
- Plan de estudio armado por un agente de IA, con confirmación y seguimiento de sesiones.
- Foro de resúmenes: publicación, moderación, búsqueda y calificación.
- Notificación por mail al autor cuando su resumen es aprobado o rechazado.

## Flujo principal

### 1. Plan de estudio

1. **Ingreso.** El estudiante se registra o inicia sesión.
2. **Materias.** Da de alta sus materias con su dificultad.
3. **Calendario.** Carga las fechas de examen de cada materia: tipo, fecha y temas.
4. **Disponibilidad.** Indica sus horas de estudio disponibles: franjas semanales y máximo de minutos por día.
5. **Propuesta del agente de IA.** Pide un plan para un examen y el agente propone sesiones con fecha, horario, duración, tema y objetivo. Por ejemplo: "Leer teoría de Integrales", "Resolver ejercicios", "Simulacro de parcial".
6. **Confirmación.** El estudiante confirma el plan. Las sesiones quedan reservadas en su calendario sólo si:
   - todas caen dentro de sus horas disponibles y antes del examen;
   - ninguna se superpone con otra sesión ya reservada;
   - ningún día supera el máximo de minutos declarado;
   - la confirmación no fue procesada antes (si se repite, se devuelve el mismo resultado en lugar de duplicar sesiones).
7. **Cambios.** Si el examen cambia de fecha o se cancela, el plan pasa a *desactualizado* y las sesiones pendientes se replanifican o se liberan.
8. **Seguimiento.** A medida que estudia, marca cada sesión como completada o pospuesta.

### 2. Foro de resúmenes

1. **Publicación.** El estudiante sube un resumen de una materia y queda *pendiente* de moderación.
2. **Moderación.** El administrador revisa las publicaciones pendientes y aprueba o rechaza cada una. También puede eliminar cualquier publicación.
3. **Notificación.** Al aprobarse o rechazarse, el sistema le envía un mail al autor con el resultado.
4. **Consulta.** Los estudiantes buscan resúmenes aprobados por materia o por texto.
5. **Calificación.** Cada estudiante puede calificar un resumen publicado una sola vez.

## Desafíos adicionales

Además de lo obligatorio, el grupo encara dos desafíos adicionales. El alcance y los criterios de aceptación están en la sección 10 del [`SPEC.md`](SPEC.md#10-desafíos-adicionales).

| Desafío | Qué agrega |
|---|---|
| **Agente que opera el sistema mediante MCP** | El agente de IA consulta las materias y los exámenes del estudiante y propone planes de estudio a través de herramientas expuestas con el *Model Context Protocol* (MCP), un protocolo estándar para que un agente ejecute operaciones de un sistema. El agente propone, pero nunca confirma: la confirmación sigue siendo del estudiante. |
| **Tests end-to-end del flujo principal** | Pruebas automáticas que recorren el flujo del plan de estudio de punta a punta contra el sistema levantado con `docker compose`: desde el registro hasta la confirmación del plan y su reintento. |

## Cómo levantar el sistema

Todo el sistema se levanta con **un único comando** usando Docker, sin configuraciones manuales.

Funcionan **users-service** (registro, inicio de sesión y emisión del JWT) y **planner-service** (pedir y confirmar el plan de estudio), cada uno con su base en PostgreSQL, y **forum-service** (foro de resúmenes y capacidad de documentos para el Grupo 4) con MongoDB y MinIO. El **frontend web** usa users-service y planner-service a través del api-gateway. notification-service todavía es un esqueleto que sólo responde `GET /health`.

### Requisitos

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (incluye Docker Compose v2).
- Git.

### Pasos

```bash
git clone https://github.com/MichiSil/ProyectoEstudiario.git
cd ProyectoEstudiario
cp .env.example .env        # variables locales; los secretos reales nunca se suben al repo
docker compose up --build
```

La primera vez tarda unos minutos porque descarga las imágenes y compila los servicios. Las bases `planner` y `users` se crean solas, cada una con su usuario y su contraseña del `.env`.

Para detenerlo:

```bash
docker compose down         # agregar -v para borrar también los datos locales
```

### Puertos y URLs

| Componente | URL local | Para qué |
|---|---|---|
| **frontend** | http://localhost:5173 | Interfaz web de Estudiario. Reenvía `/api` al api-gateway |
| **api-gateway** | http://localhost:8080 | Punto de entrada único del frontend |
| planner-service | http://localhost:8082 | Plan de estudio |
| users-service | http://localhost:8081 | Registro, inicio de sesión y emisión del JWT (se usa a través del gateway, en `/api/users`) |
| forum-service | http://localhost:8083 | Foro de resúmenes (a través del gateway, en `/api/forum`) y capacidad de documentos para el Grupo 4 (directo, en `/v1`) |
| notification-service | http://localhost:8084 | Esqueleto: sólo `GET /health` |
| **Jaeger** | http://localhost:16686 | Interfaz web para ver las trazas. Recibe trazas OTLP en los puertos 4317 (gRPC) y 4318 (HTTP) |
| **Mock de la capacidad (Prism)** | http://localhost:4010 | Registro y consulta de documentos: contrato de [`docs/contracts/`](docs/contracts/) con respuestas de ejemplo |
| MongoDB (base `forum`) | Sin puerto publicado | Sólo lo usan forum-service y `mongo-init`, por la red interna de Docker. forum-service entra con su usuario (`FORUM_DB_*`), que sólo lee y escribe la base `forum`; el administrador (`MONGO_ROOT_*`) sólo lo usa `mongo-init` |
| Solr (core `summaries`) | http://localhost:8983/solr | Índice de búsqueda de los resúmenes aprobados. El panel de administración sirve para ver el esquema y probar consultas |
| MinIO (bucket `summaries`) | Consola web en http://localhost:9001 | PDF de los resúmenes. La API S3 no se publica: forum-service la usa por la red interna de Docker con su propio usuario (`FORUM_MINIO_*`), que sólo puede usar el bucket `summaries`. La consola se abre con `MINIO_ROOT_*` |
| PostgreSQL (bases `planner` y `users`) | `localhost:5432` | Usuario y contraseña de cada base: variables `PLANNER_DB_*` y `USERS_DB_*` del `.env`. Cada usuario entra sólo a su base; el administrador (`POSTGRES_ADMIN_*`) sólo lo usa el contenedor `db-init` |

Cada servicio responde `GET /health`. Por ejemplo:

```bash
curl http://localhost:8080/health
```

El puerto de PostgreSQL se expone sólo para inspeccionar la base durante el desarrollo: los servicios se conectan entre sí por la red interna de Docker. Si algún puerto ya está ocupado en tu máquina, cambiá el número de la izquierda en el `docker-compose.yml` (por ejemplo, `"5433:5432"`).

Para levantar sólo el mock de la capacidad publicada:

```bash
docker compose up contract-mock
```

> Las credenciales del proveedor de IA se configuran sólo en el `.env` local y nunca se suben al repositorio. Sin credenciales de IA, el plan se arma con el motor de reglas (RN-09).

### Cuentas e identidad

Todo pedido a `/api/planner/*` pasa por el api-gateway y necesita un **JWT** en `Authorization: Bearer <token>`. El token lo emite **users-service** al iniciar sesión. El gateway lo valida, borra cualquier `X-User-Id` que mande el cliente y completa la identidad a partir del token ([ADR-001](docs/adr/ADR-001-limites-de-servicios.md)). planner-service y users-service sólo aceptan `X-User-Id` si el pedido trae además el secreto interno que les manda el gateway, así que llamarlos directo a su puerto con el id de otro usuario devuelve `401`.

**1. Registrarse** (público, no necesita token):

```bash
curl -X POST http://localhost:8080/api/users/register \
  -H "Content-Type: application/json" \
  -d '{"name":"Ana Pérez","email":"ana@mail.com","password":"secreta123"}'
```

Devuelve `201` con el usuario. La contraseña tiene como mínimo 8 caracteres y se guarda sólo como hash bcrypt (RN-02). Un mail ya registrado devuelve `409 EMAIL_IN_USE` (RN-01), y datos inválidos, `422 VALIDATION_ERROR`. El rol es `estudiante`; el rol `admin` no se elige (RF-03): lo tienen los mails de la variable `ADMIN_EMAILS` del `.env`. El rol se recalcula en cada inicio de sesión, así que agregar o sacar un mail de esa lista vale desde el próximo login, sin tocar la base (después de cambiar el `.env`, recreá el servicio con `docker compose up -d users-service`).

**2. Iniciar sesión** y guardar el token (vale 8 horas):

```bash
export TOKEN=$(curl -s -X POST http://localhost:8080/api/users/login \
  -H "Content-Type: application/json" \
  -d '{"email":"ana@mail.com","password":"secreta123"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
```

El login devuelve `{"token": ..., "expiresAt": ..., "user": {...}}`. Si el mail no existe o la contraseña está mal, responde `401 INVALID_CREDENTIALS` con el mismo mensaje en los dos casos.

**3. Usar el token:**

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/users/me
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/planner/sessions
```

**Tokens de prueba.** Para tests o scripts que no quieren pasar por el registro, el comando `devtoken` firma un token con el mismo formato y el `JWT_SECRET` del `.env`:

```bash
cd services/api-gateway
export TOKEN=$(JWT_SECRET=$(grep ^JWT_SECRET= ../../.env | cut -d= -f2 | tr -d '\r') go run ./cmd/devtoken -sub estudiante-1)
cd ../..
```

> Si ya tenías un `.env` de antes, volvé a copiar `.env.example` a `.env` (y, si tenías una `AI_API_KEY`, volvé a ponerla). Cambiaron las variables de PostgreSQL (`POSTGRES_ADMIN_*` y `PLANNER_DB_*`) y se sumaron `JWT_SECRET`, `INTERNAL_TOKEN`, `USERS_DB_*` y `ADMIN_EMAILS`: sin ellas, `docker compose` no levanta. No hace falta borrar la base: `db-init` adapta la que ya tenías.

### Editar y cancelar exámenes

El estudiante puede listar sus exámenes, editarlos y cancelarlos (RF-06), con el JWT de la sección anterior:

```bash
G=http://localhost:8080/api/planner
curl -H "Authorization: Bearer $TOKEN" $G/exams
curl -H "Authorization: Bearer $TOKEN" -X PATCH $G/exams/<id del examen> \
  -H "Content-Type: application/json" -d '{"startsAt":"2026-11-27T09:00:00-03:00"}'
curl -H "Authorization: Bearer $TOKEN" -X POST $G/exams/<id del examen>/cancel
```

- `GET /exams` devuelve cada examen con `planStatus`: `confirmado`, `desactualizado` o ausente si todavía no tiene plan.
- `PATCH /exams/{id}` cambia sólo los campos que se mandan (`type`, `startsAt`, `topics`); la materia no se cambia. La fecha nueva tampoco puede estar en el pasado (RN-04). Si cambia la fecha de un examen con plan confirmado, el plan pasa a **desactualizado** y sus sesiones pendientes siguen en `GET /sessions` con `"outdated": true` hasta que el estudiante replanifique (RN-16). Al confirmar el plan nuevo, esas sesiones se reemplazan.
- `POST /exams/{id}/cancel` cancela el examen y sus planes, libera las sesiones pendientes y conserva las completadas como historial (RN-17). Cancelar dos veces no falla. Un examen cancelado no se puede editar ni planificar: responde `409 EXAM_CANCELLED`.

**Borrar una materia:** `DELETE /subjects/{id}` responde `204` (o `404` si no existe o ya estaba borrada). Cancela sus exámenes y planes y libera las sesiones pendientes, igual que cancelar cada examen. La materia deja de verse junto con sus exámenes y sesiones; las sesiones completadas se conservan en la base.

Editar, cancelar, borrar una materia y confirmar un plan bloquean la fila del estudiante en la misma transacción, así que no se pisan entre sí ([ADR-004](docs/adr/ADR-004-concurrencia.md)).

### Material de cada materia

Cada materia tiene su **material**: los teóricos (apuntes, unidades, capítulos) y los prácticos (trabajos prácticos, guías, parciales viejos) que el estudiante tiene para estudiar, con un título y, opcional, lo que tiene adentro. Cada examen indica su **modalidad** (teórico, práctico o mixto) y qué material entra.

```bash
G=http://localhost:8080/api/planner
curl -H "Authorization: Bearer $TOKEN" -X POST $G/subjects/<id de la materia>/materials \
  -H "Content-Type: application/json" -d '{"kind":"practico","title":"TP 5 — Ondas estacionarias","content":"Ejercicios 1 a 10"}'
curl -H "Authorization: Bearer $TOKEN" $G/subjects/<id de la materia>/materials
curl -H "Authorization: Bearer $TOKEN" -X PATCH $G/exams/<id del examen> \
  -H "Content-Type: application/json" -d '{"modality":"practico","materialIds":["<id del material>"]}'
```

**El plan se arma con ese material real.** El agente de IA recibe la lista con el contenido de cada material y la instrucción de no mencionar nada que no esté cargado; el motor de reglas arma las sesiones con los títulos ("Estudiar Unidad 4", "Resolver TP 5") según la modalidad. Si no hay material, se usan los temas del examen y, si tampoco se sabe la modalidad, el plan no supone si hay que leer teoría o hacer ejercicios. Sin material elegido en el examen, entra todo el de la materia.

### Agente por MCP

planner-service expone un servidor **MCP** (*Model Context Protocol*) en `/api/planner/mcp`, detrás del api-gateway y con el mismo JWT, para que un agente de IA opere Estudiario en nombre del estudiante (SPEC 10.1). Las herramientas usan los mismos casos de uso que la API, con las mismas reglas:

| Herramienta | Qué hace |
|---|---|
| `listar_materias` | Las materias del estudiante, con su dificultad |
| `listar_examenes` | Los próximos exámenes programados, con fecha, temas, modalidad y estado del plan |
| `proponer_plan` | Pide una propuesta para un examen. No reserva nada |

No hay herramienta para confirmar: el agente propone y el estudiante confirma desde la app (RN-37). Cada pedido arma su servidor con la identidad que validó el gateway, así que el agente nunca ve datos de otro estudiante (RN-36). Para probarlo con el inspector oficial:

```bash
npx @modelcontextprotocol/inspector
# Transporte: Streamable HTTP · URL: http://localhost:8080/api/planner/mcp
# Encabezado: Authorization: Bearer $TOKEN
```

### Tests end-to-end

Recorren el flujo principal completo contra el sistema levantado, a través del gateway (SPEC 10.2): registro, materia, examen, disponibilidad, propuesta, confirmación, reintento idempotente y propuesta con la IA caída (`AI_FALLBACK`). Cómo correrlos: [tests/e2e/README.md](tests/e2e/README.md).

### Foro de resúmenes

forum-service está organizado por capas (controller → service → repository, [ADR-002](docs/adr/ADR-002-arquitectura-interna.md)). Los resúmenes se guardan en MongoDB y los PDF en MinIO ([ADR-003](docs/adr/ADR-003-persistencia.md)). Desde el frontend se usa a través del api-gateway en `/api/forum/*`, con el JWT de la sección anterior.

Para probarlo hace falta un estudiante y un administrador. El mail `admin@estudiario.local` está en `ADMIN_EMAILS` del `.env.example`, así que al registrarse recibe el rol `admin`:

```bash
G=http://localhost:8080
login() { curl -s -X POST $G/api/users/register -H "Content-Type: application/json" -d "{\"name\":\"$1\",\"email\":\"$2\",\"password\":\"secreta123\"}" >/dev/null
          curl -s -X POST $G/api/users/login -H "Content-Type: application/json" -d "{\"email\":\"$2\",\"password\":\"secreta123\"}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])'; }
EST=$(login "Ana Pérez" ana@mail.com)
ADMIN=$(login "Admin" admin@estudiario.local)
```

**Subir un resumen** (multipart: `title`, `category` con la materia, `description`, `tags` separadas por comas y el PDF en `file`). El **autor sale del usuario logueado**: el nombre y el mail del token se copian en el resumen, y la respuesta muestra el nombre pero nunca el mail. Si el token no trae el nombre (por ejemplo, uno de `devtoken`), se usa el campo opcional `authorName` y, si tampoco viene, "Estudiante de Estudiario". Queda **pendiente** (RN-19). Si el archivo no es PDF responde `415`, y si pesa más de 10 MB, `413` (RN-20):

```bash
curl -s -H "Authorization: Bearer $EST" $G/api/forum/summaries \
  -F "title=Resumen de integrales dobles" -F "category=Análisis Matemático II" \
  -F "tags=parcial,integrales" \
  -F "file=@docs/contracts/examples/guia-ejemplo.pdf;type=application/pdf"
# guardá el "id" de la respuesta:
ID=<id del resumen>
```

**Moderar** (sólo administradores; un estudiante recibe `403`):

```bash
curl -s -H "Authorization: Bearer $ADMIN" $G/api/forum/admin/summaries/pending                 # de la más vieja a la más nueva
curl -s -X POST -H "Authorization: Bearer $ADMIN" $G/api/forum/admin/summaries/$ID/approve
curl -s -X POST -H "Authorization: Bearer $ADMIN" -H "Content-Type: application/json" \
  -d '{"reason":"El PDF está incompleto"}' $G/api/forum/admin/summaries/$ID/reject           # el motivo es obligatorio
curl -s -X DELETE -H "Authorization: Bearer $ADMIN" $G/api/forum/admin/summaries/$ID
```

Aprobar o rechazar algo que ya no está pendiente responde `409 ALREADY_MODERATED` (RN-23). Cada moderación guarda su evento (`summary.approved`, `summary.rejected` o `summary.deleted`, con el formato del [ADR-005](docs/adr/ADR-005-comunicacion.md)) en el arreglo `outbox` del mismo documento, en la misma actualización.

**Leer y calificar:**

```bash
curl -s -H "Authorization: Bearer $EST" $G/api/forum/summaries/mine                            # mis publicaciones con su estado
curl -s -H "Authorization: Bearer $EST" "$G/api/forum/summaries?q=integrales&category=analisis%20matematico%20ii&sort=rating&page=1&size=20"
curl -s -H "Authorization: Bearer $EST" $G/api/forum/summaries/$ID                              # detalle
curl -s -o resumen.pdf -H "Authorization: Bearer $EST" $G/api/forum/summaries/$ID/file         # descarga
curl -s -X PUT -H "Authorization: Bearer $EST" -H "Content-Type: application/json" \
  -d '{"score":4}' $G/api/forum/summaries/$ID/rating                                            # de 1 a 5
```

Sólo se califican resúmenes aprobados (RN-28), nunca el propio (RN-29), y calificar de nuevo reemplaza el puntaje anterior (RN-30). La búsqueda (la del foro y la de `/v1`) usa **Solr** ([ADR-006](docs/adr/ADR-006-busqueda.md)):

- **Qué busca:** texto en título, materia y descripción, sin importar tildes ni plurales ("analisis integral" encuentra "Análisis… Integrales"). Filtra por materia, etiquetas y calificación mínima, y ordena por relevancia, calificación o fecha.
- **Cómo se mantiene al día:** el indexador corre dentro de forum-service y cada 5 s pasa a Solr los resúmenes que cambiaron en MongoDB. Un aprobado aparece y un eliminado desaparece en menos de 10 s (el SPEC pide 1 minuto). Cada 5 minutos, además, compara todo MongoDB con el índice y corrige diferencias, incluidas las calificaciones.
- **Si Solr se cae:** la búsqueda responde `503 SEARCH_UNAVAILABLE` y el resto del foro sigue funcionando. Al volver, el índice se pone al día solo.
- **Si se pierde el índice** (por ejemplo, con `docker compose down -v`) o cambia el esquema, se reconstruye desde MongoDB:

```bash
docker compose exec forum-service forum-service reindex
```

El esquema del índice está versionado en [`schema.json`](services/forum-service/internal/search/schema.json). forum-service lo aplica al arrancar con la Schema API de Solr, sin pasos manuales.

**Limpieza de PDF huérfanos.** forum-service borra solo, cada hora, los PDF del bucket que no tienen ningún resumen (por ejemplo, si falló la creación del documento después de subir el archivo), siempre que tengan más de 1 hora. Se configura con `FORUM_CLEANUP_INTERVAL` y `FORUM_CLEANUP_MIN_AGE` en el `.env` ([ADR-003](docs/adr/ADR-003-persistencia.md)); para probarlo se pueden bajar, por ejemplo a `30s` y `0s`. Cada vuelta deja en el log `limpieza de PDF huérfanos` con lo revisado y lo borrado.

### Capacidad de documentos para el Grupo 4

forum-service expone directamente, sin pasar por el gateway, la API de [`docs/contracts/documents-v1.yaml`](docs/contracts/documents-v1.yaml) en `http://localhost:8083/v1`. La guía para el consumidor está en [`docs/contracts/README.md`](docs/contracts/README.md).

Las API keys se configuran en `FORUM_API_KEYS` **sólo como hash SHA-256** (`consumidor:hash`). El `.env.example` trae la clave de prueba local `clave-de-prueba-grupo4`. La clave real del Grupo 4 se genera aparte y se entrega por un canal privado. Su hash se calcula con:

```bash
printf '%s' 'la-clave' | shasum -a 256
```

Ejemplo de punta a punta, como lo haría el Grupo 4:

```bash
F=http://localhost:8083
K="X-API-Key: clave-de-prueba-grupo4"
curl -i -X POST $F/v1/documents -H "$K" -H "Idempotency-Key: $(uuidgen)" \
  -F "title=Guía de trekking en los glaciares" -F "category=Patagonia" -F "tags=trekking,invierno" \
  -F "authorName=Lucía Fernández" -F "authorExternalId=viajero-1842" \
  -F "file=@docs/contracts/examples/guia-ejemplo.pdf;type=application/pdf"     # 202, queda pendiente
curl -s "$F/v1/submissions?status=pendiente" -H "$K"                           # lo ve sólo esta API key
# un administrador lo aprueba desde el foro (/api/forum/admin/summaries/<id>/approve) y entonces:
curl -s "$F/v1/documents?category=Patagonia&tag=trekking&sort=rating" -H "$K" # aparece en la búsqueda
```

Repetir el registro con la misma `Idempotency-Key` y el mismo cuerpo devuelve el mismo documento con `Idempotent-Replayed: true`. Con otro cuerpo responde `422 IDEMPOTENCY_KEY_REUSED`. El cupo es de 60 pedidos por minuto por API key, de los cuales hasta 10 registros, y se informa en `RateLimit-Limit`, `RateLimit-Remaining` y `Retry-After`. Los documentos de sistemas externos se moderan igual que los resúmenes, pero no generan mail ni beneficios (RN-41): su evento lo indica con `notifyAuthor` y `awardBenefits` en `false`.

### Tests de forum-service

```bash
cd services/forum-service
go test ./...                                                   # unitarios y de contrato (en memoria)
FORUM_CONTRACT_URL=http://localhost:8083 FORUM_CONTRACT_API_KEY=clave-de-prueba-grupo4 \
  go test -run Contrato ./internal/controller/                  # contrato contra el servicio real
```

Los tests de integración usan MongoDB y MinIO reales. Como sus puertos no se publican fuera de Docker, se corren en un contenedor de Go conectado a la red de `docker compose` (con el sistema levantado, desde la raíz del repositorio):

```bash
docker run --rm --network estudiario_default -v "$PWD/services/forum-service:/src" -w /src \
  -e FORUM_TEST_MONGO_URI='mongodb://forum_svc:forum-local@mongo:27017/forum?authSource=forum' \
  -e FORUM_TEST_MINIO_ENDPOINT=minio:9000 \
  golang:1.26-alpine go test ./internal/repository/ ./internal/storage/
```

Los tests borran al terminar los datos que crean.

El test de integración de Solr vacía el core que usa, así que se corre contra un Solr descartable, no contra el del `docker compose`:

```bash
docker run -d --rm --name solr-test -p 18983:8983 solr:9.7.0 solr-precreate summaries_test
FORUM_TEST_SOLR_URL=http://localhost:18983/solr/summaries_test go test ./internal/search/
docker stop solr-test
```

El test de contrato valida cada respuesta de `/v1/*` y de `/health`, de éxito y de error, contra `docs/contracts/documents-v1.yaml` con [kin-openapi](https://github.com/getkin/kin-openapi). Si el servicio y el contrato no coinciden, el test falla.

### Logs y trazas

El api-gateway, users-service y planner-service escriben **un log JSON por pedido** con `time`, `level`, `service`, `correlationId`, `method`, `path`, `status` y `durationMs`. Cada pedido lleva un `X-Correlation-Id`: si el cliente no lo manda, lo genera el gateway, lo reenvía al servicio y lo devuelve en la respuesta. La estrategia completa está en el [ADR-011](docs/adr/ADR-011-observabilidad.md).

**Ver los logs:**

```bash
docker compose logs -f api-gateway planner-service
```

Para seguir un pedido puntual, mandalo con tu propio identificador y filtrá por él:

```bash
curl -H "X-Correlation-Id: prueba-1" -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/planner/sessions
docker compose logs api-gateway planner-service | grep prueba-1
```

**Ver las trazas:** abrí Jaeger en http://localhost:16686, elegí el servicio `api-gateway` y tocá *Find Traces*. Cada traza muestra el pedido completo: el span del gateway y, adentro, el de planner-service, con la duración de cada uno. Para encontrar un pedido puntual, buscá por el tag `correlation.id=prueba-1`.

### Agente de IA

planner-service arma las propuestas de plan con un agente de IA sobre [Groq](https://console.groq.com) y, si la IA falla, con el motor de reglas (RN-09, [ADR-010](docs/adr/ADR-010-resiliencia.md)). Se configura en el `.env` local:

| Variable | Valor por defecto | Para qué |
|---|---|---|
| `AI_API_KEY` | *(vacía)* | Clave de Groq, de https://console.groq.com/keys. **Va sólo en tu `.env`**, nunca en el repositorio. |
| `AI_MODEL` | `openai/gpt-oss-120b` | Modelo de Groq. Los modelos disponibles cambian: se listan con `GET https://api.groq.com/openai/v1/models` |
| `AI_BASE_URL` | `https://api.groq.com/openai/v1` | API de Groq compatible con OpenAI |
| `AI_TIMEOUT` | `8s` | Presupuesto total de la llamada, reintento incluido (ADR-005) |

Después de cambiar el `.env`, recreá el servicio con `docker compose up -d planner-service`.

| Configuración | Qué devuelve `POST /exams/{id}/proposals` |
|---|---|
| Sin `AI_API_KEY` | `generatedBy: "reglas"`, sin avisos. Al arrancar, planner-service avisa una vez en el log que usa sólo el motor de reglas. |
| Con una clave válida | `generatedBy: "ia"`: el plan lo armó Groq y pasó las mismas reglas que la confirmación. |
| Con una clave inválida, o si Groq falla, tarda más de 8 s o responde algo inválido | `generatedBy: "reglas"` y `warnings: ["AI_FALLBACK"]`. En `docker compose logs planner-service` queda un `WARN` en JSON con el `correlationId` del pedido, el motivo y el error. Después de 3 fallas seguidas, el *circuit breaker* deja de llamar a Groq durante 30 s. |

Para probarlo a través del gateway, con el `TOKEN` de prueba de la sección anterior:

```bash
H=(-H Content-Type:application/json -H X-Correlation-Id:prueba-ia -H "Authorization: Bearer $TOKEN")
G=localhost:8080/api/planner
curl "${H[@]}" -X POST $G/subjects -d '{"name":"Análisis II","difficulty":3}'          # devuelve el id de la materia
curl "${H[@]}" -X PUT $G/availability -d '{"weeklySlots":[{"weekday":1,"startTime":"18:00","endTime":"21:00"},{"weekday":3,"startTime":"18:00","endTime":"21:00"}],"maxDailyMinutes":120,"preferredSessionMinutes":90,"timezone":"America/Argentina/Cordoba"}'
curl "${H[@]}" $G/availability                                                      # la misma forma que el PUT; 404 AVAILABILITY_NOT_SET si nunca se cargó
curl "${H[@]}" -X POST $G/exams -d '{"subjectId":"<id de la materia>","type":"parcial","startsAt":"2026-11-10T09:00:00-03:00","topics":["Integrales dobles","Series"]}'
curl "${H[@]}" -X POST $G/exams/<id del examen>/proposals
docker compose logs planner-service | grep prueba-ia
```

### Frontend

El frontend (`frontend/`, React + TypeScript con Vite y Tailwind) sólo habla con el api-gateway, en `/api`. Con `docker compose` lo sirve nginx, que reenvía `/api` al gateway. Para desarrollarlo con recarga en caliente, con el backend levantado:

```bash
cd frontend
npm install
npm run dev                 # http://localhost:5173, reenvía /api a http://localhost:8080
```

Para entrar hay que registrarse o iniciar sesión: el frontend llama a `/api/users/register` y `/api/users/login`, guarda el JWT en el navegador y lo manda como `Authorization: Bearer ...` en cada pedido (nunca `X-User-Id`, que lo pone el gateway). Si el gateway responde 401 (token vencido o inválido), vuelve a la pantalla de ingreso; cerrar sesión borra el token. Los exámenes se leen de `GET /exams` y se editan o cancelan desde su detalle en el calendario. La disponibilidad se guarda con `PUT /availability` y se lee con `GET /availability`, con la misma forma.

### Estructura del repositorio

| Carpeta | Contenido |
|---|---|
| `services/` | Un módulo de Go por servicio (`api-gateway`, `users-service`, `planner-service`, `forum-service`, `notification-service`), con el ejecutable en `cmd/<nombre>/` y su `Dockerfile` |
| `frontend/` | Interfaz web (React + TypeScript, Vite) con su `Dockerfile` y la configuración de nginx |
| `docs/` | Arquitectura, ADR y contrato de la capacidad publicada |
| `tests/e2e/` | [Tests end-to-end](tests/e2e/README.md) del flujo principal contra el sistema levantado |
| `deploy/` | Inicialización de las bases y [despliegue en Azure](deploy/azure/README.md) |

### Entorno desplegado

El sistema se despliega en una máquina virtual de Azure con el mismo `docker compose` y Caddy adelante con HTTPS: el paso a paso está en [deploy/azure/README.md](deploy/azure/README.md). La URL pública (la app y la capacidad en `/v1`) la vamos a publicar acá cuando esté levantada.

**Integración con otros grupos** (asignada por la cátedra):

| Dirección | Grupo | Capacidad | Dónde |
|---|---|---|---|
| Exportamos | Grupo 4, Agencia de viajes | **Registro y consulta de documentos del foro**, que usan para guías de viaje compartidas | forum-service · [contrato](docs/contracts/) · [ADR-014](docs/adr/ADR-014-contrato-documentos.md) |
| Consumimos | Grupo 1, Gimnasio | **Programa de beneficios**: premia al estudiante cuando se aprueba un resumen suyo | forum-service · [ADR-009](docs/adr/ADR-009-consumo-del-proveedor.md) |

El contrato que exportamos y su **mock** (Prism, puerto `4010`) ya están disponibles para que el Grupo 4 se integre. La capacidad que habíamos publicado antes (generar un plan de estudio) quedó retirada sin llegar a implementarse ([ADR-008](docs/adr/ADR-008-contrato-propio.md), reemplazado por el ADR-014).

## Documentación

| Documento | Contenido |
|---|---|
| [`SPEC.md`](SPEC.md) | Alcance, funcionalidades, reglas de negocio y criterios de aceptación |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Arquitectura general, servicios, datos, comunicaciones y diagramas |
| [`docs/adr/`](docs/adr/) | Registro de decisiones arquitectónicas (ADR) |
| [`docs/contracts/`](docs/contracts/) | Contrato versionado de la capacidad publicada para otros grupos |
| [`docs/POSTMORTEM.md`](docs/POSTMORTEM.md) | Informe de la caída controlada |

## Forma de trabajo

- `main` contiene siempre la última versión estable y evaluable.
- Cada tarea se desarrolla en su propia rama (`feature/...`, `docs/...`, `fix/...`) y se integra a `main` con un *pull request* revisado por otra integrante.
