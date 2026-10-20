# ADR-013: Capacidad y costos

- **Estado:** Aceptada (primera medición; se repite con el sistema completo para la presentación final)
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
