# ADR-012: Balanceo de carga

- **Estado:** Propuesta
- **Fecha:** 2026-10-19
- **Decisión del enunciado:** D12
- **Reemplaza a:** —

## Contexto

El enunciado (2.3) pide **balanceo de carga sobre al menos un servicio desplegado en dos o más instancias**. El mecanismo tiene que **reconocer cuándo una instancia no está disponible**, y la distribución y el estado de las instancias se tienen que poder **comprobar con la información de observabilidad**. La decisión D12 tiene que justificar el servicio elegido, la estrategia de distribución, la verificación de disponibilidad y el comportamiento ante la caída de una instancia.

La sección *Balanceo de carga* de [`ARCHITECTURE.md`](../ARCHITECTURE.md) ya fija la versión preliminar: **forum-service en dos instancias**, el **api-gateway** reparte los pedidos en *round robin*, consulta el `GET /health` de cada instancia y deja de enviarle pedidos a la que no responde. Este ADR completa esa decisión.

Condiciones que pesan:

- forum-service es el servicio con más tráfico de lectura: listado, búsqueda y detalle de resúmenes.
- No guarda estado en memoria: sus datos están en MongoDB, MinIO, Solr y Memcached ([ADR-003](ADR-003-persistencia.md)), compartidos por todas las instancias.
- El api-gateway ya es el único punto de entrada y ya reenvía los pedidos a los servicios, con timeouts y errores uniformes ([ADR-005](ADR-005-comunicacion.md)).

## Opciones consideradas

**Qué servicio replicar**

1. **planner-service.** Es el de la acción principal, pero la prueba de carga del [ADR-013](ADR-013-capacidad-y-costos.md) muestra que su límite es la CPU de PostgreSQL, no la del servicio: una segunda instancia no aumentaría la capacidad.
2. **forum-service (elegida).** Es el de más lecturas, no tiene estado propio y cualquier instancia puede atender cualquier pedido.

**Quién balancea**

1. **Un balanceador aparte (nginx, HAProxy o Traefik) delante de forum-service.** Son herramientas probadas, pero suman un contenedor y un salto más, y la distribución quedaría fuera de los logs y las trazas del gateway.
2. **El DNS de Docker** (`deploy.replicas` en `docker compose`, con varias direcciones para el mismo nombre). No necesita código, pero no detecta instancias caídas: el cliente sigue resolviendo la dirección de la que no responde.
3. **El api-gateway (elegida).** Ya recibe todos los pedidos del foro: agregar el balanceo ahí no suma saltos, y cada decisión queda en sus logs, sus métricas y sus trazas, con el mismo `correlationId`.

**Estrategia de distribución**

1. ***Round robin* (elegida).** Reparte los pedidos en orden entre las instancias sanas. Las dos instancias son iguales y los pedidos del foro tienen un costo parecido, así que alcanza para repartir la carga, y la distribución esperada (mitad y mitad) es fácil de comprobar.
2. **Menos conexiones activas.** Se adapta mejor a pedidos de duración muy distinta, pero es más difícil de verificar en una demostración y no aporta con dos instancias iguales.
3. **Aleatorio.** Es simple, pero con pocos pedidos el reparto puede quedar desparejo.

## Decisión

### Instancias

- forum-service corre en **dos instancias**, `forum-service-1` y `forum-service-2`, definidas en `docker-compose.yml` a partir de la misma imagen.
- Cada instancia toma su nombre de la variable `INSTANCE` y, si no está definida, de `HOSTNAME`, y lo incluye en sus logs y sus métricas. Así coincide con los logs de planner-service, que hoy usan `HOSTNAME`.

### Distribución

- El api-gateway mantiene la lista de instancias de forum-service (por configuración) y reparte los pedidos del foro en **round robin entre las instancias sanas**.
- Las demás rutas no cambian: users-service y planner-service tienen una sola instancia.

### Verificación de disponibilidad

