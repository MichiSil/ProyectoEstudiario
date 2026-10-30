-- Esquema de users-service (base `users`). Lo aplica el servicio al arrancar;
-- es idempotente.

CREATE TABLE IF NOT EXISTS users (
    id            TEXT        PRIMARY KEY,
    name          TEXT        NOT NULL,
    -- RN-01: no puede haber dos usuarios con el mismo mail. Se guarda en
    -- minúsculas, así la restricción vale sin importar cómo lo escriba el usuario.
    email         TEXT        NOT NULL UNIQUE CHECK (email = lower(email)),
    -- RN-02: sólo el hash bcrypt, nunca la contraseña.
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL CHECK (role IN ('estudiante', 'admin')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
