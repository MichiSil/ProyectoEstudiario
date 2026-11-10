package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// RF-04 contra PostgreSQL real: la edición se guarda y otro estudiante no la ve.
func TestIntegration_EditarMateria(t *testing.T) {
	svc, _, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()
	exam, err := svc.Repo.GetExam(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	name, profs, diff := "Análisis Matemático II", []string{"Dra. Pérez"}, domain.DifficultyVeryHard
	if _, err := svc.UpdateSubject(ctx, student, exam.SubjectID, app.SubjectPatch{Name: &name, Professors: &profs, Difficulty: &diff}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Repo.GetSubject(ctx, student, exam.SubjectID)
	if err != nil || got.Name != name || len(got.Professors) != 1 || got.Difficulty != diff {
		t.Fatalf("materia = %+v, err = %v", got, err)
	}
	if _, err := svc.UpdateSubject(ctx, "otro-"+student, exam.SubjectID, app.SubjectPatch{Name: &name}); err == nil {
		t.Fatal("otro estudiante pudo editarla")
	}
}

// RF-11 contra PostgreSQL real: completar, deshacer y posponer; posponer deja
// el plan desactualizado en la misma transacción y libera el horario.
func TestIntegration_MarcarSesiones(t *testing.T) {
	svc, pool, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()
	plan, err := svc.ProposePlan(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ConfirmPlan(ctx, student, plan.ID, "clave-sesiones")
	if err != nil {
		t.Fatal(err)
	}
	first, second := res.Sessions[0], res.Sessions[1]

	svc.Clock = fixedClock{first.Start.Add(time.Minute)}
	if s, err := svc.SetSessionStatus(ctx, student, first.ID, domain.SessionCompleted); err != nil || s.Status != domain.SessionCompleted {
		t.Fatalf("completar: %+v, %v", s, err)
	}
	if s, err := svc.SetSessionStatus(ctx, student, first.ID, domain.SessionPending); err != nil || s.Status != domain.SessionPending {
		t.Fatalf("deshacer: %+v, %v", s, err)
	}

	if s, err := svc.SetSessionStatus(ctx, student, second.ID, domain.SessionPostponed); err != nil || !s.Outdated {
		t.Fatalf("posponer: %+v, %v", s, err)
	}
	var planStatus string
	_ = pool.QueryRow(ctx, `SELECT status FROM plans WHERE id = $1`, plan.ID).Scan(&planStatus)
	if planStatus != "desactualizado" {
		t.Fatalf("el plan quedó %q, se esperaba desactualizado", planStatus)
	}
	// El horario quedó libre: la restricción de superposición ya no lo cuenta.
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, plan_id, student_id, exam_id, starts_at, ends_at, topic, objective, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'x', 'x', 'pendiente')`,
		"libre-"+second.ID, plan.ID, student, examID, second.Start, second.End); err != nil {
		t.Fatalf("el horario de la sesión pospuesta no quedó libre: %v", err)
	}
}

// Sección 6.3 contra PostgreSQL real: otra propuesta descarta la anterior.
func TestIntegration_OtraPropuestaDescartaLaAnterior(t *testing.T) {
	svc, pool, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()
	first, err := svc.ProposePlan(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProposePlan(ctx, student, examID); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM plans WHERE id = $1`, first.ID).Scan(&status)
	if status != "descartado" {
		t.Fatalf("la primera quedó %q", status)
	}
	if _, err := svc.ConfirmPlan(ctx, student, first.ID, "clave-vieja"); !errors.Is(err, domain.ErrPlanNotProposed) {
		t.Fatalf("confirmar la descartada: %v", err)
	}
}
