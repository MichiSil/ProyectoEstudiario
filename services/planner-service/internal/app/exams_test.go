package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// ---------- memRepo: exámenes ----------

func (r *memRepo) ListExams(_ context.Context, st string) ([]domain.Exam, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Exam
	for _, e := range r.exams {
		if e.StudentID == st {
			e.PlanStatus = r.planStatus(e.ID)
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *memRepo) UpdateExam(_ context.Context, st, id string, apply func(domain.Exam) (domain.Exam, bool, error)) (domain.Exam, error) {
	r.mu.Lock() // equivale a la transacción con las filas del estudiante bloqueadas
	defer r.mu.Unlock()
	e, ok := r.exams[id]
	if !ok || e.StudentID != st {
		return domain.Exam{}, domain.ErrNotFound
	}
	next, outdate, err := apply(e)
	if err != nil {
		return domain.Exam{}, err
	}
	r.exams[id] = next
	if outdate {
		for pid, p := range r.plans {
			if p.ExamID == id && p.Status == domain.PlanConfirmed {
				p.Status = domain.PlanOutdated
				r.plans[pid] = p
			}
		}
	}
	next.PlanStatus = r.planStatus(id)
	return next, nil
}

func (r *memRepo) CancelExam(_ context.Context, st, id string) (domain.Exam, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.exams[id]
	if !ok || e.StudentID != st {
		return domain.Exam{}, domain.ErrNotFound
	}
	if e.Status == domain.ExamCancelled {
		return e, nil
	}
	e.Status = domain.ExamCancelled
	r.exams[id] = e
	r.removeSessions(func(s domain.Session) bool { return s.ExamID == id && s.Status != domain.SessionCompleted })
	for pid, p := range r.plans {
		if p.ExamID == id && p.Status != domain.PlanDiscarded {
			p.Status = domain.PlanCancelled
			r.plans[pid] = p
		}
	}
	return e, nil
}

// planStatus devuelve el estado del plan vigente del examen (confirmado o desactualizado).
func (r *memRepo) planStatus(examID string) domain.PlanStatus {
	var status domain.PlanStatus
	for _, p := range r.plans {
		if p.ExamID != examID {
			continue
		}
		if p.Status == domain.PlanConfirmed {
			return p.Status
		}
		if p.Status == domain.PlanOutdated {
			status = p.Status
		}
	}
	return status
}

func (r *memRepo) removeSessions(match func(domain.Session) bool) {
	kept := r.sessions[:0]
	for _, s := range r.sessions {
		if !match(s) {
			kept = append(kept, s)
		}
	}
	r.sessions = kept
}

// ---------- tests ----------

// confirmPlan pide una propuesta para el examen y la confirma.
func confirmPlan(t *testing.T, svc *app.Service, examID, key string) app.ConfirmResult {
	t.Helper()
	ctx := context.Background()
	plan, err := svc.ProposePlan(ctx, "est-1", examID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ConfirmPlan(ctx, "est-1", plan.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// RF-06: se pueden editar el tipo y los temas sin tocar el plan confirmado.
func TestUpdateExam_TemasSinCambiarFecha(t *testing.T) {
	svc, repo, exam := setup(t)
	confirmPlan(t, svc, exam.ID, "clave-1")

	typ, topics := domain.ExamRecuperatorio, []string{"Series"}
	got, err := svc.UpdateExam(context.Background(), "est-1", exam.ID, app.ExamChanges{Type: &typ, Topics: &topics})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != typ || len(got.Topics) != 1 || got.PlanStatus != domain.PlanConfirmed {
		t.Fatalf("el examen no quedó como se esperaba: %+v", got)
	}
	for _, s := range repo.sessions {
		if repo.plans[s.PlanID].Status != domain.PlanConfirmed {
			t.Fatal("sin cambio de fecha el plan tiene que seguir confirmado")
		}
	}
}

// CA-12 / RN-16: si cambia la fecha de un examen con plan confirmado, el plan
// pasa a desactualizado y sus sesiones pendientes siguen visibles, marcadas.
func TestUpdateExam_CambioDeFechaDesactualizaElPlan(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	res := confirmPlan(t, svc, exam.ID, "clave-1")

	newDate := exam.StartsAt.AddDate(0, 0, 7)
	got, err := svc.UpdateExam(ctx, "est-1", exam.ID, app.ExamChanges{StartsAt: &newDate})
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanStatus != domain.PlanOutdated || repo.plans[res.PlanID].Status != domain.PlanOutdated {
		t.Fatalf("el plan tendría que quedar desactualizado, quedó %q", got.PlanStatus)
	}
	sessions, _ := svc.ListSessions(ctx, "est-1")
	if len(sessions) != len(res.Sessions) {
		t.Fatalf("las sesiones pendientes tienen que seguir visibles: hay %d de %d", len(sessions), len(res.Sessions))
	}
	for _, s := range sessions {
		if !s.Outdated {
			t.Fatal("las sesiones del plan desactualizado tienen que venir marcadas")
		}
	}
}

// RN-16: al replanificar, el plan nuevo reemplaza las sesiones desactualizadas.
func TestUpdateExam_ReplanificarReemplazaLasSesiones(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	old := confirmPlan(t, svc, exam.ID, "clave-1")
	newDate := exam.StartsAt.AddDate(0, 0, 7)
	if _, err := svc.UpdateExam(ctx, "est-1", exam.ID, app.ExamChanges{StartsAt: &newDate}); err != nil {
		t.Fatal(err)
	}

	res := confirmPlan(t, svc, exam.ID, "clave-2")
	if len(repo.sessions) != len(res.Sessions) {
		t.Fatalf("tendrían que quedar sólo las %d sesiones del plan nuevo, hay %d", len(res.Sessions), len(repo.sessions))
	}
	for _, s := range repo.sessions {
		if s.PlanID == old.PlanID {
			t.Fatal("quedó una sesión del plan desactualizado")
		}
	}
}

// RN-04: la nueva fecha tampoco puede estar en el pasado.
func TestUpdateExam_FechaEnElPasado(t *testing.T) {
	svc, _, exam := setup(t)
	past := now.AddDate(0, 0, -1)
	_, err := svc.UpdateExam(context.Background(), "est-1", exam.ID, app.ExamChanges{StartsAt: &past})
	var verr *domain.ValidationError
	if !errors.As(err, &verr) || verr.Violations[0].Rule != "RN-04" {
		t.Fatalf("se esperaba RN-04, llegó %v", err)
	}
}

// CA-02: un estudiante no puede editar ni cancelar exámenes de otro.
func TestUpdateExam_DeOtroEstudiante(t *testing.T) {
	svc, _, exam := setup(t)
	topics := []string{"x"}
	if _, err := svc.UpdateExam(context.Background(), "est-2", exam.ID, app.ExamChanges{Topics: &topics}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("se esperaba ErrNotFound al editar, llegó %v", err)
	}
	if _, err := svc.CancelExam(context.Background(), "est-2", exam.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("se esperaba ErrNotFound al cancelar, llegó %v", err)
	}
}

// CA-13 / RN-17: al cancelar, las sesiones pendientes se liberan y las
// completadas se conservan como historial.
func TestCancelExam_LiberaPendientesYConservaCompletadas(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	res := confirmPlan(t, svc, exam.ID, "clave-1")
	if len(res.Sessions) < 2 {
		t.Skip("hacen falta al menos dos sesiones")
	}
	repo.sessions[0].Status = domain.SessionCompleted
	completed := repo.sessions[0].ID

	got, err := svc.CancelExam(ctx, "est-1", exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.ExamCancelled || repo.plans[res.PlanID].Status != domain.PlanCancelled {
		t.Fatalf("el examen y su plan tendrían que quedar cancelados: %+v", got)
	}
	if len(repo.sessions) != 1 || repo.sessions[0].ID != completed {
		t.Fatalf("tendría que quedar sólo la sesión completada, quedaron %d", len(repo.sessions))
	}
}

// Cancelar dos veces no falla, y un examen cancelado no se edita ni se planifica.
func TestCancelExam_EsIdempotenteYBloqueaElExamen(t *testing.T) {
	svc, _, exam := setup(t)
	ctx := context.Background()
	if _, err := svc.CancelExam(ctx, "est-1", exam.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelExam(ctx, "est-1", exam.ID); err != nil {
		t.Fatalf("cancelar de nuevo no tendría que fallar: %v", err)
	}
	topics := []string{"x"}
	if _, err := svc.UpdateExam(ctx, "est-1", exam.ID, app.ExamChanges{Topics: &topics}); !errors.Is(err, domain.ErrExamCancelled) {
		t.Fatalf("se esperaba ErrExamCancelled al editar, llegó %v", err)
	}
	if _, err := svc.ProposePlan(ctx, "est-1", exam.ID); !errors.Is(err, domain.ErrExamCancelled) {
		t.Fatalf("se esperaba ErrExamCancelled al planificar, llegó %v", err)
	}
}

// Una propuesta pedida antes de cancelar ya no se puede confirmar.
func TestCancelExam_PropuestaPreviaNoSeConfirma(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	plan, err := svc.ProposePlan(ctx, "est-1", exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelExam(ctx, "est-1", exam.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmPlan(ctx, "est-1", plan.ID, "clave-1"); !errors.Is(err, domain.ErrExamCancelled) {
		t.Fatalf("se esperaba ErrExamCancelled, llegó %v", err)
	}
	if len(repo.sessions) != 0 {
		t.Fatal("no se tendría que haber reservado ninguna sesión")
	}
}
