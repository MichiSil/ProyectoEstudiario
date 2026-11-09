#!/bin/sh
# Crea en RabbitMQ los usuarios de forum-service, notification-service y
# users-service y la topología de eventos del ADR-005 (3.1), para que ningún servicio use el
# administrador del contenedor (ADR-001). Mismo criterio que
# deploy/postgres/init-databases.sh y deploy/minio/init-storage.sh.
#
#   forum.events (topic) ──summary.approved / summary.rejected──▶ notifications.summary-moderation
#   notifications.retry   cola de espera: cada mensaje vence según su backoff y
#                         vuelve a notifications.summary-moderation
#   notifications.dlq     mensajes con error permanente o reintentos agotados
#
#   forum.events ──summary.approved──────────────▶ users.benefits          (puntos del Club, RF-28)
#   forum.events ──document.external-registered──▶ users.external-authors  (cuenta del autor externo, ADR-014)
#   cada una con su cola .retry y su .dlq, igual que las de notificaciones
#
# Permisos:
#   - forum-service sólo puede publicar en forum.events.
#   - notification-service y users-service sólo pueden leer sus colas y
#     publicar en las de reintentos y la DLQ (por el exchange por defecto).
#   Ninguno puede declarar ni borrar colas o exchanges: la topología es ésta.
#
# Es idempotente: lo corre el contenedor rabbitmq-init en cada
# `docker compose up`, contra la API HTTP de administración.
set -eu

: "${RABBITMQ_ADMIN_USER:?falta RABBITMQ_ADMIN_USER}"
: "${RABBITMQ_ADMIN_PASSWORD:?falta RABBITMQ_ADMIN_PASSWORD}"
: "${FORUM_RABBITMQ_USER:?falta FORUM_RABBITMQ_USER}"
: "${FORUM_RABBITMQ_PASSWORD:?falta FORUM_RABBITMQ_PASSWORD}"
: "${NOTIFICATION_RABBITMQ_USER:?falta NOTIFICATION_RABBITMQ_USER}"
: "${NOTIFICATION_RABBITMQ_PASSWORD:?falta NOTIFICATION_RABBITMQ_PASSWORD}"
: "${USERS_RABBITMQ_USER:?falta USERS_RABBITMQ_USER}"
: "${USERS_RABBITMQ_PASSWORD:?falta USERS_RABBITMQ_PASSWORD}"

for u in "$FORUM_RABBITMQ_USER" "$NOTIFICATION_RABBITMQ_USER" "$USERS_RABBITMQ_USER"; do
	if [ "$u" = "$RABBITMQ_ADMIN_USER" ]; then
		echo "Los usuarios de los servicios no pueden ser el administrador del contenedor." >&2
		exit 1
	fi
done

API=http://rabbitmq:15672/api
VHOST=%2F # el vhost por defecto, "/"

# api METHOD RUTA [JSON]: llama a la API de administración y corta si falla.
api() {
	if [ $# -eq 3 ]; then
		curl -fsS -u "$RABBITMQ_ADMIN_USER:$RABBITMQ_ADMIN_PASSWORD" -X "$1" \
			-H 'content-type: application/json' -d "$3" "$API$2" >/dev/null
	else
		curl -fsS -u "$RABBITMQ_ADMIN_USER:$RABBITMQ_ADMIN_PASSWORD" -X "$1" "$API$2" >/dev/null
	fi
}

for intento in $(seq 1 30); do
	if api GET /overview 2>/dev/null; then
		break
	fi
	if [ "$intento" = 30 ]; then
		echo "RabbitMQ no respondió después de 30 intentos" >&2
		exit 1
	fi
	sleep 1
done

# --- Usuarios propios (sin tags: no entran a la interfaz web) ---
api PUT "/users/$FORUM_RABBITMQ_USER" "{\"password\":\"$FORUM_RABBITMQ_PASSWORD\",\"tags\":\"\"}"
api PUT "/permissions/$VHOST/$FORUM_RABBITMQ_USER" \
	'{"configure":"^$","write":"^forum\\.events$","read":"^$"}'

api PUT "/users/$NOTIFICATION_RABBITMQ_USER" "{\"password\":\"$NOTIFICATION_RABBITMQ_PASSWORD\",\"tags\":\"\"}"
api PUT "/permissions/$VHOST/$NOTIFICATION_RABBITMQ_USER" \
	'{"configure":"^$","write":"^amq\\.default$","read":"^notifications\\..*"}'

api PUT "/users/$USERS_RABBITMQ_USER" "{\"password\":\"$USERS_RABBITMQ_PASSWORD\",\"tags\":\"\"}"
api PUT "/permissions/$VHOST/$USERS_RABBITMQ_USER" \
	'{"configure":"^$","write":"^amq\\.default$","read":"^users\\..*"}'

# --- Topología (ADR-005, 3.1) ---
api PUT "/exchanges/$VHOST/forum.events" '{"type":"topic","durable":true}'

api PUT "/queues/$VHOST/notifications.summary-moderation" '{"durable":true}'
api POST "/bindings/$VHOST/e/forum.events/q/notifications.summary-moderation" '{"routing_key":"summary.approved"}'
api POST "/bindings/$VHOST/e/forum.events/q/notifications.summary-moderation" '{"routing_key":"summary.rejected"}'

# Sin consumidores: cada mensaje lleva su TTL (expiration) y, al vencer, vuelve
# a la cola principal por el exchange por defecto.
api PUT "/queues/$VHOST/notifications.retry" \
	'{"durable":true,"arguments":{"x-dead-letter-exchange":"","x-dead-letter-routing-key":"notifications.summary-moderation"}}'

api PUT "/queues/$VHOST/notifications.dlq" '{"durable":true}'

# Colas de users-service: el mismo esquema de cola principal, reintentos y DLQ.
# users_queues COLA ROUTING_KEY
users_queues() {
	api PUT "/queues/$VHOST/$1" '{"durable":true}'
	api POST "/bindings/$VHOST/e/forum.events/q/$1" "{\"routing_key\":\"$2\"}"
	api PUT "/queues/$VHOST/$1.retry" \
		"{\"durable\":true,\"arguments\":{\"x-dead-letter-exchange\":\"\",\"x-dead-letter-routing-key\":\"$1\"}}"
	api PUT "/queues/$VHOST/$1.dlq" '{"durable":true}'
}
users_queues users.benefits summary.approved
users_queues users.external-authors document.external-registered

echo "RabbitMQ listo (exchange forum.events, colas notifications.* y users.*, usuarios $FORUM_RABBITMQ_USER, $NOTIFICATION_RABBITMQ_USER y $USERS_RABBITMQ_USER)"
