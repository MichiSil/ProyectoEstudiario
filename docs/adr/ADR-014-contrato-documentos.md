# ADR-014: Contrato propio: registro y consulta de documentos

- **Estado:** Aceptada (con los cambios de la sección [*Actualización (2026-11-09)*](#actualización-2026-11-09))
- **Fecha:** 2026-10-29
- **Decisión del enunciado:** D8
- **Reemplaza a:** [ADR-008](ADR-008-contrato-propio.md) (contrato de "generar un plan de estudio")

## Contexto

El [ADR-008](ADR-008-contrato-propio.md) eligió publicar la capacidad de **generar un plan de estudio**, expuesta por planner-service. En la Entrega 2 su implementación se pospuso por indicación de la cátedra, y quedaron publicados sólo el contrato y el mock.

Después, la cátedra **asignó las integraciones entre grupos**:

- **Estudiario exporta al Grupo 4 (Agencia de viajes)** el **registro y la consulta de documentos del foro**, adaptado a guías o documentos de viaje. El Grupo 4 lo usa para **consultar guías de viaje compartidas**.
- **Estudiario consume del Grupo 1 (Gimnasio)** su **programa de beneficios**, para premiar la participación de los usuarios. Ese consumo se decide en el [ADR-009](ADR-009-consumo-del-proveedor.md).

La capacidad asignada no es la que habíamos elegido: el plan de estudio deja de ser la capacidad publicada y el contrato del ADR-008 no sirve para el Grupo 4. Lo que el Grupo 4 necesita ya existe en el diseño del foro: subir un PDF con sus datos, moderarlo y buscar los publicados (RF-14 a RF-21, [ADR-003](ADR-003-persistencia.md), [ADR-006](ADR-006-busqueda.md)).

El enunciado sigue pidiendo lo mismo para el contrato: formal y versionado en `docs/contracts/`, con las operaciones, los datos, los errores, la autenticación, la idempotencia, la versión vigente y ejemplos suficientes para integrarse sin explicaciones privadas, y la capacidad desplegada en la nube.

## Opciones consideradas

**Qué documento publicar**

1. **Un recurso propio de viajes** (`/v1/travel-guides` con destino, duración, presupuesto). Le facilita la vida al Grupo 4, pero mete el dominio de otro sistema en el nuestro: cualquier campo nuevo de viajes nos obligaría a cambiar el contrato, y no sirve para ningún otro consumidor.
2. **El documento del foro, genérico (elegida).** Título, descripción, categoría, etiquetas, autor y PDF. El destino va en `category` y los datos de viaje en `tags`. Es lo que ya modela el foro, y el contrato no depende del dominio del consumidor.

**Qué pasa con lo que registra el consumidor**

1. **Publicarlo directamente.** Es lo más simple para el Grupo 4, pero saltea la moderación (RN-19): un tercero podría publicar cualquier cosa en el foro de Estudiario sin control.
2. **Moderarlo igual que un resumen del foro (elegida).** Entra como `pendiente` y lo aprueba o rechaza un administrador. El consumidor sigue el estado en `GET /v1/submissions` o en el detalle. Los estudiantes de Estudiario y el consumidor ven el mismo contenido moderado.

**Cómo se envía el archivo**

1. **El PDF en base64 dentro de un JSON.** Un solo formato, pero agranda el pedido un 33 % y obliga a cargar el archivo entero en memoria para decodificarlo.
2. **Una URL de donde descargarlo.** Deja el pedido liviano, pero nos hace depender de la disponibilidad de un servidor del consumidor y abre la puerta a descargar contenido de cualquier lado.
3. **`multipart/form-data` con un campo por dato y el PDF como archivo (elegida).** Es el formato estándar para subir archivos, se prueba con `curl -F` y lo soporta cualquier librería HTTP. Una primera versión con los datos en una parte JSON se descartó: el mock (Prism) no la interpreta y es más difícil de armar para el consumidor.

**Cómo se entera el consumidor de la moderación**

1. **Un *webhook* que avisa al consumidor.** Es inmediato, pero obliga al Grupo 4 a exponer un endpoint público y a los dos grupos a manejar firmas y reintentos de entrega.
2. **Consulta del estado (elegida para v1).** `GET /v1/submissions?status=pendiente`. Es simple y no le pide nada al consumidor. Un *webhook* se puede agregar en una versión menor sin romper v1.

**Qué servicio la expone**

El dueño de los documentos es **forum-service** ([ADR-001](ADR-001-limites-de-servicios.md)), así que la capacidad la expone él, directamente y sin pasar por el api-gateway, por el mismo motivo que el ADR-008: el gateway es para nuestro frontend y valida JWT de usuarios, no API keys de otros sistemas.

## Decisión

### Diseño

| Aspecto | Decisión |
|---|---|
| **Formato del contrato** | OpenAPI **3.0.3** en [`docs/contracts/documents-v1.yaml`](../contracts/documents-v1.yaml), con la guía de uso en [`docs/contracts/README.md`](../contracts/README.md). |
| **Operaciones** | `POST /v1/documents` (registrar), `GET /v1/documents` (buscar aprobados), `GET /v1/documents/{id}` (detalle), `GET /v1/documents/{id}/file` (descargar PDF), `GET /v1/submissions` (mis registros) y `GET /health`. |
| **Visibilidad** | Los documentos **aprobados** los ve cualquier consumidor, vengan del foro (`source: estudiario`) o de un consumidor (`source: externo`). Los **pendientes y rechazados** sólo los ve la API key que los registró. Un eliminado no lo ve nadie (`404`). |
| **Moderación** | Lo registrado entra como `pendiente` (RN-19) y lo moderan los administradores de Estudiario con las mismas reglas que un resumen (RN-21 a RN-23). El rechazo trae su motivo (RN-22). |
| **Búsqueda** | La misma de RF-19 y RF-20 sobre Solr ([ADR-006](ADR-006-busqueda.md)): texto, filtros por categoría, etiquetas y calificación mínima, orden por relevancia, calificación o fecha, y páginas de hasta 50 (RN-27). Un documento aprobado aparece en como máximo 1 minuto (RN-26). |
| **Archivo** | PDF de hasta 10 MB (RN-20), guardado en MinIO como cualquier resumen ([ADR-003](ADR-003-persistencia.md)). La descarga pasa por forum-service, que verifica la visibilidad antes de entregarlo. |
| **Datos del autor** | `authorName` (visible) y `authorExternalId` (el id del usuario en el sistema consumidor, que Estudiario guarda sin interpretar y sólo le devuelve a quien lo registró). Los usuarios del consumidor no necesitan cuenta en Estudiario. |
| **Autenticación** | API key por grupo consumidor en `X-API-Key`, como en el ADR-008. Además de identificar al consumidor para el cupo y la idempotencia, define **qué documentos son suyos**. |
| **Idempotencia** | `Idempotency-Key` **obligatoria** en el registro: misma clave y mismo cuerpo dentro de las 24 h devuelven el mismo documento (`Idempotent-Replayed: true`); misma clave con otro cuerpo, `422 IDEMPOTENCY_KEY_REUSED`; un pedido concurrente con la misma clave, `409 REQUEST_IN_PROGRESS`. Sin la clave, un reintento por timeout subiría el mismo PDF dos veces y lo pondría dos veces en la cola de moderación. |
| **Errores** | *Problem Details* (RFC 9457) con un `code` estable y `correlationId`, igual que el resto del sistema. Se suman `413 FILE_TOO_LARGE`, `415 UNSUPPORTED_FILE_TYPE` y `503 SEARCH_UNAVAILABLE`, que avisa que la búsqueda no está pero el detalle y la descarga sí. |
| **Uso** | Cupo de 60 pedidos por minuto por API key, de los cuales hasta 10 registros. Informado con `RateLimit-*` y `Retry-After`. |
| **Tiempos** | Menos de 2 s para buscar y ver un detalle, menos de 10 s para registrar un PDF de 10 MB. Al consumidor se le recomiendan timeouts de 5 s para leer y 20 s para registrar. |
| **Lector tolerante** | Los campos de la respuesta que el consumidor no conozca se ignoran, y `status` y `source` pueden sumar valores. A cambio, no hacemos cambios incompatibles dentro de `/v1`. |

### Publicación

- El contrato vive en `docs/contracts/` y llega a `main` por *pull request* revisado. La versión publicada es la de `main`.
- **Mock desde el primer día:** Prism sobre `documents-v1.yaml`, en el `docker-compose.yml` como `contract-mock` (puerto 4010) y también levantable con Docker o `npx`. El consumidor puede forzar cada error con el encabezado `Prefer`.
- **Implementación real en la nube** antes de la presentación final, con su URL pública en el README del contrato. Se mantiene operativa hasta el final de la evaluación.
- La API key del Grupo 4 se entrega por un canal privado y nunca se sube a ningún repositorio. forum-service guarda sólo su *hash* y la configura por variable de entorno o secreto de la plataforma.

### Compatibilidad y versionado

Se mantienen las reglas del ADR-008: versionado semántico del documento, versión mayor en la ruta (`/v1`) y en el nombre del archivo (`documents-v1.yaml`), cambios compatibles en versiones menores, `/v2` para los incompatibles, convivencia de al menos 4 semanas con los encabezados `Deprecation` y `Sunset`, y un historial de cambios en el README del contrato. Cada *pull request* que modifique el contrato se compara con la versión de `main` con una herramienta de detección de cambios incompatibles (por ejemplo, `oasdiff breaking`).

### Qué pasa con el contrato anterior

- `planner-v1.yaml`, su guía y sus ejemplos se mueven a [`docs/contracts/retirados/`](../contracts/retirados/) con una nota de retiro. **No se borran**, para conservar la historia de la decisión.
- Como `/v1/study-plan-proposals` nunca se implementó ni se publicó en la nube, retirarlo no afecta a ningún consumidor.
- RF-13 del SPEC pasa a describir la capacidad de documentos. La generación del plan sigue existiendo para los estudiantes de Estudiario (RF-08), sólo deja de publicarse para otros sistemas.

## Consecuencias

**Positivas**

- La capacidad publicada coincide con la asignación de la cátedra y reutiliza lo que el foro ya tiene que hacer: el mismo modelo, la misma moderación, el mismo almacenamiento de archivos y la misma búsqueda. No hay un segundo camino de datos para mantener.
- El contrato no depende del dominio de viajes: si otro grupo necesitara compartir documentos, usaría el mismo.
- El Grupo 4 puede desarrollar y probar su integración desde hoy contra el mock, incluidos todos los errores.
- Lo que publica un tercero pasa por la misma moderación que lo que publica un estudiante, así que el foro no pierde control sobre su contenido.

**Negativas / limitaciones aceptadas**

- La capacidad pasa a depender de forum-service, que hoy es un esqueleto. Implementarlo es ahora prioridad para la presentación final.
- El consumidor no se entera solo de la moderación: tiene que consultar el estado. Se acepta para v1; un *webhook* queda como posible versión menor.
- Un documento registrado por el Grupo 4 tarda en publicarse lo que tarde un administrador en moderarlo. El contrato no promete un plazo de moderación, sólo el de aparición en la búsqueda una vez aprobado.
- Como en el ADR-008, forum-service tiene que resolver por su cuenta, para estas rutas, la API key, el cupo y la correlación, y la idempotencia guarda respuestas durante 24 h en su base.
- El cupo se cuenta por instancia. Con forum-service en dos instancias ([ADR-012](ADR-012-balanceo-de-carga.md)) el límite efectivo se duplica. Se acepta para el volumen esperado.
- El mock no guarda, no modera ni controla el tamaño y el tipo del archivo. Las diferencias están documentadas en el README del contrato.

**A validar para la presentación final**

- Que la implementación de forum-service cumpla el contrato: tests que validen las respuestas reales contra el YAML, y Prism en modo *proxy* contra el servicio.
- Que el Grupo 4 se integre usando sólo el contrato y la guía.
- Publicar la URL de la nube y medir los tiempos comprometidos con una prueba de carga.

## Actualización (2026-11-09)

### Qué pidió la cátedra

En la corrección de la Entrega 2, la cátedra pidió cuatro cambios a la capacidad:

1. **Que la agencia la use sin estar logueada, sólo con la API key.** Ya funcionaba así: las rutas `/v1/*` no usan el JWT del api-gateway, sólo `X-API-Key`. No cambia.
2. **Que `source` diga quién subió el documento.** Lo registrado por el Grupo 4 salía como `externo`, sin decir de qué sistema venía.
3. **Que los PDF de un source que no es `estudiario` estén en una parte pública**, para que cualquiera los pueda ver sin login ni API key.
4. **Que la persona de la agencia que sube el documento pueda entrar a Estudiario**, con una cuenta creada para ella y una contraseña generada que le llega por mail.

### Cambios

| Pedido | Decisión |
|---|---|
| `source` | Es el **nombre del consumidor** de la API key que registró el documento. En `FORUM_API_KEYS` el consumidor del Grupo 4 pasa a llamarse `agencia_de_viajes`. Los documentos ya guardados con `externo` lo conservan y se siguen leyendo igual: en el contrato, `externo` queda como valor **obsoleto**. |
| Archivos públicos | Los PDF de los documentos con un source distinto de `estudiario` se guardan en un bucket aparte de MinIO, **`public-documents`**, con permisos sólo para el usuario de forum-service. Los de los estudiantes siguen en `summaries` y siguen siendo privados. La nueva ruta **`GET /v1/public/documents/{id}/file`**, sin autenticación, entrega el PDF si el documento es de un source externo, está en el bucket público y no fue rechazado ni eliminado; si no, `404`. El documento informa esa ruta en **`file.publicUrl`** (`null` si el archivo no es público). |
| Cuenta para el autor | El registro suma el campo opcional **`authorEmail`**. Si viene, forum-service guarda en el *outbox* del documento, en la misma escritura que lo crea, el evento **`document.external-registered`** (con el sobre del [ADR-005](ADR-005-comunicacion.md) y en `data` el id, el título, el source y el autor con nombre, mail e id externo), y el relay lo publica en el exchange `forum.events`. **La cuenta la crea users-service**, que consume ese evento, genera la contraseña y la manda por mail. |

**Por qué el bucket público no es público en MinIO.** MinIO no se publica a internet ([ADR-003](ADR-003-persistencia.md)): los PDF públicos también salen a través de forum-service, que antes de entregarlos verifica que el documento no fue rechazado ni eliminado. Así un rechazo o una eliminación dejan de mostrar el archivo sin borrarlo ni moverlo: enseguida, salvo que al eliminar un aprobado falle la invalidación de la caché, que puede mantenerlo hasta 10 min ([ADR-007](ADR-007-cache.md)). En la nube la ruta llega sola, porque Caddy ya manda `/v1/*` a forum-service.

**Por qué la cuenta la crea users-service por evento.** Las cuentas son de users-service ([ADR-001](ADR-001-limites-de-servicios.md)): forum-service no las conoce ni las crea. Con el evento, registrar un documento no depende de que users-service o el servidor de mail respondan en ese momento ([ADR-005](ADR-005-comunicacion.md), [ADR-010](ADR-010-resiliencia.md)): si RabbitMQ está caído, el evento espera en el *outbox* y el relay lo publica cuando vuelve. Como el evento se crea junto con el documento, un reintento con la misma `Idempotency-Key` devuelve el documento ya registrado y **no publica otro evento**. notification-service no se entera: su cola sólo está enlazada a `summary.approved` y `summary.rejected`.

### Compatibilidad

Todos los cambios son compatibles con v1, según la política de este ADR: un valor nuevo en `source`, un campo nuevo opcional en el registro (`authorEmail`), un campo nuevo en la respuesta (`file.publicUrl`) y una operación nueva. El contrato pasa a la versión **1.1.0**, sin cambiar la ruta `/v1`, y el cambio queda en el historial del [README del contrato](../contracts/README.md#14-historial-de-cambios).

### Consecuencias

- **Los archivos externos son públicos desde que se suben.** Cualquiera que conozca el id de un documento **pendiente** puede ver su PDF antes de que un administrador lo modere. Se acepta porque es lo que pidió la cátedra y porque el id no es adivinable (26 caracteres aleatorios). Rechazarlo o eliminarlo lo saca de la ruta pública.
- **Lo moderado sigue moderándose.** El archivo es público, pero el documento sólo aparece en la búsqueda y en el foro cuando se aprueba (RN-19, RN-24).
- **Los documentos anteriores a la 1.1.0 no cambian:** conservan `source: externo` y su PDF queda en `summaries`, así que no tienen `publicUrl`. No se migran porque nadie los pidió públicos y moverlos agregaría un paso que puede fallar a mitad de camino.
- **forum-service guarda el mail del autor externo**, porque viaja en el evento del *outbox*. Es un dato personal: nunca se devuelve en las respuestas. Los documentos externos siguen sin generar el mail de moderación (RN-41), aunque traigan `authorEmail`.
- **La ruta pública no tiene cupo**, porque no hay API key con la que contarlo. Cada descarga pasa por forum-service, así que una avalancha de pedidos le pega a sus dos instancias ([ADR-012](ADR-012-balanceo-de-carga.md)). Se acepta para el volumen esperado.
- **La limpieza de PDF huérfanos** ([ADR-003](ADR-003-persistencia.md)) recorre los dos buckets.
- **Hay que renombrar el consumidor en la nube.** El `FORUM_API_KEYS` del `.env` de la nube tiene que usar `agencia_de_viajes` como nombre del consumidor del Grupo 4 (el hash de su clave no cambia). Si no, sus documentos nuevos salen con el nombre viejo como `source`.
- **El contrato del evento lo comparten forum-service y users-service**: cambiar sus campos exige acordarlo con quien mantiene users-service.
