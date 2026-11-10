# ADR-011: Observabilidad

- **Estado:** Aceptada (métricas, tablero y alertas implementados: ver [*Actualización (2026-11-09)*](#actualización-2026-11-09))
- **Fecha:** 2026-10-19
- **Decisión del enunciado:** D11
- **Reemplaza a:** —

## Contexto

El enunciado (2.10) pide que el sistema permita **comprender el estado de una operación sin inspeccionar cada componente a mano**, y que incluya como mínimo **logs estructurados, métricas y un tablero** que muestre su estado general. Para la Entrega 2 pide además **logs estructurados y correlacionados y una primera traza distribuida del flujo principal**; para la presentación final, **logs, métricas y trazas completos, junto con el tablero y la alerta** que se van a usar.

Otras decisiones ya dependen de la observabilidad:

- El [ADR-005](ADR-005-comunicacion.md) (2.5) define que el api-gateway genera o respeta un `X-Correlation-Id` y que cada servicio lo incluye en sus logs, y deja para este ADR la estrategia completa. También pide una métrica con alerta para la cantidad de mensajes en cada *dead letter queue*.
- La sección *Balanceo de carga* de [`ARCHITECTURE.md`](../ARCHITECTURE.md) pide que la distribución entre las instancias de forum-service y su estado se puedan comprobar en el tablero, con métricas separadas por instancia (se detalla en el ADR-012).
- El enunciado pide demostrar el impacto de la caché con métricas (D7) y registrar una caída provocada en `POSTMORTEM.md`, para lo que hacen falta datos de lo que pasó.

El flujo principal de esta entrega es **pedir una propuesta de plan y confirmarlo**: el pedido entra por el api-gateway y lo resuelve planner-service con PostgreSQL. Es el flujo que tiene que poder seguirse de punta a punta.

## Opciones consideradas

**Logs**

1. **Texto libre con `log`.** Es lo más simple, pero no se puede filtrar por campo ni correlacionar pedidos entre servicios.
2. **JSON con `log/slog` de la biblioteca estándar (elegida).** Viene con Go, no agrega dependencias, escribe una línea JSON por evento y permite agregar campos fijos (`service`) y campos del pedido (`correlationId`).
3. **Una librería externa (zap, zerolog).** Es más rápida en volúmenes altos, pero agrega dependencias sin que el volumen del sistema lo justifique.

**Trazas**

1. **Sin trazas, sólo correlation id en los logs.** Permite agrupar los logs de un pedido, pero no muestra cuánto tardó cada salto ni dónde se fue el tiempo.
2. **OpenTelemetry con exportador OTLP y Jaeger (elegida).** Es el estándar abierto: propaga el contexto con el encabezado W3C `traceparent`, instrumenta `net/http` con `otelhttp` y exporta por OTLP, así que se puede cambiar de herramienta sin tocar el código. Jaeger *all-in-one* recibe OTLP y trae la interfaz para ver las trazas en un solo contenedor.
3. **Zipkin.** Cumple la misma función, pero usarlo a través de OpenTelemetry no aporta nada sobre Jaeger, que recibe OTLP directamente.

**Métricas y tablero** *(propuesta: falta validarla en el grupo)*

1. **Prometheus + Grafana.** Cada servicio expone `GET /metrics`, Prometheus las recolecta cada pocos segundos y Grafana arma el tablero y las alertas. Es la combinación más usada, corre en `docker compose` y permite separar las métricas por instancia (necesario para el ADR-012).
2. **Métricas de OpenTelemetry enviadas a un OpenTelemetry Collector**, que las expone a Prometheus. Unifica trazas y métricas en el mismo SDK, pero suma un contenedor más para configurar.
3. **Sólo las métricas que Jaeger calcula a partir de las trazas** (*Service Performance Monitoring*). No necesita instrumentar métricas, pero sólo da tasa, errores y latencia por operación: no sirve para métricas de negocio, de la caché ni de las colas.

## Decisión

### 1. Logs

- Todos los servicios escriben a la salida estándar **una línea JSON por evento** con `log/slog`. Docker los junta y se leen con `docker compose logs`.
- **Campos comunes de cada pedido HTTP:** `time`, `level`, `msg` (`"request"`), `service`, `correlationId`, `method`, `path`, `status` y `durationMs`. planner-service agrega además `instance` (el contenedor que atendió), `traceId` y `spanId`.
- **Nivel según el resultado:** `INFO` para respuestas `2xx` y `3xx`, `WARN` para `4xx` y `ERROR` para `5xx`. Las fallas al llamar a otro componente se loguean como `ERROR` con el motivo en `error`.
- `GET /health` no se loguea, porque lo consultan los healthchecks cada pocos segundos y taparía el resto.
- **Nunca se loguean** contraseñas, tokens, API keys ni el contenido de los resúmenes.

### 2. Correlación

- El api-gateway toma el `X-Correlation-Id` del pedido o, si no viene, genera uno. Lo reenvía al servicio, lo devuelve en la respuesta y lo agrega a cada log del pedido (ADR-005, 2.5).
- El mismo identificador se guarda como atributo `correlation.id` en el span del pedido, así se puede buscar la traza en Jaeger a partir de un log, y viceversa.

### 3. Trazas

- Cada servicio configura OpenTelemetry con el propagador **W3C Trace Context** (`traceparent`) y exporta las trazas por **OTLP/HTTP** a la dirección de `OTEL_EXPORTER_OTLP_ENDPOINT`. En local es Jaeger (`http://jaeger:4318`); si la variable no está, el servicio funciona igual y no exporta.
- El servidor HTTP de cada servicio se envuelve con `otelhttp.NewHandler`: abre un span por pedido o continúa la traza que llega en `traceparent`. Las llamadas salientes usan `otelhttp.NewTransport`, que inyecta el `traceparent`.
- **Primera traza distribuida del flujo principal (Entrega 2):** `api-gateway` (span del pedido y de la llamada HTTP) → `planner-service` (span del pedido). Se ve en Jaeger, en http://localhost:16686.
- Se muestrea el 100 % de las trazas. El volumen del sistema lo permite y así no se pierde ninguna durante las pruebas y la caída controlada.

### 4. Métricas: qué se mide *(propuesta)*

| Grupo | Métrica | Para qué |
|---|---|---|
| Tráfico, errores y latencia de cada servicio | Pedidos por `service`, `instance`, ruta, método y `status`; histograma de duración | Disponibilidad y latencia del SLO; distribución entre instancias (ADR-012) |
| Gateway | Respuestas `503 UPSTREAM_UNAVAILABLE` y `504 UPSTREAM_TIMEOUT` por servicio de destino | Detectar un servicio caído o lento detrás del gateway |
| Plan de estudio | Propuestas generadas por `generatedBy` (`ia` / `reglas`); confirmaciones por resultado (confirmada, rechazada por regla, repetida por `Idempotency-Key`) | Ver la degradación de la IA (RN-09) y el comportamiento de la acción principal (RN-10 a RN-14) |
| Dependencias | Duración y errores de las llamadas al proveedor de IA y a PostgreSQL | Encontrar el límite de capacidad (D13) |
| Caché del foro (cuando exista) | Aciertos y fallos por operación | Medición antes y después que pide el enunciado (ADR-007) |
| Mensajería (cuando exista) | Mensajes en cada cola y en cada *dead letter queue* | Alerta del ADR-005 |

### 5. Objetivo de servicio (SLO) inicial

Se mide sobre los pedidos a `/api/planner/*` que pasan por el api-gateway, en ventanas de 7 días. Los valores salen de los umbrales que ya fijaron otros ADR:

| Objetivo | Valor inicial | De dónde sale |
|---|---|---|
| **Disponibilidad** | 99 % de los pedidos sin respuesta `5xx` (los `4xx` son errores del pedido y no cuentan) | Menos de 1 % de errores, umbral de la prueba de carga del [ADR-013](ADR-013-capacidad-y-costos.md) |
| **Latencia de proponer y confirmar** cuando el plan lo arma el motor de reglas | p95 por debajo de **500 ms** | Umbral de aceptación del [ADR-013](ADR-013-capacidad-y-costos.md) |
| **Latencia de la propuesta** cuando interviene la IA | p95 por debajo de **10 s** | Lo que se le compromete al consumidor en el [ADR-008](ADR-008-contrato-propio.md) |

Se revisan si cambian los umbrales del ADR-013 o el presupuesto de la IA del [ADR-010](ADR-010-resiliencia.md).

### 6. Criterios de alerta *(propuesta)*

| Alerta | Condición | Qué indica |
|---|---|---|
| Errores altos | Más del 5 % de respuestas `5xx` en 5 minutos | Se está consumiendo el margen del SLO de disponibilidad |
| Servicio no disponible | Algún `503 UPSTREAM_UNAVAILABLE` sostenido por 1 minuto, o una instancia que no responde su healthcheck | Un servicio o una instancia del foro está caído |
| Latencia | p95 de proponer o confirmar por encima de 500 ms (o de 10 s cuando interviene la IA) durante 10 minutos | Se incumple el SLO de latencia |
| IA degradada | Más de la mitad de las propuestas armadas por `reglas` en 15 minutos | El proveedor de IA está fallando (RN-09) |
| Mensajes no procesados | Algún mensaje en una *dead letter queue* | Pedido del ADR-005 |

### 7. Tablero *(propuesta)*

Un tablero de estado general con: pedidos por segundo, porcentaje de errores y latencia p95 por servicio; estado de cada instancia y reparto de pedidos entre ellas (ADR-012); propuestas por `generatedBy`; confirmaciones por resultado; y, cuando existan, aciertos de la caché y mensajes en las DLQ.

## Consecuencias

**Positivas**

- Un pedido se puede seguir de punta a punta: el `correlationId` agrupa sus logs en todos los servicios y la traza de Jaeger muestra cuánto tardó cada salto.
- Al usar OpenTelemetry y OTLP, cambiar Jaeger por otra herramienta (o pasar a una en la nube) no requiere cambiar el código.
- Los logs y las trazas ya cubren el flujo principal en la Entrega 2, con lo que se puede analizar la caída controlada del `POSTMORTEM.md`.

**Negativas / limitaciones aceptadas**

- Los logs sólo se ven con `docker compose logs`: no hay un almacenamiento central con búsqueda. Para el volumen del sistema alcanza, y el `correlationId` permite filtrarlos con `grep`.
- Jaeger *all-in-one* guarda las trazas en memoria: se pierden al reiniciar el contenedor.
- Muestrear el 100 % de las trazas suma trabajo a cada pedido. Si las pruebas de carga muestran que pesa, se baja el muestreo.
- Los logs del api-gateway todavía no incluyen el `traceId`: para ir de un log del gateway a su traza se busca por `correlation.id` en Jaeger.
- En esta entrega todavía no hay métricas, tablero ni alertas: ver abajo.

**Queda para la presentación final**

- Validar en el grupo la herramienta de métricas y tablero (opción 1 o 2), implementarla y exponer las métricas de la sección 4 en todos los servicios.
- Armar el tablero y configurar al menos la alerta que se va a mostrar en la presentación.
- Agregar las métricas por instancia de forum-service (ADR-012), las de la caché (ADR-007) y las de las colas (ADR-005) a medida que esos componentes se implementen.
- Instrumentar el resto de los servicios con el mismo paquete de observabilidad, y sumar spans de las consultas a PostgreSQL y de la llamada a la IA en planner-service.
- Revisar el SLO y los umbrales de alerta con nuevas pruebas de carga (ADR-013), en especial si se suman instancias o se cambia el tamaño de la base.

## Actualización (2026-11-09)

Se implementaron las métricas, el tablero y las alertas con la **opción 1: Prometheus + Grafana**. Los logs y las trazas siguen como estaban.

### Cómo se exponen las métricas

- Cada servicio expone `GET /metrics` con `prometheus/client_golang`, en un **servidor aparte, en el puerto 9100** (`METRICS_ADDR`). Ese puerto no se publica en `docker compose` ni en la nube, y como no es el puerto de la API, `/metrics` **no se puede pedir a través del api-gateway** (`/api/forum/metrics` llega a la API del foro, que no tiene esa ruta) **ni de Caddy**.
- **Prometheus** (`prom/prometheus:v2.55.1`, http://localhost:9090) lee cada servicio cada 5 s. Las dos instancias de forum-service se leen **por separado**, con `instance` = `forum-service-1` y `forum-service-2` ([ADR-012](ADR-012-balanceo-de-carga.md)). Configuración en [`deploy/prometheus/prometheus.yml`](../../deploy/prometheus/prometheus.yml).
- **Grafana** (`grafana/grafana:11.3.0`, http://localhost:3000) arranca con el datasource y el tablero **provisionados desde archivos** de [`deploy/grafana`](../../deploy/grafana): no se configura nada a mano. El usuario y la contraseña salen de `GRAFANA_ADMIN_USER` y `GRAFANA_ADMIN_PASSWORD` del `.env`, y el tablero no se puede editar desde la interfaz: se cambia en el repositorio.

### Qué se mide

| Servicio | Métrica | Etiquetas | Para qué |
|---|---|---|---|
| Los cinco con API HTTP | `http_requests_total` y el histograma `http_request_duration_seconds` | `route` (el **patrón registrado**, por ejemplo `/summaries/{id}`, no la URL con ids), `method`, `status` | Tráfico, errores y latencia p95 del SLO. Usar el patrón evita una serie por cada id. `GET /health` no se cuenta: lo consultan los healthchecks cada pocos segundos. |
| api-gateway | `gateway_upstream_responses_total` | `upstream`, `upstream_instance` (el `X-Served-By`) | Reparto de los pedidos entre las instancias del foro |
| api-gateway | `gateway_upstream_instance_up` | `upstream`, `upstream_instance` | 1 si la instancia está en la rotación del balanceador, 0 si está fuera. El nombre sale del `X-Instance` de su `/health`. |
| forum-service | `forum_cache_requests_total` y `forum_cache_invalidations_total` (ya existían, [ADR-007](ADR-007-cache.md)) | `operation`, `result` / `write`, `result` | Porcentaje de aciertos de la caché |
| forum-service | `forum_search_index_lag_seconds` y `forum_search_index_last_sync_timestamp_seconds` | — | Retraso del indexador de Solr (el `lagMs` del log) y hace cuánto sincronizó bien por última vez (RN-26) |
| forum-service | `forum_outbox_pending_events` | — | Eventos que quedaron sin publicar en RabbitMQ en la última vuelta del relay (como máximo 100 por vuelta) |
| notification-service | `notification_messages_total` | `result`: `procesado`, `reintento`, `dlq`, `reencolado` | Mensajes procesados, reintentos y DLQ ([ADR-005](ADR-005-comunicacion.md)) |
| users-service | `users_club_requests_total` | `result`: `ok`, `indisponible`, `no_vinculada`, `rechazado`, `mal_configurado` | Llamadas al Club de Beneficios por resultado ([ADR-015](ADR-015-club-de-beneficios.md)) |
| users-service | `users_club_breaker_state` | — | Circuit breaker del Club: 0 cerrado, 1 semiabierto, 2 abierto |
| users-service | `users_club_pending_points` | — | Puntos guardados y todavía sin acreditar (RN-43), leídos de la base en cada lectura de Prometheus |
| planner-service | `planner_proposals_total` | `generated_by`: `ia`, `reglas` | Propuestas por origen: la degradación de la IA (RN-09) |
| planner-service | `planner_confirmations_total` | `result`: `confirmada`, `rechazada`, `error` | Confirmaciones de la acción principal |

En planner-service las métricas de negocio entran por el puerto `app.Metrics` y las implementa el adaptador `internal/adapters/metrics`, así el núcleo hexagonal no depende de Prometheus ([ADR-002](ADR-002-arquitectura-interna.md)). Los contadores con etiquetas fijas arrancan en 0 con todas sus etiquetas: si no, `increase()` no ve el primer evento de una serie nueva (por ejemplo, el primer mensaje que va a la DLQ).

Quedó afuera de la sección 4 original: la duración de las llamadas a la IA y a PostgreSQL como métricas propias (se ven en la latencia de cada ruta y en las trazas) y la cantidad de mensajes en cada cola (se cuentan los que pasan por la DLQ, no los que esperan; RabbitMQ los muestra en su panel).

### Tablero: "Estudiario: estado general"

Es la página de inicio de Grafana ([`estudiario.json`](../../deploy/grafana/dashboards/estudiario.json)) y se actualiza cada 5 s:

1. **Servicios:** tráfico, porcentaje de `5xx` y latencia p95 de cada servicio, con las líneas del SLO (5 % y 500 ms).
2. **Foro:** pedidos por instancia (apilados, así se ve el reparto y el total), estado de cada instancia en la rotación, instancias sanas, porcentaje de aciertos de la caché y lecturas de la caché por resultado, eventos pendientes en el outbox, retraso del índice y última sincronización con Solr.
3. **Mensajería y Club de Beneficios:** mensajes por resultado y DLQ de la última hora; llamadas al Club por resultado, estado del circuit breaker y puntos pendientes.
4. **Plan de estudio y alertas:** propuestas por origen, confirmaciones por resultado y la tabla de alertas disparadas.

### SLO

Queda el de la sección 5, ahora medido: **99 % de los pedidos sin `5xx`** (con `http_requests_total`, por servicio) y **p95 de confirmar por debajo de 500 ms** (con el histograma de `/plans/{planId}/confirm`). El de la propuesta con IA (10 s) se ve en el tablero, pero no tiene alerta propia: la latencia de la ruta mezcla las propuestas de la IA y las de reglas.

### Alertas

Son reglas de Prometheus en [`deploy/prometheus/alerts.yml`](../../deploy/prometheus/alerts.yml), provisionadas desde el archivo. Se ven en Prometheus (*Alerts*), en Grafana (*Alerting > Alert rules*, como reglas del datasource) y en la tabla del tablero:

| Alerta | Condición | Se alinea con |
|---|---|---|
| `ErroresAltos` | `5xx` / total de un servicio > 5 % durante 5 min | SLO de disponibilidad (sección 6) |
| `InstanciaDelForoFueraDeRotacion` | `gateway_upstream_instance_up == 0` durante 1 min | Servicio no disponible (sección 6), [ADR-012](ADR-012-balanceo-de-carga.md) |
| `ServicioSinResponder` | `up == 0` durante 1 min | Servicio no disponible |
| `ConfirmarLento` | p95 de confirmar > 500 ms durante 10 min | SLO de latencia |
| `MensajesEnLaDLQ` | algún mensaje a `notifications.dlq` en 10 min | Mensajes no procesados ([ADR-005](ADR-005-comunicacion.md)) |
| `ClubDeBeneficiosNoDisponible` | circuit breaker abierto durante 1 min | [ADR-015](ADR-015-club-de-beneficios.md) |

No se configuró un canal de aviso (Alertmanager, mail o chat): las alertas se ven en las tres pantallas. Sumar uno no cambia las reglas.

### Evidencia (2026-11-09, `docker compose` local)

Con tráfico constante contra el gateway (unos 30 pedidos/s, de los cuales unos 25 al foro), se apagó la segunda instancia del foro y se la volvió a levantar. Horas de Argentina (UTC−3):

| Hora | Qué pasó |
|---|---|
| Antes | Reparto parejo: 7,56 y 7,54 pedidos/s por instancia. Caché con 88,7 % de aciertos. |
| 20:22:20 | `docker compose stop forum-service-2` |
| 20:22:21 | El gateway la saca de la rotación al fallar un pedido real (chequeo pasivo) y reintenta la lectura en `forum-service-1`. El tablero la muestra *fuera de la rotación* y todo el tráfico en `forum-service-1`. |
| 20:22:24 | `InstanciaDelForoFueraDeRotacion` y `ServicioSinResponder` pasan a *pending* |
| 20:23:26 | Las dos alertas se **disparan** (1 minuto después) |
| 20:24:26 | `docker compose start forum-service-2` |
| 20:24:31 | El gateway la vuelve a poner en la rotación; el reparto vuelve a ser parejo |
| 20:24:36 | Las alertas se resuelven solas |

En los 15 minutos de la prueba, incluida la caída, el gateway atendió **8.984 pedidos del foro sin ningún `5xx`**: las lecturas que fallaron en la instancia caída se reintentaron en la otra, como define el ADR-012.

También se vio en las métricas el comportamiento esperado de otros ADR: al aprobar resúmenes, `notification_messages_total` muestra `reintento` además de `procesado`, porque **las dos instancias del relay publican el mismo evento** (entrega al menos una vez, ADR-012) y notification-service manda el mail una sola vez y descarta la copia (RN-32). Y como el `.env` local no tiene `CLUB_API_KEY`, las llamadas al Club salen como `mal_configurado` y los puntos quedan pendientes; el pequeño pico de `5xx` de users-service del tablero es ese pedido de vinculación (`503 CLUB_UNAVAILABLE`).

Capturas en [`docs/observabilidad/`](../observabilidad/):

- [Tablero con la caída y la recuperación](../observabilidad/01-tablero-caida-y-recuperacion.jpg) y [mensajería, Club y plan](../observabilidad/02-tablero-mensajeria-club-y-plan.jpg)
- [Tablero durante la caída](../observabilidad/03-tablero-durante-la-caida.jpg) y [alertas disparadas en el tablero](../observabilidad/04-tablero-alertas-disparadas.jpg)
- [La alerta disparada en Prometheus](../observabilidad/05-prometheus-alerta-disparada.jpg) y [las reglas en Grafana](../observabilidad/06-grafana-reglas-de-alerta.jpg)

### Consecuencias

- El estado del sistema se ve en un solo lugar, y la caída de una instancia se nota en segundos en el tablero y en un minuto como alerta, sin mirar los logs.
- Prometheus guarda 7 días de datos en un volumen de Docker. Para el volumen del sistema alcanza; no hay almacenamiento de largo plazo.
- `users_club_pending_points` hace una consulta a PostgreSQL en cada lectura de Prometheus (cada 5 s). Es una suma sobre un índice parcial y no pesa; si creciera, se calcula aparte.
- **En la nube:** Prometheus y Grafana publican los puertos 9090 y 3000 en el `docker-compose.yml`. Para que en la nube no queden expuestos hay que resetearlos en `deploy/azure/docker-compose.cloud.yml`, como el resto, y verlos por túnel SSH. Además, el `.env` de la nube necesita `GRAFANA_ADMIN_USER` y `GRAFANA_ADMIN_PASSWORD` (con valores propios): sin ellos `docker compose` no levanta. Se coordina con el despliegue.

## Actualización (2026-11-10): alertas del Club y de confirmar

La jornada de fallas ([`POSTMORTEM.md`](../POSTMORTEM.md), hallazgo 5) mostró dos alertas que no se comportaban como se esperaba. Se cambiaron sus condiciones en [`alerts.yml`](../../deploy/prometheus/alerts.yml):

| Alerta | Antes | Ahora | Por qué |
|---|---|---|---|
| `ClubDeBeneficiosNoDisponible` | `users_club_breaker_state == 2` durante 1 min | `max_over_time(users_club_breaker_state[1m]) == 2` durante 1 min | Cada 30 s el breaker pasa a semiabierto para probar el Club. Si Prometheus leía justo ese momento, la condición dejaba de cumplirse y el `for` volvía a empezar: tardó 3 min 45 s en dispararse. Con el máximo del último minuto, la prueba no la reinicia. |
| `ConfirmarLento` | p95 de confirmar > 500 ms durante 10 min | Lo mismo, **y** al menos 10 confirmaciones en los 10 min | Con muy pocas confirmaciones, una sola lenta (la que falló con la base caída) ya es el p95, y la alerta quedó en *pending* sin que el SLO estuviera en riesgo. |

**Validación.**

- **Club caído** (`CLUB_BASE_URL=http://club-caido.invalid`, entrando al perfil cada 8 s): el breaker se abrió a las 11:18:33, pasó por semiabierto a las 11:19:31 y la alerta se **disparó a las 11:19:39, 66 s después de abrirse**, sin reiniciarse. Al volver users-service a su configuración, se resolvió sola.
- **Tests de las reglas** con `promtool` ([`alerts_test.yml`](../../deploy/prometheus/alerts_test.yml)), con series simuladas: el breaker alternando entre abierto y semiabierto dispara la alerta a los 2 min 15 s; tres confirmaciones con una lenta no dejan `ConfirmarLento` ni en *pending*; veinte por cada 10 min, todas lentas, sí la disparan. Contra las reglas anteriores, los dos primeros casos fallan.

**Consecuencia.** Como mira el último minuto, la alerta del Club también tarda hasta 1 minuto más en resolverse después de que el breaker se cierra.
