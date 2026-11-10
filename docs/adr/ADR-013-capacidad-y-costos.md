# ADR-013: Capacidad y costos

- **Estado:** Aceptada (repetida con el sistema completo el 2026-11-10, ver [Actualización](#actualización-2026-11-10-sistema-completo))
- **Fecha:** 2026-10-19
- **Decisión del enunciado:** D13
- **Reemplaza a:** —

## Contexto

El enunciado pide pruebas de carga con resultados documentados y analizados, el principal límite identificado, una alternativa de escalado y una estimación de costos.

En la Entrega 2 el servicio operativo es **planner-service**, que resuelve la acción principal: confirmar el plan de estudio (RF-09). Es también la operación con más trabajo por pedido: dentro de una transacción bloquea al estudiante, valida RN-10 a RN-13 contra sus sesiones, inserta las sesiones nuevas y registra la clave de idempotencia ([ADR-003](ADR-003-persistencia.md)).

**Demanda esperada.** Estudiario apunta a estudiantes de una facultad. Tomamos como referencia **5.000 estudiantes activos**. El pico de uso es antes de una semana de parciales: si la mitad arma o rehace su plan en las 2 horas de mayor uso de una noche, son 2.500 flujos en 7.200 s, unos **0,35 flujos por segundo**. Con un margen de 10 veces por picos dentro de esa franja, el objetivo es sostener **4 flujos por segundo** con la confirmación por debajo de 500 ms en el p95.

## Opciones consideradas

**Qué medir**

1. **Sólo la confirmación**, con datos precargados. Aísla la operación crítica, pero no refleja el uso real: antes de confirmar, el estudiante siempre carga datos y pide una propuesta.
2. **El flujo completo de un estudiante (elegida).** Materia, disponibilidad, examen, propuesta, confirmación y un reintento de la confirmación. Mide el camino real y, de paso, verifica bajo carga que el reintento no duplica sesiones.

**Herramienta**

1. **JMeter.** Muy completo, pero los escenarios se arman en XML o con interfaz gráfica, y son difíciles de revisar en un pull request.
2. **k6 (elegida).** Los escenarios son JavaScript versionado en el repositorio, define umbrales que hacen fallar la prueba si no se cumplen y corre en Docker sin instalar nada.

## Decisión

Se mide el flujo completo con k6 ([`tests/load/plan-flow.js`](../../tests/load/plan-flow.js)), con planner-service y PostgreSQL limitados a **1 vCPU cada uno**, como una instancia chica de la nube. Los umbrales de aceptación son **p95 < 500 ms** para proponer y para confirmar, y **menos de 1 % de errores**.

### Resultados

El detalle está en [`tests/load/RESULTADOS.md`](../../tests/load/RESULTADOS.md).

| Escenario | Flujos por segundo | Errores | p95 confirmar | p95 proponer |
|---|---|---|---|---|
| Hasta 100 usuarios simultáneos | **273** | 0 % | 100 ms | 101 ms |
| Hasta 200 usuarios simultáneos | 256 | 0 % | 195 ms | 206 ms |

Después de la primera prueba, la base tenía 46.387 planes confirmados, 46.387 confirmaciones registradas, 8 sesiones por plan y **ningún examen con dos planes confirmados**. Los 46.387 reintentos no duplicaron sesiones.

### Principal límite: la CPU de PostgreSQL

- Con 200 usuarios, PostgreSQL estuvo al **100 % de su CPU** durante toda la prueba, mientras planner-service usaba alrededor del 50 % de la suya y menos de 35 MB de memoria.
- Al duplicar los usuarios, el throughput no creció y la latencia se duplicó: los pedidos hacen fila en la base.
- Es lo esperable por diseño: la confirmación serializa las operaciones **de un mismo estudiante** con un bloqueo de fila y valida contra la restricción de exclusión `gist`. Entre estudiantes distintos no hay bloqueo, así que el límite es el trabajo total de la base y no la contención.

### Capacidad

Con 1 vCPU para cada componente, el sistema sostiene unos **250 flujos por segundo**, frente a un objetivo de 4. Es un margen de más de **60 veces** sobre el pico estimado. Para la demanda prevista, una instancia chica de cada componente alcanza holgadamente.

### Alternativas de escalado, en orden

1. **Escalar verticalmente PostgreSQL** (más vCPU). Es el cuello de botella y no requiere cambios de código.
2. **Más instancias de planner-service** detrás del api-gateway. El servicio no guarda estado en memoria, así que escala horizontalmente sin cambios. Hoy no es el límite: sólo tiene sentido después del punto 1.
3. **Pool de conexiones** (por ejemplo PgBouncer) cuando haya muchas instancias de planner-service compartiendo la base.
4. **Réplicas de lectura** para el calendario y las consultas de sesiones, que no necesitan ver la última escritura al instante. La confirmación sigue yendo a la base principal.
5. **Particionar por estudiante.** No hay datos compartidos entre estudiantes, así que se podría repartir en varias bases. Sólo se justificaría con varios órdenes de magnitud más de usuarios.

### Estimación de costos

Valores **aproximados y a confirmar** con la calculadora de cada proveedor, porque cambian seguido. Sirven para comparar escenarios, no como presupuesto.

| Escenario | planner-service | PostgreSQL | Costo mensual aproximado |
|---|---|---|---|
| **Evaluación de la materia** | Plan gratuito de una plataforma como Render (la instancia se duerme sin tráfico) | Plan gratuito de PostgreSQL administrado (por ejemplo Neon o Supabase) | **USD 0** |
| **5.000 estudiantes** (demanda estimada) | 1 instancia chica siempre encendida (≈ 0,5 a 1 vCPU, 512 MB) | PostgreSQL administrado chico (1 vCPU, 1 a 2 GB) | **USD 15 a 40** |
| **50.000 estudiantes** (10 veces más) | 2 instancias, por disponibilidad más que por carga | PostgreSQL de 2 vCPU con respaldo automático | **USD 80 a 150** |

El costo lo domina la base de datos, igual que la capacidad. A esto se suma el proveedor de IA, que **se cobra por uso** (tokens por propuesta) y no por capacidad. Si el costo de la IA crece, el sistema puede limitar las propuestas por estudiante, ya que el motor de reglas siempre está disponible como alternativa sin costo (RN-09).

## Consecuencias

**Positivas**

- Hay una medición reproducible: cualquiera del grupo puede correr el mismo script con un comando y compararlo con estos números.
- La prueba confirma bajo carga lo que el diseño prometía: sin errores, sin duplicados (RN-14) y sin dos planes confirmados para un mismo examen (RN-15).
- El límite está identificado y la primera alternativa de escalado no requiere cambiar código.

**Negativas / limitaciones aceptadas**

- La prueba corre en una sola máquina: no incluye latencia de red real ni el paso por el api-gateway.
- Se midió sin el proveedor de IA. Con IA, la propuesta va a tardar segundos y el límite pasa a ser el proveedor (timeout de 8 s y fallback al motor de reglas, [ADR-005](ADR-005-comunicacion.md)). Eso no afecta a la confirmación, que nunca llama a la IA.
- Todos los usuarios de la prueba son estudiantes distintos. No se midió el caso de muchas confirmaciones simultáneas del **mismo** estudiante, que se serializan a propósito; ese caso lo cubre el test de integración de confirmaciones concurrentes.

**A repetir para la presentación final**

- Con el sistema completo: a través del api-gateway, con el foro (lecturas con caché) y con el proveedor de IA.
- Con el sistema desplegado en la nube, para medir con latencia de red real.

## Actualización (2026-11-10): sistema completo

Se repitió la prueba con el **sistema completo**, como pedía la sección *A repetir*: el flujo principal del plan y la lectura del foro (con caché) **al mismo tiempo y a través del api-gateway**, con el `docker compose` entero (22 contenedores) en una notebook. Script: [`tests/load/sistema-completo.js`](../../tests/load/sistema-completo.js). Detalle: [`tests/load/RESULTADOS.md`](../../tests/load/RESULTADOS.md#2026-11-10-sistema-completo-por-el-api-gateway).

### Resultados

| Usuarios (plan + foro) | Flujos del plan por segundo | Pedidos del foro por segundo | Errores | p95 confirmar |
|---|---|---|---|---|
| 10 + 10 | **3,9** | 70,7 | 0 % | 726 ms |
| 50 + 50 | 6,1 | 91,8 | 0,04 % | 2,4 s |
| 100 + 100 | 6,9 | 111,9 | 0 % | 3,6 s |
| Referencia: sólo planner-service, sin gateway | 34,9 | — | 0 % | 820 ms |

Después de todas las corridas: 15.456 planes confirmados, 15.456 confirmaciones, 8 sesiones por plan y **ningún examen con dos planes confirmados**. Bajo carga y con el sistema completo, RN-14 y RN-15 se siguen cumpliendo.

La notebook resultó unas **8 veces más lenta** que la máquina de la primera medición para el mismo script (35 contra 273 flujos por segundo). Por eso estos números no se comparan con los de octubre: se comparan entre sí.

### Principal límite

- **Con todo en una máquina, el límite es la CPU de la máquina, compartida por todos los componentes.** De 50 a 100 usuarios el throughput casi no crece y la latencia se duplica, con los contenedores sumando siempre lo mismo (680 % de promedio, 850 % de pico) y ninguno al máximo. Es el caso del despliegue actual en la nube: una sola VM con todo.
- **Dentro de esa CPU, el que más consume es el api-gateway** (1,2 a 1,5 CPU), porque todo pasa por él y en cada pedido valida el JWT, arma una traza y registra métricas. Le siguen RabbitMQ (ver abajo) y **PostgreSQL**, que sigue siendo el límite del flujo del plan cuando se lo mide solo (241 % de CPU contra 172 % de planner-service).
- **La memoria es el segundo límite.** El sistema completo usa entre 1,6 y 2,2 GB. Con 2,9 GB para Docker, la prueba encontró a Jaeger guardando trazas en memoria sin tope (928 MB) y a la VM usando todo el swap, con pausas de 30 s y fallas del DNS interno de Docker. La VM de la nube tiene 4 GB.

Se corrigieron dos cosas de infraestructura que la prueba encontró, sin tocar código:

| Cambio | Efecto |
|---|---|
| Jaeger con `MEMORY_MAX_TRACES: 5000` | De 928 MB a ~145 MB con la misma carga |
| Healthcheck de RabbitMQ cada 30 s (cada 5 s sólo mientras arranca) y `+sbwt none` | RabbitMQ en reposo de ~250 % a ~1 % de CPU: cada chequeo arranca una VM de Erlang que gasta ~9 s de CPU |

### Capacidad frente a la demanda

La demanda estimada en la sección *Contexto* es de **0,35 flujos por segundo** en la hora pico, con un objetivo de **4 flujos por segundo** (10 veces de margen) y el p95 de confirmar por debajo de 500 ms.

- En la notebook, con el sistema completo, se sostuvieron **3,9 flujos por segundo junto con 71 lecturas del foro por segundo, sin errores**. El p95 de confirmar (726 ms) quedó arriba del objetivo en esa corrida: el objetivo de latencia se cumple con menos carga.
- Para la demanda esperada (0,35 flujos por segundo), alcanza con holgura. Para el margen de 10 veces, en esta notebook, no.
- **La VM de la nube** (`Standard_B2s`: 2 vCPU *burstable* y 4 GB) tiene bastante menos CPU que la notebook, y además sólo sostiene el 100 % de sus 2 vCPU mientras le duran los créditos de CPU. Con estas mediciones, **alcanza para la demanda estimada y para la evaluación, pero no para el margen de 10 veces**. Queda por medir ahí (se repite cuando el despliegue esté estable).

### Alternativas de escalado, en orden

Reemplazan la lista de la primera medición, que pensaba sólo en planner-service:

1. **Muestrear las trazas** (por ejemplo, el 10 % de los pedidos más todos los que fallan) en vez de trazar todos. Baja la CPU del gateway y de cada servicio, y la memoria de Jaeger, sin cambiar la arquitectura. Es configuración de OpenTelemetry.
2. **Una VM más grande y no *burstable*** para el despliegue de una sola máquina (por ejemplo 4 vCPU y 16 GB). No requiere cambios.
3. **Separar las bases** (PostgreSQL y MongoDB) a servicios administrados, para que no compitan por CPU con los servicios. PostgreSQL es el límite del flujo del plan.
4. **Una segunda instancia del api-gateway y de users-service** detrás de Caddy, como forum-service. El gateway no guarda estado salvo el control de tráfico, que pasaría a ser por instancia (o compartido en Memcached). Además resuelve que reiniciar users-service corte el login ([POSTMORTEM](../POSTMORTEM.md)).
5. Las de la primera medición siguen valiendo para PostgreSQL una vez separado: más vCPU, PgBouncer con varias instancias de planner-service y réplicas de lectura.

### Costos

Valores **aproximados y a confirmar** con la calculadora de Azure (región East US, pago por uso), para comparar escenarios:

| Escenario | Qué se usa | Costo mensual aproximado |
|---|---|---|
| **Evaluación de la materia** (hoy) | Una VM `Standard_B2s` (2 vCPU, 4 GB) con todo el `docker compose` | **USD 30** (del crédito de Azure for Students; detenida no cobra el cómputo) |
| **5.000 estudiantes** (demanda estimada) | Una VM de 4 vCPU y 16 GB no *burstable* (por ejemplo `D4s_v5`) con todo, y trazas muestreadas | **USD 140 a 160** |
| **5.000 estudiantes con margen de 10 veces** | Una VM de 4 vCPU para los servicios, PostgreSQL administrado (2 vCPU) y MongoDB administrado chico | **USD 250 a 350** |
| **50.000 estudiantes** | Dos VM para los servicios detrás de un balanceador, PostgreSQL de 4 vCPU con réplica, MongoDB administrado | **USD 600 a 900** |

Respecto de la primera estimación (USD 15 a 40 para 5.000 estudiantes), el costo sube porque aquella consideraba sólo planner-service y PostgreSQL, y ahora el sistema suma MongoDB, Solr, RabbitMQ, MinIO, Memcached, Prometheus, Grafana y Jaeger. **El costo lo sigue dominando lo que guarda datos**: las bases y Solr son lo que más memoria y CPU pide. La IA se sigue cobrando por uso, y el motor de reglas sigue siendo la alternativa sin costo (RN-09).

### Consecuencias de la actualización

- La prueba encontró dos problemas de configuración que en la nube habrían costado caro: Jaeger llenando la memoria de la VM de 4 GB con tráfico sostenido y el healthcheck de RabbitMQ gastando una parte grande de sus 2 vCPU. Los dos se corrigieron en `docker-compose.yml`, que usa también el despliegue.
- El sistema completo sigue sin perder ni duplicar datos bajo carga.
- **Limitaciones:** todo corre en una notebook, k6 incluido, y corridas iguales variaron hasta un 30 %. No se midió con la IA (sin clave en el `.env` local) ni en la nube; las dos quedan pendientes.
