-- Esquema de planner-service (base `planner`). Ver docs/adr/ADR-003-persistencia.md.
-- Se aplica al arrancar el servicio; todas las sentencias son idempotentes.

CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Una fila por estudiante. Se bloquea con SELECT ... FOR UPDATE al confirmar
-- un plan, para que dos confirmaciones del mismo estudiante no se validen a la
-- vez (RN-12, RN-13, RN-14).
CREATE TABLE IF NOT EXISTS students (
    id TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS subjects (
    id          TEXT PRIMARY KEY,
    student_id  TEXT NOT NULL,
    name        TEXT NOT NULL,
    professors  TEXT[] NOT NULL DEFAULT '{}',
    difficulty  SMALLINT NOT NULL CHECK (difficulty BETWEEN 1 AND 4),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS subjects_student_idx ON subjects (student_id);

CREATE TABLE IF NOT EXISTS exams (
    id          TEXT PRIMARY KEY,
    student_id  TEXT NOT NULL,
    subject_id  TEXT NOT NULL REFERENCES subjects (id),
    type        TEXT NOT NULL,
    starts_at   TIMESTAMPTZ NOT NULL,
    topics      TEXT[] NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS exams_student_idx ON exams (student_id, starts_at);

CREATE TABLE IF NOT EXISTS availability (
    student_id                 TEXT PRIMARY KEY,
    slots                      JSONB NOT NULL,
    max_daily_minutes          INT NOT NULL CHECK (max_daily_minutes > 0),
    preferred_session_minutes  INT NOT NULL CHECK (preferred_session_minutes > 0),
    timezone                   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS plans (
    id            TEXT PRIMARY KEY,
    student_id    TEXT NOT NULL,
    exam_id       TEXT NOT NULL REFERENCES exams (id),
    status        TEXT NOT NULL,
    generated_by  TEXT NOT NULL,
    warnings      TEXT[] NOT NULL DEFAULT '{}',
    drafts        JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL
);
-- RN-15: un examen tiene como máximo un plan confirmado vigente.
CREATE UNIQUE INDEX IF NOT EXISTS plans_one_confirmed_per_exam ON plans (exam_id) WHERE status = 'confirmado';

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    plan_id     TEXT NOT NULL REFERENCES plans (id),
    student_id  TEXT NOT NULL,
    exam_id     TEXT NOT NULL,
    starts_at   TIMESTAMPTZ NOT NULL,
    ends_at     TIMESTAMPTZ NOT NULL,
    topic       TEXT NOT NULL,
    objective   TEXT NOT NULL,
    status      TEXT NOT NULL,
    CHECK (ends_at > starts_at),
    -- RN-12: el motor impide que dos sesiones del mismo estudiante que ocupan
    -- el calendario se superpongan, aunque las validaciones de la aplicación
    -- fallaran. Las sesiones pospuestas liberan su horario.
    CONSTRAINT sessions_no_overlap EXCLUDE USING gist (
        student_id WITH =,
        tstzrange(starts_at, ends_at) WITH &&
    ) WHERE (status <> 'pospuesta')
);
CREATE INDEX IF NOT EXISTS sessions_student_idx ON sessions (student_id, starts_at);

-- RN-14: cada confirmación se registra con su clave de idempotencia y su
-- resultado, para devolver lo mismo ante un reintento.
CREATE TABLE IF NOT EXISTS confirmations (
    student_id       TEXT NOT NULL,
    idempotency_key  TEXT NOT NULL,
    plan_id          TEXT NOT NULL REFERENCES plans (id),
    result           JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (student_id, idempotency_key)
);

-- Material de cada materia (teóricos y prácticos): el plan se arma con lo que
-- el estudiante realmente tiene para estudiar.
CREATE TABLE IF NOT EXISTS materials (
    id          TEXT PRIMARY KEY,
    student_id  TEXT NOT NULL,
    subject_id  TEXT NOT NULL REFERENCES subjects (id),
    kind        TEXT NOT NULL CHECK (kind IN ('teorico', 'practico')),
    title       TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS materials_subject_idx ON materials (student_id, subject_id, created_at);

-- Modalidad del examen y material que entra. Se agregan con ALTER para que las
-- bases que ya existían se adapten solas al arrancar.
ALTER TABLE exams ADD COLUMN IF NOT EXISTS modality TEXT NOT NULL DEFAULT '';
ALTER TABLE exams ADD COLUMN IF NOT EXISTS material_ids TEXT[] NOT NULL DEFAULT '{}';
