#!/bin/sh
# Crea una base y un usuario por servicio (ADR-003): ningún servicio tiene
# credenciales para la base de otro. PostgreSQL lo ejecuta sólo la primera vez,
# cuando el volumen de datos está vacío.
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<-EOSQL
	CREATE USER users_svc WITH PASSWORD '$USERS_DB_PASSWORD';
	CREATE DATABASE users OWNER users_svc;
	REVOKE ALL ON DATABASE users FROM PUBLIC;

	CREATE USER planner_svc WITH PASSWORD '$PLANNER_DB_PASSWORD';
	CREATE DATABASE planner OWNER planner_svc;
	REVOKE ALL ON DATABASE planner FROM PUBLIC;
EOSQL

# planner-service usa una restricción EXCLUDE con gist para impedir sesiones
# superpuestas (ADR-003). La extensión la crea el superusuario.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname planner \
	-c 'CREATE EXTENSION IF NOT EXISTS btree_gist;'
