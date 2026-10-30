# Postmortem: caída controlada de PostgreSQL

- **Fecha:** 30 de octubre de 2026, de 12:56:52 a 12:57:28 (hora de Córdoba, UTC−3)
- **Tipo:** caída **provocada** para verificar la resiliencia del flujo principal (enunciado, ADR-010)
- **Servicios afectados:** planner-service (y users-service, que usa el mismo PostgreSQL)
- **Duración del impacto:** 32 segundos
- **Formato:** sin culpables; el objetivo es aprender del comportamiento del sistema

## Resumen

Con un estudiante usando la app, se apagó PostgreSQL durante 30 segundos. Mientras la base no estuvo, planner-service respondió **503 `SERVICE_UNAVAILABLE`** en Problem Details a cada pedido, rápido y sin colgarse, y el frontend mostró un mensaje que explica qué pasa y que se puede reintentar. Cuando la base volvió, el servicio se recuperó **solo, en 3 segundos y sin reiniciar nada**. No hubo errores 500, no se perdieron ni se corrompieron datos y no hubo que intervenir.

## Cómo se hizo

Entorno: el sistema completo corriendo en una compu del grupo (api-gateway, users-service, planner-service y PostgreSQL 16, con la misma configuración que `docker compose`). El script de la prueba:

1. Registra un estudiante e inicia sesión (JWT real de users-service).
2. Durante 75 segundos pide `GET /api/planner/sessions` por el api-gateway cada segundo, con `X-Correlation-Id: caida-<n>` para encontrar cada pedido en los logs.
3. A los 15 segundos apaga PostgreSQL (`pg_ctl stop -m fast`), espera 30 segundos y lo vuelve a prender (`pg_ctl start`).

Se registró, por cada pedido, la hora, el status, el `code` de la respuesta y la duración vista por el cliente.

## Línea de tiempo

| Hora (UTC−3) | Qué pasó |
|---|---|
| 12:56:36 | Empieza la prueba: todos los pedidos responden 200 |
| 12:56:52 | Se apaga PostgreSQL |
| 12:56:56 | Primer `503 SERVICE_UNAVAILABLE` (pedido `caida-9`): el pool de conexiones detecta que la base no está |
| 12:56:56 a 12:57:26 | 16 pedidos seguidos con 503; planner-service loguea `dependencia no disponible` con el error de conexión y el `correlationId` de cada pedido |
| 12:57:25 | Se prende PostgreSQL |
| 12:57:28 | Primer 200 después de la caída (`caida-25`): el pool reconecta solo |
| 12:57:28 a 12:59:00 | Todos los pedidos vuelven a responder 200 |

## Impacto

| | Antes y después de la caída | Durante la caída |
|---|---|---|
| Pedidos | 59, todos `200` | 16, todos `503 SERVICE_UNAVAILABLE` |
| Duración vista por el cliente | 310 ms en promedio | 398 ms en promedio, 1,3 s como máximo |
| Errores 500 | 0 | 0 |

- **Lo que vio el estudiante:** el aviso "El servicio no está disponible en este momento. Probá de nuevo en unos segundos", con la explicación "La base de datos no respondió a tiempo. No se guardó nada: podés reintentar sin miedo". Sin códigos técnicos en pantalla.
- **Datos:** ninguna pérdida. Las sesiones reservadas antes de la caída estaban intactas después.
- **Durante la caída tampoco funcionaban** el registro ni el inicio de sesión, porque users-service usa el mismo servidor PostgreSQL (con su propia base). Quien ya tenía el token siguió viendo los avisos de arriba.

## Qué funcionó

- **Fallar rápido en lugar de colgarse** (ADR-010): cada pedido falló en milisegundos dentro de planner-service (4 ms en el log). El timeout de 3 s por operación no llegó a hacer falta, porque la conexión se rechazaba enseguida.
- **Error claro y uniforme** (ADR-005): 503 con `code: SERVICE_UNAVAILABLE`, `Retry-After: 5` y el `correlationId` en el cuerpo y en los logs, en lugar de un 500 genérico.
- **Recuperación automática:** el pool de pgx volvió a conectar en el primer pedido después de que la base volvió. No hizo falta reiniciar ningún servicio.
- **Diagnóstico con los logs:** con el `correlationId` se encontró cada pedido fallido y la causa exacta ("no se pudo conectar con la base") en los logs JSON, sin entrar a cada componente.

## Qué no funcionó o faltó

- **Nadie se enteró solo:** no hubo alerta. La caída se vio porque se estaba mirando. Hace falta una alerta por tasa de 503 (ADR-011).
- **Login caído junto con el plan:** users-service y planner-service comparten el servidor PostgreSQL (con bases y usuarios separados, ADR-003). Una caída del servidor afecta a los dos.
- **La confirmación del plan no se probó durante la caída.** Está protegida por la clave de idempotencia (RN-14): si falla, el frontend reintenta con la misma clave sin duplicar sesiones. Conviene incluirla en la próxima prueba.

## Acciones

| Acción | Responsable | Estado |
|---|---|---|
| Alerta en el tablero cuando los 503 de un servicio superan el umbral del SLO (ADR-011) | Grupo (métricas y tablero) | Pendiente |
| Repetir la prueba con `docker compose` en la nube y con una confirmación de plan en el medio | Grupo | Pendiente |
| Documentar en el README cómo provocar la caída (`docker compose stop postgres`) para la presentación | Grupo | Pendiente |

## Cómo repetirla con docker compose

```bash
docker compose up -d --build
# en otra terminal, con un token (ver "Cuentas e identidad" en el README):
while true; do curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/planner/sessions; sleep 1; done
docker compose stop postgres     # 503 SERVICE_UNAVAILABLE
docker compose start postgres    # vuelve a 200 solo, en segundos
docker compose logs planner-service | grep "dependencia no disponible"
```
