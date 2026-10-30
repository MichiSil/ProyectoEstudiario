package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// RN-16: el plan queda desactualizado si cambia algo con lo que se armó.
func TestUpdateExam_CambiosQueDesactualizanElPlan(t *testing.T) {
	topics := []string{"Integrales", "Series", "Sucesiones"}
	modality := domain.ModalityPractice
	casos := map[string]app.ExamChanges{
		"temas":     {Topics: &topics},
		"modalidad": {Modality: &modality},
	}
	for nombre, cambio := range casos {
		t.Run(nombre, func(t *testing.T) {
			svc, _, exam := setup(t)
			confirmPlan(t, svc, exam.ID, "clave-1")
			got, err := svc.UpdateExam(context.Background(), "est-1", exam.ID, cambio)
			if err != nil {
				t.Fatal(err)
			}
			if got.PlanStatus != domain.PlanOutdated {
				t.Fatalf("cambiando %s el plan tendría que quedar desactualizado, quedó %q", nombre, got.PlanStatus)
			}
		})
	}
}

// Rehacer el plan a pedido: queda desactualizado y el plan nuevo reemplaza
// sus sesiones pendientes, como cuando cambia la fecha.
func TestReplan_RehacerAPedido(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()

	if _, err := svc.Replan(ctx, "est-1", exam.ID); !errors.Is(err, domain.ErrNoConfirmedPlan) {
		t.Fatalf("sin plan confirmado: se esperaba ErrNoConfirmedPlan, llegó %v", err)
	}
	old := confirmPlan(t, svc, exam.ID, "clave-1")
	got, err := svc.Replan(ctx, "est-1", exam.ID)
	if err != nil || got.PlanStatus != domain.PlanOutdated {
		t.Fatalf("rehacer: estado %q, error %v", got.PlanStatus, err)
	}
	// Hasta confirmar el nuevo, las sesiones del plan anterior siguen.
	if sessions, _ := svc.ListSessions(ctx, "est-1"); len(sessions) != len(old.Sessions) {
		t.Fatalf("antes de confirmar el nuevo plan tienen que seguir las %d sesiones, hay %d", len(old.Sessions), len(sessions))
	}
	res := confirmPlan(t, svc, exam.ID, "clave-2")
	for _, s := range repo.sessions {
		if s.PlanID == old.PlanID {
			t.Fatal("quedó una sesión del plan anterior")
		}
	}
	if len(repo.sessions) != len(res.Sessions) {
		t.Fatalf("tendrían que quedar las %d sesiones del plan nuevo, hay %d", len(res.Sessions), len(repo.sessions))
	}

	if _, err := svc.CancelExam(ctx, "est-1", exam.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Replan(ctx, "est-1", exam.ID); !errors.Is(err, domain.ErrExamCancelled) {
		t.Fatalf("examen cancelado: se esperaba ErrExamCancelled, llegó %v", err)
	}
}
