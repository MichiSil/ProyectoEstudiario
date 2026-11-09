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

-- Origen de la cuenta: "estudiario" si se registró en la app, o el sistema
-- consumidor que la creó (por ejemplo agencia_de_viajes, ADR-014).
ALTER TABLE users ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'estudiario';

-- Vinculación con el Club de Beneficios del Grupo 1 (RF-28, ADR-009). Una por
-- usuario. external_user_id y event_id son los que se mandan al Club.
CREATE TABLE IF NOT EXISTS club_links (
    user_id          TEXT        PRIMARY KEY REFERENCES users(id),
    external_user_id TEXT        NOT NULL UNIQUE,
    event_id         TEXT        NOT NULL UNIQUE,
    status           TEXT        NOT NULL CHECK (status IN ('pendiente', 'vinculada')),
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL
);

-- Puntos a acreditar en el Club. RN-42: event_id es único, así un resumen
-- aprobado suma una sola vez aunque el evento llegue repetido. Quedan
-- 'pendiente' mientras el usuario no esté vinculado o el Club no responda
-- (RN-43), y se acreditan solos cuando se puede.
CREATE TABLE IF NOT EXISTS club_credits (
    event_id     TEXT        PRIMARY KEY,
    user_id      TEXT        NOT NULL REFERENCES users(id),
    reference    TEXT        NOT NULL,
    points       INTEGER     NOT NULL CHECK (points > 0),
    reason       TEXT        NOT NULL,
    status       TEXT        NOT NULL CHECK (status IN ('pendiente', 'acreditado', 'rechazado')),
    movement_id  TEXT,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL,
    credited_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS club_credits_pending ON club_credits (user_id) WHERE status = 'pendiente';

-- Eventos de RabbitMQ ya procesados, para que el consumidor sea idempotente
-- (ADR-005, 3.4). status: 'cuenta_creada' (falta el mail) o 'terminado'.
CREATE TABLE IF NOT EXISTS processed_events (
    event_id     TEXT        PRIMARY KEY,
    event_type   TEXT        NOT NULL,
    status       TEXT        NOT NULL,
    user_id      TEXT,
    processed_at TIMESTAMPTZ NOT NULL
);
