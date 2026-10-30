package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// CA-12 y RN-16 contra PostgreSQL: cambiar la fecha desactualiza el plan,
// las sesiones siguen visibles marcadas y al replanificar se reemplazan.
func TestIntegration_CambioDeFechaYReplanificacion(t *testing.T) {
	svc, pool, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()

	plan, err := svc.ProposePlan(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	old, err := svc.ConfirmPlan(ctx, student, plan.ID, "clave-1")
	if err != nil {
		t.Fatal(err)
	}
	exam, err := svc.Repo.GetExam(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	newDate := exam.StartsAt.AddDate(0, 0, 7)
	updated, err := svc.UpdateExam(ctx, student, examID, app.ExamChanges{StartsAt: &newDate})
	if err != nil {
		t.Fatal(err)
	}
	if updated.PlanStatus != domain.PlanOutdated || !updated.StartsAt.Equal(newDate) {
		t.Fatalf("se esperaba el plan desactualizado y la fecha nueva, llegó %+v", updated)
	}
	exams, err := svc.ListExams(ctx, student)
	if err != nil || len(exams) != 1 || exams[0].PlanStatus != domain.PlanOutdated {
		t.Fatalf("GET /exams tendría que mostrar el plan desactualizado: %+v %v", exams, err)
	}
	sessions, err := svc.ListSessions(ctx, student)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != len(old.Sessions) {
		t.Fatalf("las sesiones tienen que seguir visibles: hay %d de %d", len(sessions), len(old.Sessions))
	}
	for _, s := range sessions {
		if !s.Outdated {
			t.Fatal("las sesiones del plan desactualizado tienen que venir marcadas")
		}
	}

	// Replanificar: la propuesta nueva puede usar los horarios de las viejas y
	// al confirmar las reemplaza (si no, chocaría con la restricción de superposición).
	plan2, err := svc.ProposePlan(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ConfirmPlan(ctx, student, plan2.ID, "clave-2")
	if err != nil {
		t.Fatalf("replanificar no debería fallar: %v", err)
	}
	if got := countSessions(t, pool, student); got != len(res.Sessions) {
		t.Fatalf("tendrían que quedar sólo las %d sesiones del plan nuevo, hay %d", len(res.Sessions), got)
	}
}

// CA-13 y RN-17 contra PostgreSQL: cancelar libera las pendientes, conserva
// las completadas y deja el examen bloqueado.
func TestIntegration_CancelarExamen(t *testing.T) {
	svc, pool, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()

	plan, err := svc.ProposePlan(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ConfirmPlan(ctx, student, plan.ID, "clave-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sessions) < 2 {
		t.Skip("hacen falta al menos dos sesiones")
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET status = 'completada' WHERE id = $1`, res.Sessions[0].ID); err != nil {
		t.Fatal(err)
	}

	exam, err := svc.CancelExam(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	if exam.Status != domain.ExamCancelled {
		t.Fatalf("el examen tendría que quedar cancelado, quedó %q", exam.Status)
	}
	if got := countSessions(t, pool, student); got != 1 {
		t.Fatalf("tendría que quedar sólo la sesión completada, hay %d", got)
	}
	var planStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM plans WHERE id = $1`, plan.ID).Scan(&planStatus); err != nil || planStatus != "cancelado" {
		t.Fatalf("el plan tendría que quedar cancelado, quedó %q (%v)", planStatus, err)
	}
	if _, err := svc.CancelExam(ctx, student, examID); err != nil {
		t.Fatalf("cancelar de nuevo no tendría que fallar: %v", err)
	}
	if _, err := svc.ProposePlan(ctx, student, examID); !errors.Is(err, domain.ErrExamCancelled) {
		t.Fatalf("se esperaba ErrExamCancelled al planificar, llegó %v", err)
	}
}
