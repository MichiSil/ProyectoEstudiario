#!/bin/sh
# Crea la base `users` con su propio usuario, para que users-service sea el
# único dueño de sus datos y no tenga acceso a la base `planner` (ADR-001,
# ADR-003).
#
# Es idempotente: lo corre el contenedor users-db-init en cada
# `docker compose up`, así funciona igual con un volumen de PostgreSQL nuevo
# o con uno que ya tenía la base `planner` de antes.
#
# Se conecta como el administrador del contenedor (variables PG* del entorno).
set -eu

: "${USERS_DB_USER:?falta USERS_DB_USER}"
: "${USERS_DB_PASSWORD:?falta USERS_DB_PASSWORD}"

psql -v ON_ERROR_STOP=1 \
	-v users_user="$USERS_DB_USER" \
	-v users_password="$USERS_DB_PASSWORD" <<'SQL'
-- Usuario propio de users-service: sin superusuario y sin permiso para crear bases.
SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'users_user', :'users_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'users_user') \gexec
SELECT format('ALTER ROLE %I WITH LOGIN PASSWORD %L', :'users_user', :'users_password') \gexec

-- Base `users`, cuyo dueño es ese usuario.
SELECT format('CREATE DATABASE users OWNER %I', :'users_user')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'users') \gexec

-- Por defecto cualquier usuario puede conectarse a cualquier base: se le
-- quita ese permiso a PUBLIC para que cada servicio entre sólo a la suya.
REVOKE CONNECT ON DATABASE users FROM PUBLIC;
REVOKE CONNECT ON DATABASE planner FROM PUBLIC;
SQL

echo "base users lista (usuario $USERS_DB_USER)"
