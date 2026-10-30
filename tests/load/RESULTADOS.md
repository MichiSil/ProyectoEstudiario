# Resultados de las pruebas de carga

Scripts (k6): [`plan-flow.js`](plan-flow.js) para planner-service y [`forum-read.js`](forum-read.js) para la caché del foro. Análisis y decisiones: [ADR-013](../../docs/adr/ADR-013-capacidad-y-costos.md) y [ADR-007](../../docs/adr/ADR-007-cache.md).

## 2026-10-19: flujo principal de planner-service

### Entorno

| Componente | Recursos |
|---|---|
| planner-service | Contenedor Docker limitado a **1 vCPU y 256 MB** |
| PostgreSQL 16 | Contenedor Docker limitado a **1 vCPU y 512 MB** |
| Generador de carga | k6 en otro contenedor de la misma máquina (Apple M5, 10 núcleos) |
| Generador de planes | Motor de reglas (sin IA) |

Los límites de CPU y memoria simulan una instancia chica de la nube. Al correr todo en la misma máquina no hay latencia de red real: los tiempos medidos son los del servicio y la base.

### Flujo medido

Cada iteración es un estudiante nuevo que hace 6 pedidos: crear materia, cargar disponibilidad, crear examen, **pedir propuesta**, **confirmar plan** y **reintentar la confirmación** con la misma `Idempotency-Key`.

### Prueba 1: rampa hasta 100 usuarios simultáneos

0 → 20 usuarios en 30 s, → 50 en 1 min, → 100 en 1 min, → 0 en 20 s.

| Métrica | Valor |
|---|---|
| Flujos completos | **46.387** (272,9 por segundo) |
| Pedidos HTTP | 278.322 (1.637 por segundo) |
| Errores | **0,00 %** |
| Chequeos correctos | 100 % (324.709 de 324.709) |
| Confirmar plan: mediana / p95 / p99 | 26,5 ms / **99,8 ms** / 109,6 ms |
| Pedir propuesta: mediana / p95 / p99 | 19,6 ms / **101,0 ms** / 111,5 ms |
| Flujo completo (6 pedidos): p95 | 385 ms |

**Consistencia después de la prueba** (consulta directa a PostgreSQL):

| Planes confirmados | Confirmaciones registradas | Sesiones | Exámenes con más de un plan confirmado |
|---|---|---|---|
| 46.387 | 46.387 | 371.096 (8 por plan) | **0** |

Los 46.387 reintentos devolvieron el mismo resultado sin duplicar sesiones (RN-14).

### Prueba 2: rampa hasta 200 usuarios simultáneos

Mismo escenario, con el último escalón en 200 usuarios.

| Métrica | Valor |
|---|---|
| Flujos completos | 43.435 (255,7 por segundo) |
| Errores | **0,00 %** |
| Confirmar plan: mediana / p95 / p99 | 76,5 ms / **194,9 ms** / 229,7 ms |
| Pedir propuesta: mediana / p95 / p99 | 70,8 ms / **205,6 ms** / 279,4 ms |

Uso de recursos durante la prueba (`docker stats`, cada 10 s):

| Contenedor | CPU | Memoria |
|---|---|---|
| PostgreSQL | **101 % a 106 %** (saturado durante toda la prueba) | 209 a 249 MB de 512 MB |
| planner-service | 43 % a 56 % | 15 a 34 MB de 256 MB |

### Lectura

- Al pasar de 100 a 200 usuarios, **el throughput no creció** (de 273 a 256 flujos por segundo) y **la latencia se duplicó** (p95 de confirmar de 100 a 195 ms). Es la señal de saturación.
- El recurso saturado es la **CPU de PostgreSQL**. planner-service usó la mitad de su CPU y menos del 15 % de su memoria.
- Aun saturado, el sistema no perdió ni duplicó datos y no devolvió errores: los pedidos esperan su turno en lugar de fallar.

## 2026-10-30: lectura del foro, sin caché y con Memcached (ADR-007)

### Entorno

| Componente | Recursos |
|---|---|
| Máquina | Notebook con Windows 11, Intel Core i7-1355U (10 núcleos, 12 hilos), 16 GB de RAM |
| Docker Desktop | 12 CPU y 7,6 GB de RAM para todos los contenedores, **sin límites por contenedor** |
| Sistema | El `docker compose` completo (una instancia de forum-service, MongoDB, Solr, Memcached y el resto de los servicios) |
| Generador de carga | k6 0.54 en otro contenedor de la misma máquina, conectado directo a forum-service por la red de Docker |

Todo corre en la misma notebook, así que k6, forum-service, MongoDB y Solr compiten por la CPU. Los números sirven para **comparar** las dos corridas, no como capacidad absoluta.

### Escenario

