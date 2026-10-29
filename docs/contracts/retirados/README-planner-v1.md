# Capacidad publicada: generar un plan de estudio

Estudiario publica para otros grupos la capacidad de **generar un plan de estudio**: el consumidor envía los datos de un examen y la disponibilidad horaria de una persona, y recibe las sesiones de estudio propuestas, cada una con fecha, horario, duración, tema, objetivo y explicación (RF-13 del [SPEC](../../SPEC.md)).

Este documento alcanza para integrarse sin hablar con nosotros. El contrato formal es [`planner-v1.yaml`](planner-v1.yaml) (OpenAPI 3.0.3). Si este README y el contrato no coinciden, vale el contrato. Las decisiones de diseño están en el [ADR-008](../adr/ADR-008-contrato-propio.md).

## Índice

1. [Versión vigente](#1-versión-vigente)
2. [Dónde está disponible](#2-dónde-está-disponible)
3. [Autenticación](#3-autenticación)
4. [Operaciones](#4-operaciones)
5. [Qué se envía y qué se recibe](#5-qué-se-envía-y-qué-se-recibe)
6. [Idempotencia](#6-idempotencia)
7. [Tiempos de espera, reintentos y cupo de uso](#7-tiempos-de-espera-reintentos-y-cupo-de-uso)
8. [Errores](#8-errores)
9. [Ejemplos con curl](#9-ejemplos-con-curl)
10. [Mock para desarrollar y probar](#10-mock-para-desarrollar-y-probar)
11. [Versionado y compatibilidad](#11-versionado-y-compatibilidad)
12. [Cómo verificar la integración contra el contrato](#12-cómo-verificar-la-integración-contra-el-contrato)
13. [Historial de cambios](#13-historial-de-cambios)

## 1. Versión vigente

| Versión mayor | Versión del contrato | Archivo | Ruta base | Estado |
|---|---|---|---|---|
| v1 | **1.0.0** | [`planner-v1.yaml`](planner-v1.yaml) | `/v1` | **Vigente** |

La versión del contrato también se informa en `GET /health` (campo `contractVersion`).

## 2. Dónde está disponible

| Entorno | URL base | Para qué |
|---|---|---|
| Mock | `http://localhost:4010` | Desarrollar y probar la integración desde hoy, sin depender de nuestra implementación (ver [sección 10](#10-mock-para-desarrollar-y-probar)) |
| Local | `http://localhost:8082` | planner-service levantado con `docker compose` desde este repositorio |
| Nube | *Se publica acá en la Entrega 2* | Implementación real, accesible desde internet y operativa hasta el fin de la evaluación |

La capacidad la expone **planner-service directamente**. No pasa por el api-gateway de Estudiario, que es sólo para nuestro frontend.

## 3. Autenticación

Cada pedido lleva una **API key** en el encabezado `X-API-Key`:

```http
X-API-Key: <clave entregada al grupo>
```

- Le damos una clave a cada grupo consumidor **por un canal privado** (mensaje directo al referente del grupo). Nunca está en este repositorio, y les pedimos que tampoco la suban al suyo: guárdenla como variable de entorno o secreto.
- La clave identifica al grupo consumidor. Sobre ella se cuentan el cupo de uso y el alcance de las claves de idempotencia.
- Si la clave falta o no es válida, la respuesta es `401 UNAUTHENTICATED`. Si creen que se filtró, avísennos y la rotamos.
- El mock acepta **cualquier** valor de `X-API-Key`. Sólo exige que el encabezado esté.
- `GET /health` no requiere clave.

No hace falta que la persona tenga cuenta en Estudiario: el plan se genera sólo con los datos del pedido.

## 4. Operaciones

| Método y ruta | Operación | Autenticación |
|---|---|---|
| `POST /v1/study-plan-proposals` | Genera una propuesta de plan de estudio | `X-API-Key` + `Idempotency-Key` |
| `GET /health` | Indica si el servicio puede atender pedidos | Ninguna |

## 5. Qué se envía y qué se recibe

### Pedido

```json
{
  "exam": {
    "subjectName": "Análisis Matemático II",
    "type": "parcial",
    "date": "2026-10-28",
    "difficulty": 3,
    "topics": ["Integrales dobles", "Integrales triples", "Cambio de variables"]
  },
  "availability": {
    "weeklySlots": [
      { "weekday": "lunes",  "startTime": "18:00", "endTime": "21:00" },
      { "weekday": "sabado", "startTime": "10:00", "endTime": "13:00" }
    ],
    "maxDailyMinutes": 120,
    "preferredSessionMinutes": 90
  },
  "timezone": "America/Argentina/Cordoba",
  "startDate": "2026-10-08"
}
```

| Campo | Obligatorio | Descripción |
|---|---|---|
| `exam.date` | Sí | Fecha del examen (`YYYY-MM-DD`). No puede estar en el pasado. |
| `exam.difficulty` | Sí | `1` fácil, `2` media, `3` difícil, `4` muy difícil. |
| `exam.topics` | Sí | Entre 1 y 30 temas, en el orden en que conviene estudiarlos. |
| `exam.subjectName` | No | Nombre de la materia. Se usa en las explicaciones. |
| `exam.type` | No | `parcial` (por defecto), `final`, `recuperatorio` o `entrega`. |
| `availability.weeklySlots` | Sí | Entre 1 y 28 franjas semanales. `weekday` va en minúsculas y sin tildes (`lunes` … `domingo`) y las horas en formato `HH:mm` de 24 h. Las franjas de un mismo día no pueden superponerse. |
| `availability.maxDailyMinutes` | Sí | Máximo de minutos de estudio por día (15 a 720). |
| `availability.preferredSessionMinutes` | Sí | Duración de cada sesión (15 a 240). |
| `timezone` | No | Zona horaria IANA. Por defecto, `America/Argentina/Cordoba`. Todas las fechas y horas del pedido y de la respuesta son locales a esta zona. |
| `startDate` | No | Primer día en que se pueden proponer sesiones. Por defecto, hoy. |

Los campos que no están en el contrato se rechazan con `422 VALIDATION_ERROR`, para que un error de tipeo no pase desapercibido.

### Respuesta `200 OK`

```json
{
  "proposalId": "0f6c2a5e-6d1b-4c1e-9a43-2b7f0d4e8a11",
  "generatedBy": "ia",
  "generatedAt": "2026-10-07T21:15:42Z",
  "timezone": "America/Argentina/Cordoba",
  "exam": { "subjectName": "Análisis Matemático II", "type": "parcial", "date": "2026-10-28", "difficulty": 3 },
  "window": { "from": "2026-10-13", "to": "2026-10-27", "anticipationDays": 15 },
  "totals": { "sessionCount": 7, "totalMinutes": 630 },
  "explanation": "Análisis Matemático II es difícil y entran 3 temas, así que el estudio empieza 15 días antes…",
  "sessions": [
    {
      "order": 1,
      "date": "2026-10-14",
      "startTime": "18:00",
      "endTime": "19:30",
      "durationMinutes": 90,
      "topic": "Integrales dobles",
      "objective": "Leer teoría de integrales dobles",
      "kind": "teoria",
      "explanation": "Es la base de los otros dos temas, por eso va primero."
    }
  ],
  "warnings": []
}
```

El ejemplo completo, con las 7 sesiones, está en el contrato (ejemplo `generadoPorIA`).

| Campo | Descripción |
|---|---|
| `proposalId` | Identificador de la propuesta. Se repite si se reintenta con la misma `Idempotency-Key`. |
| `generatedBy` | `ia` si la armó el agente de IA; `reglas` si la armó el motor de reglas fijas porque la IA no estaba disponible. |
| `window` | Días en los que se propusieron sesiones. `to` es siempre el día anterior al examen. |
| `sessions[]` | Sesiones en orden cronológico. `kind` vale `teoria`, `ejercicios`, `simulacro` o `repaso`. |
| `warnings[]` | Avisos que no impiden generar el plan (ver abajo). Puede estar vacía. |

### Qué garantiza cada propuesta

Toda respuesta `200` cumple estas reglas, sin importar si la armó la IA o el motor de reglas:

1. **Anticipación según la dificultad (RN-06).** Las sesiones empiezan como mucho 5, 10, 15 o 21 días antes del examen (fácil, media, difícil o muy difícil). Si falta menos, se usa el tiempo que queda y se avisa con `SHORT_WINDOW`.
2. **Dentro de la disponibilidad.** Cada sesión empieza y termina dentro de una de las franjas enviadas.
3. **Antes del examen.** Todas las sesiones son en días anteriores a la fecha del examen.
4. **Sin superposiciones** entre las sesiones de la propuesta.
5. **Capacidad diaria.** Ningún día supera `maxDailyMinutes`.
6. **Duración fija.** Cada sesión dura `preferredSessionMinutes`, o `maxDailyMinutes` si es menor (se avisa con `SESSION_DURATION_ADJUSTED`).
7. **Cantidad y cierre (RN-07).** La cantidad de sesiones crece con la dificultad y con la cantidad de temas. Desde 3 sesiones, las últimas son de `simulacro` o `repaso`.

Lo que **no** hace la capacidad:

- **No guarda ni reserva nada.** Es una propuesta. Si su sistema la acepta, la guarda su sistema.
- **No conoce el resto del calendario de la persona.** Cada pedido es independiente. Si la persona ya tiene otras actividades, envíen sólo las franjas libres o descarten las sesiones que choquen.

### Avisos (`warnings[].code`)

| Código | Significado | Qué hacer |
|---|---|---|
| `AI_FALLBACK` | La IA no respondió a tiempo, o su propuesta no cumplía las garantías, y el plan lo armó el motor de reglas. | Nada obligatorio: el plan es válido. Pueden mostrarle a la persona que el plan es "estándar". |
| `SHORT_WINDOW` | El examen está más cerca que la anticipación recomendada. | Informativo. |
| `REDUCED_SESSIONS` | No entran todas las sesiones recomendadas en la disponibilidad enviada. | Sugerirle a la persona que agregue franjas o aumente los minutos por día. |
| `SESSION_DURATION_ADJUSTED` | `preferredSessionMinutes` superaba `maxDailyMinutes` y se ajustó. | Informativo. |

La lista de avisos es **abierta**: pueden aparecer códigos nuevos en versiones menores. Un código desconocido se trata como informativo.

## 6. Idempotencia

`POST /v1/study-plan-proposals` exige el encabezado **`Idempotency-Key`**: un identificador único por pedido, generado por el consumidor (recomendamos un UUID v4; entre 8 y 128 caracteres con letras, números, `-` o `_`).

| Situación | Resultado |
|---|---|
| Primer pedido con una clave | Se genera la propuesta y se guarda la respuesta durante **24 h**. |
| Misma clave y **mismo cuerpo** (reintento) | Se devuelve **la misma respuesta** (mismo `proposalId`, mismo código HTTP) con `Idempotent-Replayed: true`. No se vuelve a llamar a la IA ni se consume cupo. |
| Misma clave y **otro cuerpo** | `422 IDEMPOTENCY_KEY_REUSED`. Un pedido distinto necesita una clave nueva. |
| Misma clave mientras el primer pedido **todavía se procesa** | `409 IDEMPOTENCY_REQUEST_IN_PROGRESS`. Reintentar con la misma clave después de `Retry-After`. |
| Pasaron más de 24 h | La clave se olvida y el pedido se procesa como nuevo. |

Las claves son por API key: dos grupos pueden usar la misma clave sin interferir. Sólo se guardan las respuestas `200` y `422`. Los errores `5xx` no se guardan, así un reintento puede tener éxito.

**Por qué importa:** el agente de IA no es determinista. Sin la clave, un reintento después de un timeout podría devolver un plan distinto del que la persona ya vio.

## 7. Tiempos de espera, reintentos y cupo de uso

**Nuestros tiempos.** planner-service responde en menos de **10 s** en el peor caso. Si la IA tarda más de lo previsto, la propuesta se arma con el motor de reglas y se responde igual. Los valores internos están en el [ADR-005](../adr/ADR-005-comunicacion.md).

**Lo que recomendamos del lado del consumidor:**

- **Timeout de cliente: 12 s.**
- **Reintentar sólo** ante errores de red, timeouts, `409`, `429`, `500`, `502`, `503` y `504`, siempre con **la misma `Idempotency-Key`**.
- **Espera exponencial con variación aleatoria:** 1 s, 2 s y 4 s (±20 %), con un máximo de 3 reintentos. Si la respuesta trae `Retry-After`, respetarlo.
- **No reintentar** `400`, `401` ni `422` sin antes corregir el pedido.
- Si se agotan los reintentos, conviene que su sistema tenga una alternativa: por ejemplo, avisarle a la persona que el plan no está disponible por el momento, o armar uno propio simple.

**Cupo de uso.** **30 pedidos por minuto por API key** (los reintentos idempotentes no cuentan). Cada respuesta informa el estado del cupo:

| Encabezado | Significado |
|---|---|
| `RateLimit-Limit` | Pedidos permitidos por ventana de 60 s |
| `RateLimit-Remaining` | Pedidos que quedan en la ventana actual |
| `RateLimit-Reset` | Segundos que faltan para que se renueve la ventana |

Si lo superan, la respuesta es `429 RATE_LIMITED` con `Retry-After`. Si necesitan más cupo, pídannoslo.

**Correlación.** Si envían `X-Correlation-Id`, lo usamos en nuestros logs y lo devolvemos en la respuesta. Si no, generamos uno. Ante cualquier problema, pásennos ese valor (también viene en el cuerpo de los errores como `correlationId`).

## 8. Errores

Los errores usan el formato **Problem Details** ([RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)) con `Content-Type: application/problem+json`. El campo que hay que usar para decidir qué hacer es **`code`**. Los textos `title` y `detail` son para personas y pueden cambiar.

```json
{
  "type": "https://github.com/MichiSil/ProyectoEstudiario/blob/main/docs/contracts/README.md#overlapping_slots",
  "title": "Franjas superpuestas",
  "status": 422,
  "detail": "Las franjas del lunes 18:00-21:00 y 20:00-22:00 se superponen.",
  "code": "OVERLAPPING_SLOTS",
  "correlationId": "grupo-x-7d2c41",
  "errors": [
    { "field": "availability.weeklySlots[1]", "message": "se superpone con availability.weeklySlots[0]" }
  ]
}
```

`errors[]` aparece sólo en los errores de validación e indica qué campo falló.

| HTTP | `code` | Significado | ¿Reintentar? |
|---|---|---|---|
| 400 | <a id="malformed_request"></a>`MALFORMED_REQUEST` | El cuerpo no es JSON válido o el `Content-Type` no es `application/json`. | No. Corregir el pedido. |
| 400 | <a id="idempotency_key_missing"></a>`IDEMPOTENCY_KEY_MISSING` | Falta el encabezado `Idempotency-Key`. | No. Agregarlo. |
| 400 | <a id="idempotency_key_invalid"></a>`IDEMPOTENCY_KEY_INVALID` | La `Idempotency-Key` no cumple el formato. | No. Corregirla. |
| 401 | <a id="unauthenticated"></a>`UNAUTHENTICATED` | Falta la API key, no es válida o fue revocada. | No. Revisar la clave o pedirnos una nueva. |
| 409 | <a id="idempotency_request_in_progress"></a>`IDEMPOTENCY_REQUEST_IN_PROGRESS` | Hay otro pedido con la misma clave en proceso. | Sí, con la **misma** clave, después de `Retry-After`. |
| 422 | <a id="validation_error"></a>`VALIDATION_ERROR` | Faltan campos, sobran campos o algún valor está fuera de rango o formato. El detalle está en `errors[]`. | No. Corregir los datos. |
| 422 | <a id="exam_date_in_past"></a>`EXAM_DATE_IN_PAST` | La fecha del examen ya pasó (RN-04). | No. |
| 422 | <a id="invalid_slot"></a>`INVALID_SLOT` | Una franja empieza a la misma hora o después de la que termina (RN-05). | No. |
| 422 | <a id="overlapping_slots"></a>`OVERLAPPING_SLOTS` | Dos franjas del mismo día se superponen (RN-05). | No. |
| 422 | <a id="invalid_timezone"></a>`INVALID_TIMEZONE` | `timezone` no es una zona horaria IANA conocida. | No. |
| 422 | <a id="invalid_start_date"></a>`INVALID_START_DATE` | `startDate` está en el pasado o no es anterior a la fecha del examen. | No. |
| 422 | <a id="no_available_time"></a>`NO_AVAILABLE_TIME` | Ninguna franja de la ventana de estudio alcanza para una sesión completa. | No. Mostrarle a la persona que agregue franjas, acorte la sesión o adelante `startDate`. |
| 422 | <a id="idempotency_key_reused"></a>`IDEMPOTENCY_KEY_REUSED` | La clave ya se usó con un cuerpo distinto. | No. Usar una clave nueva. |
| 429 | <a id="rate_limited"></a>`RATE_LIMITED` | Se superó el cupo de 30 pedidos por minuto. | Sí, después de `Retry-After`. |
| 500 | <a id="internal_error"></a>`INTERNAL_ERROR` | Error inesperado de nuestro lado. | Sí, con espera exponencial y la misma clave. Si persiste, avisarnos con el `correlationId`. |
| 503 | <a id="service_unavailable"></a>`SERVICE_UNAVAILABLE` | El servicio no puede atender temporalmente (por ejemplo, su base de datos no responde). | Sí, después de `Retry-After` y con la misma clave. |

Notas para interpretar los errores:

- **Una IA caída no es un error.** Si el agente de IA falla, la respuesta es `200` con `generatedBy: reglas` y el aviso `AI_FALLBACK`. Nunca devolvemos `503` por eso.
- **`400` contra `422`.** `400` significa que no pudimos leer el pedido. `422` significa que lo leímos pero sus datos no son aceptables.
- Un `502` o `504` no lo genera planner-service sino algún intermediario de red (por ejemplo, el proveedor de la nube). Trátenlo igual que un `503`.

## 9. Ejemplos con curl

Los ejemplos usan el mock (`http://localhost:4010`). Para el entorno real, cambien la URL base y usen su API key. Los cuerpos de ejemplo están en [`examples/`](examples/). Los comandos se ejecutan desde la raíz del repositorio en bash (Linux, macOS o Git Bash en Windows).

**Generar un plan:**

```bash
curl -i -X POST http://localhost:4010/v1/study-plan-proposals \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $ESTUDIARIO_API_KEY" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H "X-Correlation-Id: grupo-x-prueba-1" \
  -d @docs/contracts/examples/generar-plan.json
```

> Si no tienen `uuidgen`, sirve cualquier identificador único, por ejemplo `-H "Idempotency-Key: prueba-$(date +%s)"`.

**Reintentar el mismo pedido** (misma clave y mismo cuerpo, devuelve la misma propuesta):

```bash
KEY=3f1e9c2a-8b7d-4e6f-9a01-5c2d7e8f9b10
curl -s -X POST http://localhost:4010/v1/study-plan-proposals \
  -H "Content-Type: application/json" -H "X-API-Key: $ESTUDIARIO_API_KEY" \
  -H "Idempotency-Key: $KEY" -d @docs/contracts/examples/generar-plan.json
```

**Pedido sin API key** (`401 UNAUTHENTICATED`):

```bash
curl -i -X POST http://localhost:4010/v1/study-plan-proposals \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d @docs/contracts/examples/generar-plan.json
```

**Pedido con datos inválidos** (`422 VALIDATION_ERROR`):

```bash
curl -i -X POST http://localhost:4010/v1/study-plan-proposals \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $ESTUDIARIO_API_KEY" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d @docs/contracts/examples/generar-plan-invalido.json
```

**Estado del servicio:**

```bash
curl -i http://localhost:4010/health
```

## 10. Mock para desarrollar y probar

El mock es [Prism](https://docs.stoplight.io/docs/prism), que levanta un servidor a partir de `planner-v1.yaml`: valida cada pedido contra el contrato y responde con los ejemplos definidos en él. Sirve para que el grupo consumidor desarrolle y pruebe su integración antes de que esté nuestra implementación real.

### Cómo levantarlo

Cualquiera de estas opciones lo deja escuchando en `http://localhost:4010`.

**Con el sistema completo** (cuando esté el `docker-compose.yml`, ver el [README](../../README.md#cómo-levantar-el-sistema)):

```bash
docker compose up planner-mock
```

**Sólo el mock, con Docker:**

```bash
docker run --rm -p 4010:4010 -v "$(pwd)/docs/contracts:/contracts" stoplight/prism:5 mock -h 0.0.0.0 /contracts/planner-v1.yaml
```

> En Git Bash para Windows, anteponer `MSYS_NO_PATHCONV=1` al comando para que no se reescriba la ruta `/contracts`.

**Sólo el mock, con Node.js 18 o superior:**

```bash
npx @stoplight/prism-cli@5 mock -p 4010 docs/contracts/planner-v1.yaml
```

### Cómo elegir la respuesta

Por defecto, el mock devuelve el primer ejemplo de la respuesta exitosa (`generadoPorIA`). Con el encabezado `Prefer` se puede forzar otro código o ejemplo, lo que sirve para probar cómo reacciona su sistema ante cada caso:

| Para probar… | Agregar el encabezado |
|---|---|
| Un plan armado por reglas, con avisos | `Prefer: example=generadoPorReglas` |
| Un examen en el pasado | `Prefer: code=422, example=examenEnElPasado` |
| Que no haya tiempo disponible | `Prefer: code=422, example=sinTiempoDisponible` |
| Una clave de idempotencia reutilizada | `Prefer: code=422, example=claveReutilizada` |
| Un pedido en curso | `Prefer: code=409` |
| Que se supere el cupo | `Prefer: code=429` |
| Un error interno | `Prefer: code=500` |
| Que el servicio no esté disponible | `Prefer: code=503` |

Por ejemplo:

```bash
curl -i -X POST http://localhost:4010/v1/study-plan-proposals \
  -H "Content-Type: application/json" -H "X-API-Key: prueba" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H "Prefer: code=503" \
  -d @docs/contracts/examples/generar-plan.json
```

### Limitaciones del mock

- **No planifica.** Devuelve siempre los ejemplos del contrato, sin importar las fechas o los temas enviados.
- **No aplica reglas de negocio** (examen en el pasado, franjas superpuestas, idempotencia, cupo). Esos casos se prueban forzándolos con `Prefer`.
- **Valida el esquema, pero con sus propios códigos.** Si el pedido no cumple el contrato, responde `422` con el cuerpo del ejemplo `validacion`, y el detalle real de qué falló viene en el encabezado `sl-violations`. Si falta la `Idempotency-Key`, el mock devuelve `422`, mientras que el servicio real devuelve `400 IDEMPOTENCY_KEY_MISSING`.
- **Acepta cualquier API key.** Sólo controla que el encabezado `X-API-Key` esté presente. Si falta, responde `401`.
- No agrega los encabezados `RateLimit-*`, `Idempotent-Replayed` ni `X-Correlation-Id`.

## 11. Versionado y compatibilidad

**Esquema de versiones.** El contrato usa versionado semántico (`info.version` en el YAML). La **versión mayor** va en la ruta (`/v1`) y en el nombre del archivo (`planner-v1.yaml`).

| Tipo de cambio | Ejemplos | Versión | ¿Afecta al consumidor? |
|---|---|---|---|
| **Compatible** | Agregar un campo **opcional** al pedido; agregar campos a la respuesta; agregar códigos de aviso o de error nuevos; agregar valores a `sessions[].kind`; agregar operaciones nuevas | Menor (`1.0.0` → `1.1.0`), misma ruta `/v1` | No, si el consumidor es un **lector tolerante** |
| **Corrección** | Aclarar descripciones o ejemplos sin cambiar el comportamiento | Parche (`1.0.0` → `1.0.1`) | No |
| **Incompatible** | Quitar o renombrar un campo; volver obligatorio un campo opcional; cambiar un tipo o un formato; cambiar el significado de un código; endurecer una validación | Mayor: nueva ruta `/v2` y archivo `planner-v2.yaml` | Sí |

**Lector tolerante.** Para que los cambios compatibles no rompan su integración:

- Ignoren los campos de la respuesta que no conozcan.
- Acepten valores desconocidos en `warnings[].code` y `sessions[].kind`.
- Decidan según `status` y `code`, nunca según `title`, `detail` o `message`.

**Cómo se publica una versión nueva.**

1. El cambio se hace en una rama propia con *pull request* revisado, igual que el resto del código. Un cambio incompatible requiere además un ADR que reemplace al [ADR-008](../adr/ADR-008-contrato-propio.md).
2. Se actualizan el YAML, este README y el [historial de cambios](#13-historial-de-cambios).
3. Se avisa al grupo consumidor **antes** de desplegar el cambio.

**Ciclo de vida de una versión mayor.**

- Cuando sale `/v2`, `/v1` sigue funcionando **al menos 4 semanas** y, en cualquier caso, **hasta el final de la evaluación**.
- Mientras tanto, las respuestas de `/v1` incluyen los encabezados `Deprecation: true` y `Sunset: <fecha de baja>` (RFC 8594), además del aviso directo al grupo.
- Nunca se hace un cambio incompatible dentro de `/v1`.

## 12. Cómo verificar la integración contra el contrato

El enunciado pide que el grupo consumidor tenga un **test de contrato** que detecte cambios incompatibles. Sugerimos:

- **Validar las respuestas contra el esquema.** Cargar `planner-v1.yaml` en el test y validar cada respuesta (`200` y errores) contra el esquema correspondiente, por ejemplo con [kin-openapi](https://github.com/getkin/kin-openapi) en Go o con el validador de OpenAPI de su lenguaje.
- **Fijar la versión del contrato.** Copiar el YAML a su repositorio con la versión explícita y, en el test, compararlo con el publicado acá. Si cambia la versión mayor, el test falla.
- **Probar contra el servicio real a través de Prism en modo proxy**, que valida cada respuesta real contra el contrato y reporta las diferencias:

  ```bash
  npx @stoplight/prism-cli@5 proxy docs/contracts/planner-v1.yaml <URL base real> --errors
  ```

Nosotros validamos nuestra implementación contra este mismo YAML en los tests de planner-service, así que cualquier diferencia entre el contrato y el servicio la detectamos de nuestro lado antes de desplegar.

## 13. Historial de cambios

| Versión | Fecha | Cambios |
|---|---|---|
| 1.0.0 | 2026-10-07 | Primera versión: `POST /v1/study-plan-proposals` y `GET /health`. Autenticación por API key, `Idempotency-Key` obligatoria, errores en formato Problem Details y cupo de 30 pedidos por minuto. |
