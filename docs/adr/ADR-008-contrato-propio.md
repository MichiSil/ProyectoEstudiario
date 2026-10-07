# ADR-008: Contrato propio

- **Estado:** Propuesta
- **Fecha:** 2026-10-07
- **Decisión del enunciado:** D8
- **Reemplaza a:** —

## Contexto

El enunciado pide publicar una capacidad del sistema para que la use otro grupo, con un **contrato formal y versionado** en `docs/contracts/`, que indique las operaciones disponibles, los datos de entrada y salida, los errores y cómo interpretarlos, las condiciones de autenticación, idempotencia y uso, la versión vigente y ejemplos suficientes. El grupo consumidor tiene que poder integrarse **sin depender de explicaciones privadas**, y la capacidad debe estar desplegada en la nube y operativa hasta el final de la evaluación.

El grupo eligió publicar la capacidad de **generar un plan de estudio** (RF-13 del [SPEC](../../SPEC.md)): el consumidor envía los datos de un examen (fecha, temas y dificultad de 1 a 4) y una disponibilidad (franjas semanales, máximo de minutos por día y duración preferida de las sesiones), y recibe las sesiones propuestas, cada una con fecha, horario, duración, tema, objetivo y explicación. Según el [ADR-001](ADR-001-limites-de-servicios.md), el dueño de esta capacidad es **planner-service**, y el sistema del otro grupo entra directamente a ese servicio, sin pasar por nuestro api-gateway.

Las reglas que la capacidad tiene que respetar ya existen en el SPEC: anticipación según la dificultad (RN-06), cantidad de sesiones y repaso final (RN-07) y propuesta por reglas fijas cuando la IA no está disponible (RN-09).

Condiciones que influyen en el diseño:

- **El consumidor es un equipo que no controlamos.** Necesita poder desarrollar contra el contrato antes de que exista la implementación (Entrega 1: "contrato con mock").
- **La generación depende de un proveedor de IA** lento y no determinista.
- **Generar un plan tiene costo** (llamadas a la IA), así que hace falta identificar al consumidor y limitar el uso.
- **El consumidor tiene que hacer un test de contrato** que detecte cambios incompatibles, así que el contrato debe ser procesable por herramientas.

## Opciones consideradas

**Estilo de la interfaz**

1. **REST con JSON, descripto con OpenAPI (elegida).** Es el estilo que cualquier grupo puede consumir desde cualquier lenguaje, probar con `curl` y validar con herramientas estándar. OpenAPI permite generar el mock, clientes y validadores a partir del mismo archivo.
2. **gRPC con Protocol Buffers.** Tiene contratos estrictos y es eficiente, pero obliga al consumidor a manejar *stubs* generados y HTTP/2, es más difícil de probar a mano y no hay un mock tan simple de levantar.
3. **Mensajería asíncrona descripta con AsyncAPI.** El consumidor publicaría un pedido y recibiría el plan por otra cola. Lo obligaría a conectarse a nuestro broker (exponer infraestructura interna) y a correlacionar pedidos y respuestas, cuando en su flujo necesita el plan en el momento.
4. **GraphQL.** Es flexible para consultas, pero acá hay una sola operación con una forma fija: agrega complejidad sin beneficio.

**Forma de la operación**

1. **Síncrona: el pedido devuelve la propuesta (elegida).** El peor caso está acotado a 10 s, porque si la IA no responde, el motor de reglas arma el plan (ver [ADR-005](ADR-005-comunicacion.md)).
2. **Asíncrona con `202 Accepted` y consulta posterior.** Tolera generaciones largas, pero obliga al consumidor a consultar periódicamente y a guardar estado. Si la latencia de la IA lo justificara, se puede agregar más adelante como operación nueva sin romper la v1.

**Autenticación**

1. **API key por grupo consumidor en un encabezado (elegida).** Es simple de usar y de rotar, e identifica al consumidor para el cupo y la idempotencia.
2. **OAuth 2.0 *client credentials*.** Es más robusto, pero requiere un servidor de autorización y suma un paso más al consumidor, sin que el riesgo lo justifique.
3. **JWT de users-service.** Obligaría a que la persona tenga cuenta en Estudiario, y RF-13 pide justamente lo contrario.
4. **mTLS.** Es muy seguro, pero difícil de operar entre grupos y en la plataforma de nube.

**Versionado**

1. **Versión mayor en la ruta (`/v1`) más versionado semántico del documento (elegida).** Se ve en cada pedido, es fácil de enrutar y de probar con `curl`, y permite tener dos versiones mayores conviviendo.
2. **Versión en un encabezado o en el *media type*** (`Accept: application/vnd.estudiario.v1+json`). Deja las URLs limpias, pero la versión queda escondida, es más fácil equivocarse y complica las pruebas y los logs.
3. **Sin versión explícita.** Cualquier cambio podría romper al consumidor sin aviso.