- **Chequeo activo:** el gateway consulta el `GET /health` de cada instancia periódicamente. Si una instancia no responde o responde con error en chequeos seguidos, la saca de la rotación; cuando vuelve a responder bien, la reincorpora.
- **Chequeo pasivo:** si un pedido real a una instancia falla por error de conexión, el gateway la marca como no disponible en ese momento, sin esperar al próximo chequeo.
- **Valores iniciales**, que se ajustan con las pruebas:

  | Parámetro | Valor inicial |
  |---|---|
  | Intervalo del chequeo de `GET /health` | 5 s |
  | Timeout de cada chequeo | 2 s |
  | Fallas seguidas para sacar la instancia de la rotación | 2 |
  | Respuestas correctas para volver a usarla | 1 |

  Con estos valores, el chequeo activo saca de la rotación a una instancia caída en unos 10 s (dos chequeos seguidos). Si antes le llega un pedido real y falla, la saca el chequeo pasivo.

### Comportamiento ante la caída de una instancia

- **Pedidos nuevos:** van sólo a la instancia sana. El foro sigue funcionando con la mitad de la capacidad.
- **Pedido en curso a la instancia que se cayó:** si es una lectura (`GET`), el gateway lo reintenta una vez en la otra instancia, como permite el [ADR-005](ADR-005-comunicacion.md) para las operaciones idempotentes. Si es una escritura (`POST`, `PUT`, `PATCH`, `DELETE`), no se reintenta, porque no se sabe si la instancia llegó a aplicarla: el usuario recibe `503 UPSTREAM_UNAVAILABLE` y puede volver a intentar.
- **Las dos instancias caídas:** el gateway responde `503 UPSTREAM_UNAVAILABLE` a los pedidos del foro. El plan de estudio no se ve afectado, porque vive en otro servicio.
- **Al volver la instancia:** pasa el chequeo de salud y el gateway la reincorpora a la rotación.

### Cómo se verifica la distribución

- **Logs:** cada log de pedido de forum-service incluye el campo `instance`. Contar los logs por instancia muestra el reparto, y filtrar por `correlationId` muestra qué instancia atendió un pedido puntual.
- **Respuesta:** cada respuesta del foro indica qué instancia la atendió, en un encabezado.
- **Métricas:** las métricas de pedidos se separan por `instance` ([ADR-011](ADR-011-observabilidad.md)). El tablero muestra los pedidos por segundo de cada instancia y su estado (en rotación o fuera).
- **Trazas:** el span del pedido en forum-service lleva el nombre de la instancia, así en Jaeger se ve a cuál fue cada pedido.
- **Demostración:** con tráfico constante se apaga `forum-service-1` (`docker compose stop forum-service-1`). Los logs y el tablero muestran que todo pasa a `forum-service-2`, sin errores para las lecturas. Al volver a levantarla, el reparto vuelve a ser parejo.

## Consecuencias

**Positivas**

- Se cumple el requisito de balanceo sin sumar componentes: el gateway que ya existe reparte los pedidos y detecta las instancias caídas.
- La caída de una instancia no interrumpe las lecturas del foro, y se puede demostrar con los logs, el tablero y las trazas.
- La distribución queda en la misma observabilidad que el resto del sistema, con el mismo `correlationId`.

**Negativas / limitaciones aceptadas**

- El balanceo se programa y se mantiene en el gateway, en lugar de usar un balanceador probado.
- La lista de instancias es fija (por configuración): agregar una tercera instancia implica cambiar la configuración y reiniciar el gateway. No hay escalado automático.
- Entre que una instancia se cae y el chequeo la detecta, alguna escritura puede recibir `503`.
- Las dos instancias tienen que ser realmente sin estado: nada que dependa de la memoria de una instancia (por ejemplo, una caché local por instancia) puede usarse para lo que tiene que ser igual en las dos. Por eso la caché compartida es Memcached.
- Con varias instancias, el *relay* del *outbox* puede publicar un mismo evento más de una vez (ADR-005); los consumidores ya lo toleran por ser idempotentes.

**A validar en la presentación final**

- Implementar las dos instancias y el balanceo cuando se implemente forum-service, y ajustar los valores iniciales de los chequeos de salud con las pruebas.
- Demostrar la caída de una instancia con los logs, las métricas por instancia y el tablero.
