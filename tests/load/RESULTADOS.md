# Resultados de las pruebas de carga

Script: [`plan-flow.js`](plan-flow.js) (k6). Análisis y decisiones: [ADR-013](../../docs/adr/ADR-013-capacidad-y-costos.md).

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