## Decisión

### Diseño

| Aspecto | Decisión |
|---|---|
| **Formato del contrato** | OpenAPI **3.0.3** en [`docs/contracts/planner-v1.yaml`](../contracts/planner-v1.yaml), con la guía de uso en [`docs/contracts/README.md`](../contracts/README.md). Se eligió 3.0.3 sobre 3.1 por ser la versión con mejor soporte en generadores y validadores (por ejemplo, en Go). |
| **Operaciones** | `POST /v1/study-plan-proposals` genera una propuesta; `GET /health` informa si el servicio puede atender pedidos. |
| **Semántica** | La operación devuelve una **propuesta**, no reserva nada (RN-08) y **no guarda estado del consumidor**: cada pedido es independiente y Estudiario no conoce el resto del calendario de la persona. Lo único que se persiste es el registro de idempotencia. |
| **Garantías** | El contrato enumera las propiedades que cumple toda respuesta `200`: dentro de la ventana de RN-06, dentro de las franjas, sin superposiciones, sin superar la capacidad diaria, duración fija y cierre con repaso (RN-07). Así el consumidor puede confiar en el plan y testearlo, sin importar si lo armó la IA o el motor de reglas. |
| **Degradación** | Si la IA falla, la respuesta sigue siendo `200`, con `generatedBy: reglas` y el aviso `AI_FALLBACK` (RN-09). La falla de una dependencia interna nunca se le traslada al consumidor como error. |
| **Datos** | Campos en inglés y `camelCase`, como el resto de las APIs y eventos del sistema (`authorId`, `eventId`); los valores del dominio en español (`parcial`, `teoria`, `lunes`), como en el SPEC. Fechas `YYYY-MM-DD` y horas `HH:mm` locales a una `timezone` IANA explícita, para evitar ambigüedades con el horario de las sesiones. El pedido rechaza campos desconocidos (`additionalProperties: false`), así un error de tipeo no pasa desapercibido. |
| **Autenticación** | API key en el encabezado `X-API-Key`, una por grupo consumidor. |
| **Idempotencia** | Encabezado `Idempotency-Key` **obligatorio**. Misma clave y mismo cuerpo dentro de las 24 h devuelven la misma respuesta (`Idempotent-Replayed: true`); misma clave con otro cuerpo devuelve `422 IDEMPOTENCY_KEY_REUSED`; un pedido concurrente con la misma clave devuelve `409`. El alcance es la API key. Se sigue el borrador de la IETF *The Idempotency-Key HTTP Header Field*. |
| **Errores** | Formato *Problem Details* (RFC 9457) con un `code` estable para decidir, `errors[]` por campo en las validaciones y `correlationId`. `400` si no se puede leer el pedido, `401` sin credenciales válidas, `409` pedido en curso, `422` datos inválidos o reglas de negocio, `429` cupo superado, `500` error interno y `503` servicio no disponible. Cada `code` tiene su fila en la tabla de errores del README, que indica si se puede reintentar. |
| **Uso** | Cupo de 30 pedidos por minuto por API key, informado con los encabezados `RateLimit-*` y `Retry-After`. Correlación con `X-Correlation-Id`. |
| **Tiempos** | planner-service responde en menos de 10 s. Al consumidor se le recomienda un timeout de 12 s y reintentos con espera exponencial, siempre con la misma `Idempotency-Key`. |

**Por qué la `Idempotency-Key` es obligatoria aunque la operación no reserve nada:** el agente de IA no es determinista y cada generación tiene costo. Sin la clave, un reintento por timeout devolvería un plan distinto del que la persona ya vio y consumiría una nueva llamada a la IA. Hacerla obligatoria, en lugar de opcional, evita que el consumidor descubra el problema recién en producción.

**Dentro de planner-service,** la ruta pública es un adaptador de entrada más sobre el mismo caso de uso que usa el frontend para pedir una propuesta. Lo único que cambia es de dónde salen los datos: para el estudiante, de su materia, su examen y su disponibilidad guardados; para el consumidor externo, del cuerpo del pedido. Así las reglas RN-06, RN-07 y RN-09 se implementan una sola vez. La arquitectura interna del servicio se define en el ADR-002 (D2).

### Publicación

