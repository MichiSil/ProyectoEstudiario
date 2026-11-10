package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// UpdateSubject en memoria, con la misma regla que PostgreSQL: sólo materias propias.
func (r *memRepo) UpdateSubject(_ context.Context, st, id string, apply func(domain.Subject) (domain.Subject, error)) (domain.Subject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.subjects[id]
	if !ok || cur.StudentID != st {
		return domain.Subject{}, domain.ErrNotFound
	}
	next, err := apply(cur)
	if err != nil {
		return domain.Subject{}, err
	}
	r.subjects[id] = next
	return next, nil
}

// UpdateSession en memoria: el mutex equivale a la transacción con el estudiante bloqueado.
func (r *memRepo) UpdateSession(_ context.Context, st, id string, apply func(domain.Session) (domain.Session, bool, error)) (domain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.sessions {
		if s.ID != id || s.StudentID != st {
			continue
		}
		next, outdate, err := apply(s)
		if err != nil {
			return domain.Session{}, err
		}
		r.sessions[i] = next
		if p := r.plans[next.PlanID]; outdate && p.Status == domain.PlanConfirmed {
			p.Status = domain.PlanOutdated
			r.plans[next.PlanID] = p
			next.Outdated = true
		}
		return next, nil
	}
	return domain.Session{}, domain.ErrNotFound
}

func rule(err error) string {
	var verr *domain.ValidationError
	if errors.As(err, &verr) && len(verr.Violations) > 0 {
		return verr.Violations[0].Rule
	}
	return ""
}

// RF-04: editar una materia propia; las validaciones son las de crearla.
func TestEditarMateria(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	name, profs := "  Análisis Matemático II ", []string{" Dra. Pérez ", "", "Lic. Gómez"}
	got, err := svc.UpdateSubject(ctx, "est-1", exam.SubjectID, app.SubjectPatch{Name: &name, Professors: &profs})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Análisis Matemático II" || len(got.Professors) != 2 || got.Professors[0] != "Dra. Pérez" {
		t.Fatalf("materia = %+v", got)
	}
	if got.Difficulty != domain.DifficultyHard {
		t.Error("cambió la dificultad sin que se la pidieran")
	}
	if repo.subjects[exam.SubjectID].Name != "Análisis Matemático II" {
		t.Error("no se guardó")
	}

	empty, bad := " ", domain.Difficulty(7)
	if _, err := svc.UpdateSubject(ctx, "est-1", exam.SubjectID, app.SubjectPatch{Name: &empty}); rule(err) != "RF-04" {
		t.Errorf("nombre vacío: %v", err)
	}
	if _, err := svc.UpdateSubject(ctx, "est-1", exam.SubjectID, app.SubjectPatch{Difficulty: &bad}); rule(err) != "RF-04" {
		t.Errorf("dificultad inválida: %v", err)
	}
	if _, err := svc.UpdateSubject(ctx, "otro-estudiante", exam.SubjectID, app.SubjectPatch{Name: &name}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("otro estudiante no puede editarla: %v", err)
	}
}

// RF-11, RN-48, RN-49: completar, deshacer y posponer una sesión.
func TestMarcarSesiones(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	res := confirmPlan(t, svc, exam.ID, "clave-1")
	first, second := res.Sessions[0], res.Sessions[1]

	// RN-48: se puede completar antes de que empiece (adelantar el estudio).
	if now.After(first.Start) {
		t.Fatal("la sesión del test tiene que ser futura")
	}
	got, err := svc.SetSessionStatus(ctx, "est-1", first.ID, domain.SessionCompleted)
	if err != nil || got.Status != domain.SessionCompleted {
		t.Fatalf("completar una sesión futura: %+v, %v", got, err)
	}
	if _, err := svc.SetSessionStatus(ctx, "est-1", first.ID, domain.SessionCompleted); err != nil {
		t.Fatalf("repetir completar tiene que ser idempotente: %v", err)
	}
	got, err = svc.SetSessionStatus(ctx, "est-1", first.ID, domain.SessionPending)
	if err != nil || got.Status != domain.SessionPending {
		t.Fatalf("deshacer: %+v, %v", got, err)
	}
	if repo.plans[first.PlanID].Status != domain.PlanConfirmed {
		t.Fatal("completar o deshacer no tiene que tocar el plan")
	}

	// Posponer libera el horario y deja el plan desactualizado.
	got, err = svc.SetSessionStatus(ctx, "est-1", second.ID, domain.SessionPostponed)
	if err != nil || got.Status != domain.SessionPostponed || !got.Outdated {
		t.Fatalf("posponer: %+v, %v", got, err)
	}
	if repo.plans[second.PlanID].Status != domain.PlanOutdated {
		t.Fatal("posponer tiene que dejar el plan desactualizado")
	}
	if _, err := svc.SetSessionStatus(ctx, "est-1", second.ID, domain.SessionPending); rule(err) != "RN-49" {
		t.Fatalf("una pospuesta no vuelve a pendiente: %v", err)
	}

	if _, err := svc.SetSessionStatus(ctx, "est-1", first.ID, "otra-cosa"); rule(err) != "RF-11" {
		t.Errorf("estado inválido: %v", err)
	}
	if _, err := svc.SetSessionStatus(ctx, "otro-estudiante", first.ID, domain.SessionCompleted); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("otro estudiante no puede tocarla: %v", err)
	}
}

// Sección 6.3: pedir otra propuesta descarta la anterior, que ya no se puede confirmar.
func TestOtraPropuestaDescartaLaAnterior(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	first, err := svc.ProposePlan(ctx, "est-1", exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ProposePlan(ctx, "est-1", exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if repo.plans[first.ID].Status != domain.PlanDiscarded || repo.plans[second.ID].Status != domain.PlanProposed {
		t.Fatalf("primera %s, segunda %s", repo.plans[first.ID].Status, repo.plans[second.ID].Status)
	}
	if _, err := svc.ConfirmPlan(ctx, "est-1", first.ID, "clave-vieja"); !errors.Is(err, domain.ErrPlanNotProposed) {
		t.Fatalf("confirmar la descartada: %v", err)
	}
	if _, err := svc.ConfirmPlan(ctx, "est-1", second.ID, "clave-nueva"); err != nil {
		t.Fatalf("confirmar la vigente: %v", err)
	}
}
