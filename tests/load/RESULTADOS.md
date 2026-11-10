# Resultados de las pruebas de carga

Scripts (k6): [`plan-flow.js`](plan-flow.js) para planner-service, [`forum-read.js`](forum-read.js) para la caché del foro y [`sistema-completo.js`](sistema-completo.js) para el sistema completo por el api-gateway. Análisis y decisiones: [ADR-013](../../docs/adr/ADR-013-capacidad-y-costos.md) y [ADR-007](../../docs/adr/ADR-007-cache.md).

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
docker run --rm -i --network estudiario_default -e BASE_URL=http://forum-service:8083   -e INTERNAL_TOKEN=$(grep ^INTERNAL_TOKEN= .env | cut -d= -f2 | tr -d '
')   grafana/k6:0.54.0 run - < tests/load/forum-read.js

# Con caché: FORUM_CACHE_PROVIDER=memcached en el .env, y lo mismo
docker compose restart memcached
docker compose up -d --force-recreate forum-service
docker run ... (igual que arriba)

# Aciertos de la corrida
docker compose logs --since 5m forum-service | grep '"caché del foro"'
```

## 2026-11-10: sistema completo por el api-gateway

### Entorno

| Componente | Recursos |
|---|---|
| Máquina | Notebook con Windows 11, Intel Core i7-1355U (2 núcleos de rendimiento y 8 de eficiencia, 12 hilos) |
| Docker Desktop | 12 CPU y **2,9 GB de RAM** para todos los contenedores, **sin límites por contenedor** |
| Sistema | El `docker compose` completo: 22 contenedores, con las dos instancias de forum-service, Memcached, Solr, RabbitMQ, Prometheus, Grafana y Jaeger |
| Generador de carga | k6 0.54 en otro contenedor de la misma máquina, contra el **api-gateway** por la red de Docker |
| Generador de planes | Motor de reglas (sin clave de IA en el `.env` local) |

A diferencia de las pruebas anteriores, cada pedido pasa por el gateway: validación del JWT, control de tráfico (subido a 1.000.000 por minuto para la prueba), traza, métricas y, en el foro, balanceo entre las dos instancias. Todo compite por la CPU de una sola notebook, k6 incluido.

### Escenario

[`sistema-completo.js`](sistema-completo.js) corre a la vez dos grupos de usuarios virtuales, cada uno con la rampa de `plan-flow.js` (20 %, 50 % y 100 % de los usuarios en 30 s, 1 min y 1 min, y bajada en 20 s):

- **plan:** el flujo principal de `plan-flow.js` (materia, disponibilidad, examen, propuesta, confirmación y reintento con la misma `Idempotency-Key`), un estudiante nuevo por iteración;
- **foro:** la mezcla de `forum-read.js` (55 % listado, 42 % detalle, 3 % calificar) sobre 40 resúmenes aprobados, con la caché prendida.

Los JWT los firma el script con el `JWT_SECRET` del `.env`, como `devtoken`, para no medir el registro y el login.

### Resultados

Flujos y pedidos por segundo, promediados sobre los 2 min 50 s de la corrida (como en las pruebas de octubre):

| Usuarios (plan + foro) | Flujos del plan | Pedidos del foro | Pedidos totales | Errores | Confirmar p50 / p95 | Proponer p95 | Listado p95 | Detalle p95 |
|---|---|---|---|---|---|---|---|---|
| 10 + 10 | 669 (**3,9/s**) | 70,7/s | 86,0/s | **0 %** | 322 / **726 ms** | 392 ms | 160 ms | 130 ms |
| 50 + 50 | 1.031 (6,1/s) | 91,8/s | 115,8/s | 0,04 % | 977 / 2.398 ms | 2.362 ms | 696 ms | 615 ms |
| 100 + 100 | 1.172 (6,9/s) | 111,9/s | 140,5/s | 0 % | 1.679 / 3.565 ms | 4.022 ms | 1.108 ms | 914 ms |
| Referencia: `plan-flow.js` directo a planner-service, hasta 100 usuarios | 5.929 (34,9/s) | — | 209,5/s | 0 % | 320 / 820 ms | 959 ms | — | — |

Uso de CPU (`docker stats` cada ~20 s) con 100 + 100:

| Contenedor | CPU promedio | CPU máxima |
|---|---|---|
| **api-gateway** | **123 %** | 161 % |
| RabbitMQ | 103 % | 225 % |
| PostgreSQL | 91 % | 119 % |
| planner-service | 70 % | 99 % |
| k6 | 67 % | 104 % |
| forum-service-1 y forum-service-2 | 61 % y 58 % | 94 % y 92 % |
| MongoDB | 44 % | 99 % |
| **Todos los contenedores** | **679 %** | **842 %** |

En la referencia directa a planner-service, PostgreSQL usó 241 % y planner-service 172 %.

**Consistencia después de todas las corridas** (consulta directa a PostgreSQL): **15.456 planes confirmados, 15.456 confirmaciones, 123.648 sesiones (8 por plan) y ningún examen con más de un plan confirmado.** Ningún reintento duplicó sesiones.

### Lectura

- **El límite en esta notebook es la CPU de toda la máquina, no un servicio.** Con 50 + 50 y con 100 + 100 los contenedores suman lo mismo (680 % de promedio, 850 % de pico): de 50 a 100 usuarios el throughput apenas crece (de 116 a 141 pedidos por segundo) y la latencia se duplica. Ningún contenedor llega a su máximo.
- **Dentro del sistema, el que más CPU usa es el api-gateway** (1,2 a 1,5 CPU): valida un JWT, arma una traza y registra métricas en cada pedido, y es el único componente por el que pasa todo. En el camino del plan sin gateway, el que más usa sigue siendo **PostgreSQL**, como en octubre: la confirmación es la operación más cara (p50 de 320 ms aun con poca carga).
- **El sistema completo rinde unas 5 veces menos que planner-service solo** en la misma máquina (6,9 contra 34,9 flujos por segundo): el plan comparte la CPU con el gateway y con el foro, que en estas corridas recibió entre 15 y 18 veces más pedidos que flujos tuvo el plan.
- **Esta notebook es unas 8 veces más lenta que la máquina de octubre** para el mismo `plan-flow.js` (35 contra 273 flujos por segundo, y allá planner y PostgreSQL tenían sólo 1 vCPU cada uno). Los números sirven para comparar corridas entre sí, no como capacidad de la nube.
- **Contra el objetivo del ADR-013** (4 flujos por segundo con el p95 de confirmar por debajo de 500 ms): con 10 + 10 usuarios se sostuvieron 3,9 flujos por segundo **junto con 71 lecturas del foro por segundo**, sin errores, pero el p95 de confirmar fue de 726 ms. En esta máquina, el objetivo de latencia se cumple con menos carga que la de esa corrida.
- **Corridas repetidas con la misma carga variaron hasta un 30 %** (por ejemplo, 50 + 50 dio 8,7 flujos por segundo antes del cambio de RabbitMQ de abajo y 6,1 después, sin que el cambio pudiera empeorarlo). Con más de una hora de carga sostenida, la notebook no da números estables: hay que mirar las tendencias.

### Problemas que encontró la prueba (y se corrigieron)

Las primeras tres corridas (50 + 50, 20 + 20 y 100 + 100) se **descartaron**:

| Problema | Qué se vio | Corrección |
|---|---|---|
| **Jaeger sin tope de memoria** | Jaeger (`all-in-one`) guarda las trazas en memoria y no tenía límite: llegó a **928 MB** de los 2,9 GB, la VM de Docker usaba **2 GB de swap** (todo el disponible), hubo pausas de **hasta 30 s** y en una corrida falló el DNS interno de Docker (`lookup forum-service on 127.0.0.11:53`): el gateway sacó las dos instancias del foro de la rotación y respondió 1.403 veces `ninguna instancia disponible` (7,5 % de errores). | `MEMORY_MAX_TRACES: 5000` en `docker-compose.yml`: Jaeger quedó en **~145 MB** con la misma carga. Con 20.000 trazas quedaba en ~480 MB. |
| **Healthcheck de RabbitMQ** | RabbitMQ usaba **2 a 4 CPU sin mensajes**. La mayor parte era su propio healthcheck: `rabbitmq-diagnostics` arranca una VM de Erlang que gasta unos 9 s de CPU, y corría cada 5 s. Era también la causa de que el healthcheck pasara su timeout en la jornada de fallas ([POSTMORTEM](../../docs/POSTMORTEM.md)). | Healthcheck cada 5 s sólo durante el arranque (`start_interval`) y después cada 30 s, y `+sbwt none` para que los schedulers de Erlang no esperen girando. En reposo, RabbitMQ pasó de **~250 % a ~1 %** de CPU. |
| **Listado del foro con Solr lento** | En la primera corrida, 125 listados respondieron `503 SEARCH_UNAVAILABLE`: con la caché vencida, Solr no contestó en sus 2 s. | No se corrigió acá: es el mismo hallazgo del POSTMORTEM (el listado depende de Solr). Con Jaeger acotado no volvió a pasar. |

### Cómo repetirlo

```bash
RATE_LIMIT_USER_PER_MIN=1000000 docker compose up -d api-gateway      # que el control de tráfico no frene la prueba
docker run --rm -i --network estudiario_default \
  -e JWT_SECRET=$(grep ^JWT_SECRET= .env | cut -d= -f2 | tr -d '\r') \
  -e PLAN_VUS=50 -e FORO_VUS=50 \
  grafana/k6:0.54.0 run - < tests/load/sistema-completo.js
docker compose up -d api-gateway                                       # vuelve el límite del .env

# referencia: el plan solo, directo a planner-service
docker run --rm -i --network estudiario_default -e BASE_URL=http://planner-service:8082 \
  -e INTERNAL_TOKEN=$(grep ^INTERNAL_TOKEN= .env | cut -d= -f2 | tr -d '\r') \
  grafana/k6:0.54.0 run - < tests/load/plan-flow.js
```