- **El contrato vive en el repositorio**, en `docs/contracts/`, y llega a `main` como cualquier otro cambio: rama propia y *pull request* revisado. La versión publicada es la de `main`.
- **Mock desde la Entrega 1.** Con [Prism](https://docs.stoplight.io/docs/prism) se levanta un servidor que valida los pedidos contra el contrato y responde con sus ejemplos. Está en el `docker-compose.yml` del sistema y también se puede levantar solo con Docker o `npx`. El consumidor puede forzar cada respuesta de error con el encabezado `Prefer` para probar su manejo de fallas.
- **Implementación real en la nube** a partir de la Entrega 2, con una URL pública que se agrega al README del contrato y a la lista de servidores del YAML. Se mantiene operativa hasta el final de la evaluación.
- **Credenciales:** cada grupo consumidor recibe su API key por un canal privado. Las claves nunca se suben al repositorio (ni al nuestro ni al del consumidor). En planner-service se guardan sólo como *hash* y se configuran por variable de entorno o secreto de la plataforma.
- **Ejemplos:** el YAML incluye ejemplos de cada pedido, de cada respuesta exitosa y de cada error. El README agrega comandos `curl` y cuerpos listos para usar en `docs/contracts/examples/`.

### Compatibilidad

- **Lector tolerante.** El contrato le pide al consumidor que ignore los campos de respuesta que no conozca y que acepte valores nuevos en `warnings[].code` y `sessions[].kind`. A cambio, nos comprometemos a no hacer cambios incompatibles dentro de `/v1`.
- **Cambios compatibles** (versión menor, misma ruta): agregar campos opcionales al pedido, agregar campos a la respuesta, agregar códigos de aviso o de error y agregar operaciones.
- **Cambios incompatibles** (nueva versión mayor `/v2`): quitar o renombrar campos, volver obligatorio un campo opcional, cambiar tipos, formatos o el significado de un código, y endurecer validaciones.
- **Verificación de nuestro lado:** los tests de planner-service validan las respuestas reales contra `planner-v1.yaml`, y cada *pull request* que modifique el contrato se compara con la versión de `main` usando una herramienta de detección de cambios incompatibles (por ejemplo, `oasdiff breaking`). Si aparece un cambio incompatible sin una versión mayor nueva, el *pull request* no se aprueba.

### Estrategia de versionado

- **Versionado semántico del documento** (`info.version`): parche para aclaraciones, menor para cambios compatibles y mayor para incompatibles.
- **La versión mayor va en la ruta** (`/v1`) y en el nombre del archivo (`planner-v1.yaml`). Una `v2` sería un archivo nuevo (`planner-v2.yaml`), y las dos versiones convivirían en el servicio.
- **Retiro de una versión mayor:** cuando sale la siguiente, la anterior sigue funcionando al menos 4 semanas y, en cualquier caso, hasta el final de la evaluación. Durante ese tiempo responde con los encabezados `Deprecation` y `Sunset` (RFC 8594), y además se le avisa directamente al grupo consumidor.
- **Trazabilidad:** el README del contrato tiene un historial de cambios por versión. Un cambio de versión mayor exige además un ADR que reemplace a este.

## Consecuencias

**Positivas**

- El grupo consumidor puede desarrollar y probar su integración desde la Entrega 1 contra el mock, incluidos todos los casos de error.
- El contrato es procesable: sirve para generar el mock, para validar la implementación y para que el consumidor escriba su test de contrato.
- Una falla de la IA no rompe la integración del consumidor: siempre recibe un plan válido mientras planner-service esté en pie.
- Las garantías explícitas del plan le permiten al consumidor testear el resultado sin conocer cómo se genera.
- Los reintentos del consumidor son seguros y no consumen cupo ni llamadas a la IA.

**Negativas / limitaciones aceptadas**

- Como la capacidad no pasa por el api-gateway, planner-service tiene que resolver por su cuenta, para esta ruta, la autenticación por API key, el cupo de uso y la correlación.
- La idempotencia obliga a guardar respuestas durante 24 h en la base de planner-service. Es un dato nuevo, que el [ADR-003](ADR-003-persistencia.md) tendrá que sumar en su validación de la Entrega 2.
- El cupo de uso se cuenta en memoria por instancia. Si planner-service corre en varias instancias, el límite efectivo se multiplica por la cantidad de instancias. Se acepta para el volumen esperado.
- La API key es un secreto compartido: si se filtra, hay que rotarla. No ofrece la granularidad de OAuth.
- Rechazar campos desconocidos en el pedido hace que agregar un campo opcional sea compatible para el consumidor, pero que él no pueda enviar campos nuevos antes de que los publiquemos.
- El mock no planifica ni aplica reglas de negocio: devuelve siempre los ejemplos del contrato. Las diferencias están documentadas en el README del contrato.
- Mantener dos versiones mayores en paralelo, si llegara a ser necesario, duplica el trabajo de prueba.

**A validar en la Entrega 2**

- Que la implementación real cumpla el contrato: tests de planner-service validando las respuestas contra el YAML.
- Que el tiempo de respuesta en el peor caso (IA caída o lenta) quede por debajo de los 10 s comprometidos, con datos de las pruebas de carga.
- Publicar la URL de la nube y actualizar este ADR si el contrato tuvo que cambiar (el enunciado pide actualizar D8 en ese caso).
