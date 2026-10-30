package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// DefaultTimeout es el tiempo máximo de cada operación contra PostgreSQL
// (ADR-010). La confirmación del plan es una sola operación: su transacción
// completa tiene que terminar dentro de este tiempo.
const DefaultTimeout = 3 * time.Second

// Guarded envuelve al repositorio con dos protecciones (ADR-010):
//
//   - cada operación tiene un tiempo máximo, así una base lenta no deja
//     pedidos esperando indefinidamente;
//   - si la base no responde (caída, sin conexión o timeout), el error se
//     traduce a domain.ErrUnavailable, que la API informa como 503.
type Guarded struct {
	Repo    *Repository
	Timeout time.Duration
}

var _ app.PlanRepository = Guarded{}

func (g Guarded) run(ctx context.Context, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, g.Timeout)
	defer cancel()
	return classify(fn(ctx))
}

// classify distingue los errores de disponibilidad de la base (se reintentan
// más tarde) de los errores del pedido o del dominio (se devuelven tal cual).
func classify(err error) error {
	if err == nil {
		return nil
	}
	var connErr *pgconn.ConnectError
	var netErr net.Error
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, context.DeadlineExceeded), pgconn.Timeout(err):
		return fmt.Errorf("%w: la base no respondió a tiempo: %v", domain.ErrUnavailable, err)
	case errors.As(err, &connErr), errors.As(err, &netErr), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return fmt.Errorf("%w: no se pudo conectar con la base: %v", domain.ErrUnavailable, err)
	case errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "57") || strings.HasPrefix(pgErr.Code, "08")):
		// Clase 57: la base se está apagando o reiniciando (por ejemplo 57P01).
		// Clase 08: error de conexión.
		return fmt.Errorf("%w: la base no está disponible: %v", domain.ErrUnavailable, err)
	}
	return err
}

func (g Guarded) CreateSubject(ctx context.Context, s domain.Subject) error {
	return g.run(ctx, func(ctx context.Context) error { return g.Repo.CreateSubject(ctx, s) })
}

func (g Guarded) GetSubject(ctx context.Context, studentID, id string) (s domain.Subject, err error) {
	err = g.run(ctx, func(ctx context.Context) error { s, err = g.Repo.GetSubject(ctx, studentID, id); return err })
	return s, err
}

func (g Guarded) ListSubjects(ctx context.Context, studentID string) (out []domain.Subject, err error) {
	err = g.run(ctx, func(ctx context.Context) error { out, err = g.Repo.ListSubjects(ctx, studentID); return err })
	return out, err
}

func (g Guarded) CreateExam(ctx context.Context, e domain.Exam) error {
	return g.run(ctx, func(ctx context.Context) error { return g.Repo.CreateExam(ctx, e) })
}

func (g Guarded) GetExam(ctx context.Context, studentID, id string) (e domain.Exam, err error) {
	err = g.run(ctx, func(ctx context.Context) error { e, err = g.Repo.GetExam(ctx, studentID, id); return err })
	return e, err
}

func (g Guarded) ListExams(ctx context.Context, studentID string) (out []domain.Exam, err error) {
	err = g.run(ctx, func(ctx context.Context) error { out, err = g.Repo.ListExams(ctx, studentID); return err })
	return out, err
}

func (g Guarded) UpdateExam(ctx context.Context, studentID, id string, apply func(domain.Exam) (domain.Exam, bool, error)) (e domain.Exam, err error) {
	err = g.run(ctx, func(ctx context.Context) error { e, err = g.Repo.UpdateExam(ctx, studentID, id, apply); return err })
	return e, err
}

func (g Guarded) CancelExam(ctx context.Context, studentID, id string) (e domain.Exam, err error) {
	err = g.run(ctx, func(ctx context.Context) error { e, err = g.Repo.CancelExam(ctx, studentID, id); return err })
	return e, err
}

func (g Guarded) SaveAvailability(ctx context.Context, a domain.Availability) error {
	return g.run(ctx, func(ctx context.Context) error { return g.Repo.SaveAvailability(ctx, a) })
}

func (g Guarded) GetAvailability(ctx context.Context, studentID string) (a domain.Availability, err error) {
	err = g.run(ctx, func(ctx context.Context) error { a, err = g.Repo.GetAvailability(ctx, studentID); return err })
	return a, err
}

func (g Guarded) ListBookedSessions(ctx context.Context, studentID string, from time.Time) (out []domain.Session, err error) {
	err = g.run(ctx, func(ctx context.Context) error {
		out, err = g.Repo.ListBookedSessions(ctx, studentID, from)
		return err
	})
	return out, err
}

func (g Guarded) SaveProposal(ctx context.Context, p domain.Plan) error {
	return g.run(ctx, func(ctx context.Context) error { return g.Repo.SaveProposal(ctx, p) })
}

func (g Guarded) GetPlan(ctx context.Context, studentID, id string) (p domain.Plan, err error) {
	err = g.run(ctx, func(ctx context.Context) error { p, err = g.Repo.GetPlan(ctx, studentID, id); return err })
	return p, err
}

func (g Guarded) Confirm(ctx context.Context, req app.ConfirmRequest, validate func([]domain.Session) error) (res app.ConfirmResult, err error) {
	err = g.run(ctx, func(ctx context.Context) error { res, err = g.Repo.Confirm(ctx, req, validate); return err })
	return res, err
}
