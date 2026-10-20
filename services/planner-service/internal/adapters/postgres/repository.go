// Package postgres implementa app.PlanRepository sobre PostgreSQL.
package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

//go:embed schema.sql
var schema string

var tracer = otel.Tracer("planner-service/postgres")

type Repository struct {
	pool *pgxpool.Pool
	ids  app.IDGenerator
}

var _ app.PlanRepository = (*Repository)(nil)

func New(pool *pgxpool.Pool, ids app.IDGenerator) *Repository {
	return &Repository{pool: pool, ids: ids}
}

// Migrate aplica el esquema. Es idempotente.
func (r *Repository) Migrate(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, schema)
	return err
}

func (r *Repository) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func (r *Repository) CreateSubject(ctx context.Context, s domain.Subject) error {
	professors := s.Professors
	if professors == nil {
		professors = []string{}
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO subjects (id, student_id, name, professors, difficulty) VALUES ($1, $2, $3, $4, $5)`,
		s.ID, s.StudentID, s.Name, professors, int(s.Difficulty))
	return err
}

func (r *Repository) GetSubject(ctx context.Context, studentID, id string) (domain.Subject, error) {
	var s domain.Subject
	var diff int
	err := r.pool.QueryRow(ctx,
		`SELECT id, student_id, name, professors, difficulty FROM subjects WHERE id = $1 AND student_id = $2`,
		id, studentID).Scan(&s.ID, &s.StudentID, &s.Name, &s.Professors, &diff)
	s.Difficulty = domain.Difficulty(diff)
	return s, notFound(err)
}

func (r *Repository) ListSubjects(ctx context.Context, studentID string) ([]domain.Subject, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, student_id, name, professors, difficulty FROM subjects WHERE student_id = $1 ORDER BY name`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Subject{}
	for rows.Next() {
		var s domain.Subject
		var diff int
		if err := rows.Scan(&s.ID, &s.StudentID, &s.Name, &s.Professors, &diff); err != nil {
			return nil, err
		}
		s.Difficulty = domain.Difficulty(diff)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) CreateExam(ctx context.Context, e domain.Exam) error {
	topics := e.Topics
	if topics == nil {
		topics = []string{}
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO exams (id, student_id, subject_id, type, starts_at, topics, status) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.ID, e.StudentID, e.SubjectID, string(e.Type), e.StartsAt, topics, string(e.Status))
	return err
}

func (r *Repository) GetExam(ctx context.Context, studentID, id string) (domain.Exam, error) {
	var e domain.Exam
	var typ, status string
	err := r.pool.QueryRow(ctx,
		`SELECT id, student_id, subject_id, type, starts_at, topics, status FROM exams WHERE id = $1 AND student_id = $2`,
		id, studentID).Scan(&e.ID, &e.StudentID, &e.SubjectID, &typ, &e.StartsAt, &e.Topics, &status)
	e.Type, e.Status = domain.ExamType(typ), domain.ExamStatus(status)
	return e, notFound(err)
}

type slotJSON struct {
	Weekday     int `json:"weekday"`
	StartMinute int `json:"startMinute"`
	EndMinute   int `json:"endMinute"`
}

func (r *Repository) SaveAvailability(ctx context.Context, a domain.Availability) error {
	slots := make([]slotJSON, len(a.Slots))
	for i, s := range a.Slots {
		slots[i] = slotJSON{int(s.Weekday), s.StartMinute, s.EndMinute}
	}
	raw, _ := json.Marshal(slots)
	tz := "UTC"
	if a.Location != nil {
		tz = a.Location.String()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO availability (student_id, slots, max_daily_minutes, preferred_session_minutes, timezone)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (student_id) DO UPDATE SET slots = EXCLUDED.slots, max_daily_minutes = EXCLUDED.max_daily_minutes,
			preferred_session_minutes = EXCLUDED.preferred_session_minutes, timezone = EXCLUDED.timezone`,
		a.StudentID, raw, a.MaxDailyMinutes, a.PreferredSessionMinutes, tz)
	return err
}

func (r *Repository) GetAvailability(ctx context.Context, studentID string) (domain.Availability, error) {
	a := domain.Availability{StudentID: studentID}
	var raw []byte
	var tz string
	err := r.pool.QueryRow(ctx,
		`SELECT slots, max_daily_minutes, preferred_session_minutes, timezone FROM availability WHERE student_id = $1`,
		studentID).Scan(&raw, &a.MaxDailyMinutes, &a.PreferredSessionMinutes, &tz)
	if err != nil {
		return a, notFound(err)
	}
	var slots []slotJSON
	if err := json.Unmarshal(raw, &slots); err != nil {
		return a, err
	}
	for _, s := range slots {
		a.Slots = append(a.Slots, domain.Slot{Weekday: time.Weekday(s.Weekday), StartMinute: s.StartMinute, EndMinute: s.EndMinute})
	}
	if a.Location, err = time.LoadLocation(tz); err != nil {
		return a, err
	}
	return a, nil
}

const sessionColumns = `id, plan_id, student_id, exam_id, starts_at, ends_at, topic, objective, status`

func scanSession(row pgx.Row) (domain.Session, error) {
	var s domain.Session
	var status string
	err := row.Scan(&s.ID, &s.PlanID, &s.StudentID, &s.ExamID, &s.Start, &s.End, &s.Topic, &s.Objective, &status)
	s.Status = domain.SessionStatus(status)
	return s, err
}

func listSessions(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, studentID string, from time.Time) ([]domain.Session, error) {
	rows, err := q.Query(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE student_id = $1 AND ends_at > $2 ORDER BY starts_at`, studentID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Session{}
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) ListBookedSessions(ctx context.Context, studentID string, from time.Time) ([]domain.Session, error) {
	return listSessions(ctx, r.pool, studentID, from)
}

func (r *Repository) SaveProposal(ctx context.Context, p domain.Plan) error {
	drafts, err := json.Marshal(p.Drafts)
	if err != nil {
		return err
	}
	warnings := p.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO plans (id, student_id, exam_id, status, generated_by, warnings, drafts, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		p.ID, p.StudentID, p.ExamID, string(p.Status), string(p.GeneratedBy), warnings, drafts, p.CreatedAt)
	return err
}

func (r *Repository) GetPlan(ctx context.Context, studentID, id string) (domain.Plan, error) {
	var p domain.Plan
	var status, by string
	var drafts []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, student_id, exam_id, status, generated_by, warnings, drafts, created_at
		FROM plans WHERE id = $1 AND student_id = $2`, id, studentID).
		Scan(&p.ID, &p.StudentID, &p.ExamID, &status, &by, &p.Warnings, &drafts, &p.CreatedAt)
	if err != nil {
		return p, notFound(err)
	}
	p.Status, p.GeneratedBy = domain.PlanStatus(status), domain.GeneratedBy(by)
	return p, json.Unmarshal(drafts, &p.Drafts)
}

// Confirm reserva las sesiones del plan en una única transacción (todo o nada).
//
//  1. Bloquea la fila del estudiante: las confirmaciones del mismo estudiante
//     se procesan de a una.
//  2. Si la clave de idempotencia ya existe, devuelve el resultado guardado (RN-14).
//  3. Verifica que el plan siga propuesto y que el examen no tenga otro plan
//     confirmado (RN-15).
//  4. Valida RN-10 a RN-13 contra las sesiones reservadas en este momento.
//  5. Inserta las sesiones, marca el plan como confirmado y guarda el resultado.
//
// Las restricciones de la base (exclusión de superposiciones e índice único de
// plan confirmado) son una segunda barrera si algo de lo anterior fallara.
func (r *Repository) Confirm(ctx context.Context, req app.ConfirmRequest, validate func([]domain.Session) error) (res app.ConfirmResult, err error) {
	ctx, span := tracer.Start(ctx, "postgres.confirm_plan")
	span.SetAttributes(attribute.String("plan.id", req.PlanID))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op si ya se hizo commit

	if _, err = tx.Exec(ctx, `INSERT INTO students (id) VALUES ($1) ON CONFLICT DO NOTHING`, req.StudentID); err != nil {
		return res, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM students WHERE id = $1 FOR UPDATE`, req.StudentID); err != nil {
		return res, err
	}

	var prevPlan string
	var prevResult []byte
	err = tx.QueryRow(ctx, `SELECT plan_id, result FROM confirmations WHERE student_id = $1 AND idempotency_key = $2`,
		req.StudentID, req.IdempotencyKey).Scan(&prevPlan, &prevResult)
	switch {
	case err == nil:
		span.SetAttributes(attribute.Bool("idempotent.replayed", true))
		if prevPlan != req.PlanID {
			return res, domain.ErrIdempotencyReused
		}
		return res, json.Unmarshal(prevResult, &res)
	case !errors.Is(err, pgx.ErrNoRows):
		return res, err
	}

	var status, examID string
	var draftsRaw []byte
	err = tx.QueryRow(ctx, `SELECT status, exam_id, drafts FROM plans WHERE id = $1 AND student_id = $2 FOR UPDATE`,
		req.PlanID, req.StudentID).Scan(&status, &examID, &draftsRaw)
	if err != nil {
		return res, notFound(err)
	}
	if domain.PlanStatus(status) != domain.PlanProposed {
		return res, domain.ErrPlanNotProposed
	}
	var confirmed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM plans WHERE exam_id = $1 AND status = 'confirmado')`, examID).Scan(&confirmed); err != nil {
		return res, err
	}
	if confirmed {
		return res, domain.ErrAlreadyConfirmed
	}

	booked, err := listSessions(ctx, tx, req.StudentID, time.Now().AddDate(0, 0, -1))
	if err != nil {
		return res, err
	}
	if err = validate(booked); err != nil {
		return res, err
	}

	var drafts []domain.SessionDraft
	if err = json.Unmarshal(draftsRaw, &drafts); err != nil {
		return res, err
	}
	res.PlanID = req.PlanID
	for _, d := range drafts {
		s := domain.Session{ID: r.ids.NewID(), PlanID: req.PlanID, StudentID: req.StudentID, ExamID: examID,
			Start: d.Start, End: d.End(), Topic: d.Topic, Objective: d.Objective, Status: domain.SessionPending}
		if _, err = tx.Exec(ctx, `INSERT INTO sessions (`+sessionColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			s.ID, s.PlanID, s.StudentID, s.ExamID, s.Start, s.End, s.Topic, s.Objective, string(s.Status)); err != nil {
			return res, translate(err)
		}
		res.Sessions = append(res.Sessions, s)
	}
	if _, err = tx.Exec(ctx, `UPDATE plans SET status = 'confirmado' WHERE id = $1`, req.PlanID); err != nil {
		return res, translate(err)
	}
	raw, _ := json.Marshal(res)
	if _, err = tx.Exec(ctx, `INSERT INTO confirmations (student_id, idempotency_key, plan_id, result) VALUES ($1, $2, $3, $4)`,
		req.StudentID, req.IdempotencyKey, req.PlanID, raw); err != nil {
		return res, translate(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return res, translate(err)
	}
	span.SetAttributes(attribute.Int("sessions.count", len(res.Sessions)))
	return res, nil
}

// translate convierte las violaciones de restricciones de la base en errores
// del dominio.
func translate(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == "23P01": // exclusion_violation
		return &domain.ValidationError{Violations: []domain.RuleViolation{{Rule: "RN-12", Message: "la sesión se superpone con otra sesión reservada", SessionIndex: -1}}}
	case pgErr.Code == "23505" && pgErr.ConstraintName == "plans_one_confirmed_per_exam":
		return domain.ErrAlreadyConfirmed
	case pgErr.Code == "23505" && pgErr.ConstraintName == "confirmations_pkey":
		return domain.ErrConfirmInProgress
	}
	return fmt.Errorf("postgres: %w", err)
}
