# ADR-011: Observabilidad

- **Estado:** Propuesta (primera versión; se completa para la presentación final)
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
