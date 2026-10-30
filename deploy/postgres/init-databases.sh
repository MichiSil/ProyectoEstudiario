#!/bin/sh
# Crea el usuario y la base de cada servicio, para que cada uno sea el único
# dueño de sus datos y no tenga acceso a la base de otro (ADR-001, ADR-003):
#
#   - planner-service → base `planner`, usuario PLANNER_DB_USER
#   - users-service   → base `users`,   usuario USERS_DB_USER
#
# Ninguno de esos usuarios es superusuario. El administrador del contenedor
# (POSTGRES_ADMIN_USER) sólo lo usa este script: ningún servicio se conecta
# con él.
#
# Es idempotente: lo corre el contenedor db-init en cada `docker compose up`,
# así funciona igual con un volumen de PostgreSQL nuevo o con uno que ya tenía
# las bases de antes. En un volumen de antes, las tablas de `planner` eran del
# administrador: este script se las pasa al usuario de planner-service.
#
# Se conecta como el administrador del contenedor (variables PG* del entorno).
set -eu

: "${PLANNER_DB_USER:?falta PLANNER_DB_USER}"
: "${PLANNER_DB_PASSWORD:?falta PLANNER_DB_PASSWORD}"
: "${USERS_DB_USER:?falta USERS_DB_USER}"
: "${USERS_DB_PASSWORD:?falta USERS_DB_PASSWORD}"

if [ "$PLANNER_DB_USER" = "$PGUSER" ]; then
	echo "PLANNER_DB_USER no puede ser el administrador del contenedor ($PGUSER)." >&2
	echo "Copiá de nuevo .env.example a .env: cambió la configuración de PostgreSQL." >&2
	exit 1
fi

psql -v ON_ERROR_STOP=1 -d postgres \
	-v planner_user="$PLANNER_DB_USER" \
	-v planner_password="$PLANNER_DB_PASSWORD" \
	-v users_user="$USERS_DB_USER" \
	-v users_password="$USERS_DB_PASSWORD" <<'SQL'
-- Usuarios propios de cada servicio: sin superusuario y sin permiso para crear bases ni roles.
SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'planner_user', :'planner_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'planner_user') \gexec
SELECT format('ALTER ROLE %I WITH LOGIN NOSUPERUSER PASSWORD %L', :'planner_user', :'planner_password') \gexec

SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'users_user', :'users_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'users_user') \gexec
SELECT format('ALTER ROLE %I WITH LOGIN NOSUPERUSER PASSWORD %L', :'users_user', :'users_password') \gexec

-- Bases: la `planner` la crea el contenedor de PostgreSQL; la `users`, este script.
-- Cada una pasa a ser del usuario de su servicio.
SELECT format('CREATE DATABASE users OWNER %I', :'users_user')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'users') \gexec
SELECT format('ALTER DATABASE planner OWNER TO %I', :'planner_user') \gexec
SELECT format('ALTER DATABASE users OWNER TO %I', :'users_user') \gexec

-- Por defecto cualquier usuario puede conectarse a cualquier base: se le
-- quita ese permiso a PUBLIC para que cada servicio entre sólo a la suya.
REVOKE CONNECT ON DATABASE planner FROM PUBLIC;
REVOKE CONNECT ON DATABASE users FROM PUBLIC;
SQL

# Dentro de `planner`: el esquema y las tablas que ya existían (de un volumen de
# antes, creadas por el administrador) pasan a ser del usuario de planner-service.
psql -v ON_ERROR_STOP=1 -d planner -v planner_user="$PLANNER_DB_USER" <<'SQL'
SELECT format('ALTER SCHEMA public OWNER TO %I', :'planner_user') \gexec
SELECT format('ALTER TABLE public.%I OWNER TO %I', tablename, :'planner_user')
FROM pg_tables WHERE schemaname = 'public' AND tableowner <> :'planner_user' \gexec
SELECT format('ALTER SEQUENCE public.%I OWNER TO %I', sequencename, :'planner_user')
FROM pg_sequences WHERE schemaname = 'public' AND sequenceowner <> :'planner_user' \gexec
SQL

echo "bases listas: planner (usuario $PLANNER_DB_USER) y users (usuario $USERS_DB_USER)"