[`forum-read.js`](forum-read.js): 40 resúmenes aprobados en 4 materias (los mismos en las dos corridas). Rampa de 0 a **50 usuarios virtuales** en 20 s, 1 min 30 s con 50 y bajada en 10 s. Cada iteración elige:

| Pedido | Proporción |
|---|---|
| Listado de aprobados (página 1 el 80 % de las veces, la 2 el resto; con filtro por materia la mitad de las veces) | 55 % |
| Detalle de un resumen (el 80 % de las veces, uno de los 10 más vistos) | 42 % |
| Calificar un resumen | 2,9 % |
| Publicar y aprobar un resumen nuevo | 0,1 % |

Se corrió dos veces seguidas con los mismos datos y la misma carga, cambiando sólo `FORUM_CACHE_PROVIDER` en el `.env` (`none` y `memcached`) y recreando forum-service. Memcached se reinició antes de cada corrida, así la segunda arrancó con la caché vacía. Las lecturas de MongoDB son la diferencia de `opcounters.query + getmore` antes y después de cada corrida. Los aciertos salen del resumen que forum-service escribe en los logs cada minuto (`"caché del foro"`).

### Resultados

| Métrica | Sin caché (`none`) | Con Memcached | Cambio |
|---|---|---|---|
| Pedidos por segundo | 171,5 | **604,6** | **× 3,5** |
| Pedidos totales en la corrida | 23.165 | 81.620 | |
| Errores | 0,00 % | 0,00 % | |
| Listado: p50 / p95 | 256,4 ms / 761,9 ms | **49,5 ms / 235,6 ms** | p95 − 69 % |
| Detalle: p50 / p95 | 62,3 ms / 357,1 ms | **26,3 ms / 127,9 ms** | p95 − 64 % |
| Aciertos del listado | — | **91,6 %** (41.006 de 44.779) | |
| Aciertos del detalle | — | **90,2 %** (33.248 de 36.840) | |
| Lecturas de MongoDB por pedido | 10,1 (233.230 en total) | **0,9** (75.350 en total) | − 91 % |
| CPU de Solr (a mitad de la corrida) | 181 % | **17 %** | |
| CPU de MongoDB (a mitad de la corrida) | 181 % | **37 %** | |

### Lectura

- **La caché triplica y media el throughput y baja la latencia p95 a un tercio**, con el mismo hardware y sin errores. Sin caché, MongoDB y Solr estaban saturados; con caché casi no trabajan y el límite pasa a ser la CPU de forum-service, que arma las respuestas.
- **Los aciertos superan el 90 %** en las dos lecturas, por encima del 80 % que esperaba el ADR-007.
- Con Memcached hubo **146 lecturas del listado y 1 del detalle que superaron los 100 ms** (0,2 %). Con toda la máquina al máximo, Memcached a veces no responde a tiempo; esos pedidos se leyeron de MongoDB sin error, como se diseñó.
- **Primera medición, descartada:** con calificar invalidando también el listado (como decía el ADR-007), el listado tuvo **28 % a 33 % de aciertos**: con unas 6 calificaciones por segundo, la generación cambiaba antes de que las páginas se reusaran. Calificar pasó a borrar sólo el detalle (ver la actualización del ADR-007).
- El `min` negativo que muestra k6 en algunas duraciones es un problema conocido del reloj de k6 en Docker sobre Windows. No afecta a la mediana ni a los percentiles.

### Otras pruebas con el sistema levantado

| Prueba | Resultado |
|---|---|
| Un resumen aprobado aparece en el listado cacheado | **7 a 10 s** (límite de RN-26: 60 s) |
| Un resumen eliminado, con el detalle y el listado en la caché | Deja de verse **enseguida**: el detalle responde `404` y el listado ya no lo muestra |
| Calificar con el detalle en la caché | El detalle muestra el promedio nuevo en el pedido siguiente |
| `docker compose stop memcached` | Listado, detalle, aprobar, calificar y eliminar responden sin errores; el detalle tarda unos 130 ms (100 ms de timeout de Memcached) |
| `docker compose start memcached` | La caché se vuelve a llenar sola, sin reiniciar forum-service |

### Cómo repetirlo

```bash
# Sin caché: FORUM_CACHE_PROVIDER=none en el .env
docker compose up -d --force-recreate forum-service
docker run --rm -i --network estudiario_default -e BASE_URL=http://forum-service:8083   -e INTERNAL_TOKEN=$(grep ^INTERNAL_TOKEN= .env | cut -d= -f2 | tr -d '')   grafana/k6:0.54.0 run - < tests/load/forum-read.js

# Con caché: FORUM_CACHE_PROVIDER=memcached en el .env, y lo mismo
docker compose restart memcached
docker compose up -d --force-recreate forum-service
docker run ... (igual que arriba)

# Aciertos de la corrida
docker compose logs --since 5m forum-service | grep '"caché del foro"'
```
