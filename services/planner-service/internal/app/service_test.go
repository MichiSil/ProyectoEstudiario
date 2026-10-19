package app_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/rules"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// memRepo es un PlanRepository en memoria para los tests de los casos de uso.
// Reproduce las garantías que en producción da PostgreSQL: confirmación
// atómica, idempotencia por clave (RN-14) y un plan confirmado por examen (RN-15).
type memRepo struct {
	mu           sync.Mutex
	subjects     map[string]domain.Subject
	exams        map[string]domain.Exam
	availability map[string]domain.Availability
	plans        map[string]domain.Plan
	sessions     []domain.Session
	idempotency  map[string]idemEntry
	nextID       int
}

type idemEntry struct {
	planID string
	result app.ConfirmResult
}

func newMemRepo() *memRepo {
	return &memRepo{
		subjects: map[string]domain.Subject{}, exams: map[string]domain.Exam{},
		availability: map[string]domain.Availability{}, plans: map[string]domain.Plan{},
		idempotency: map[string]idemEntry{},
	}
}

func (r *memRepo) CreateSubject(_ context.Context, s domain.Subject) error {
	r.subjects[s.ID] = s
	return nil
}
func (r *memRepo) GetSubject(_ context.Context, st, id string) (domain.Subject, error) {
	s, ok := r.subjects[id]
	if !ok || s.StudentID != st {
		return domain.Subject{}, domain.ErrNotFound
	}
	return s, nil
}
func (r *memRepo) ListSubjects(_ context.Context, st string) ([]domain.Subject, error) {
	var out []domain.Subject
	for _, s := range r.subjects {
		if s.StudentID == st {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *memRepo) CreateExam(_ context.Context, e domain.Exam) error { r.exams[e.ID] = e; return nil }
func (r *memRepo) GetExam(_ context.Context, st, id string) (domain.Exam, error) {
	e, ok := r.exams[id]
	if !ok || e.StudentID != st {
		return domain.Exam{}, domain.ErrNotFound
	}
	return e, nil
}
func (r *memRepo) SaveAvailability(_ context.Context, a domain.Availability) error {
	r.availability[a.StudentID] = a
	return nil
}
func (r *memRepo) GetAvailability(_ context.Context, st string) (domain.Availability, error) {
	a, ok := r.availability[st]
	if !ok {
		return domain.Availability{}, domain.ErrNotFound
	}
	return a, nil
}
func (r *memRepo) ListBookedSessions(_ context.Context, st string, _ time.Time) ([]domain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Session
	for _, s := range r.sessions {
		if s.StudentID == st {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *memRepo) SaveProposal(_ context.Context, p domain.Plan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plans[p.ID] = p
	return nil
}
func (r *memRepo) GetPlan(_ context.Context, st, id string) (domain.Plan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plans[id]
	if !ok || p.StudentID != st {
		return domain.Plan{}, domain.ErrNotFound
	}
	return p, nil
}
func (r *memRepo) Confirm(_ context.Context, req app.ConfirmRequest, validate func([]domain.Session) error) (app.ConfirmResult, error) {
	r.mu.Lock() // equivale a la transacción con las filas del estudiante bloqueadas
	defer r.mu.Unlock()
	key := req.StudentID + "|" + req.IdempotencyKey
	if prev, ok := r.idempotency[key]; ok {
		if prev.planID != req.PlanID {
			return app.ConfirmResult{}, domain.ErrIdempotencyReused
		}
		return prev.result, nil
	}
	plan := r.plans[req.PlanID]
	if plan.Status != domain.PlanProposed {
		return app.ConfirmResult{}, domain.ErrPlanNotProposed
	}
	for _, p := range r.plans {
		if p.ExamID == plan.ExamID && p.Status == domain.PlanConfirmed {
			return app.ConfirmResult{}, domain.ErrAlreadyConfirmed
		}
	}
	var booked []domain.Session
	for _, s := range r.sessions {
		if s.StudentID == req.StudentID {
			booked = append(booked, s)
		}
	}
	if err := validate(booked); err != nil {
		return app.ConfirmResult{}, err
	}
	res := app.ConfirmResult{PlanID: plan.ID}
	for _, d := range plan.Drafts {
		r.nextID++
		s := domain.Session{ID: fmt.Sprintf("ses-%d", r.nextID), PlanID: plan.ID, StudentID: req.StudentID, ExamID: plan.ExamID,
			Start: d.Start, End: d.End(), Topic: d.Topic, Objective: d.Objective, Status: domain.SessionPending}
		r.sessions = append(r.sessions, s)
		res.Sessions = append(res.Sessions, s)
	}
	plan.Status = domain.PlanConfirmed
	r.plans[plan.ID] = plan
	r.idempotency[key] = idemEntry{plan.ID, res}
	return res, nil
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (g *seqIDs) NewID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("id-%d", g.n)
}

var cba, _ = time.LoadLocation("America/Argentina/Cordoba")
var now = time.Date(2026, 10, 19, 10, 0, 0, 0, cba)

// setup crea un estudiante con una materia, un examen dentro de 14 días y
// disponibilidad de 18 a 21 todos los días.
func setup(t *testing.T) (*app.Service, *memRepo, domain.Exam) {
	t.Helper()
	repo := newMemRepo()
	svc := &app.Service{Repo: repo, Generator: rules.Engine{}, Clock: fixedClock{now}, IDs: &seqIDs{}}
	ctx := context.Background()

	sub, err := svc.CreateSubject(ctx, domain.Subject{StudentID: "est-1", Name: "Análisis II", Difficulty: domain.DifficultyHard})
	if err != nil {
		t.Fatal(err)
	}
	exam, err := svc.CreateExam(ctx, domain.Exam{StudentID: "est-1", SubjectID: sub.ID, Type: domain.ExamParcial,
		StartsAt: now.AddDate(0, 0, 14), Topics: []string{"Integrales", "Series"}})
	if err != nil {
		t.Fatal(err)
	}
	var slots []domain.Slot
	for d := time.Sunday; d <= time.Saturday; d++ {
		slots = append(slots, domain.Slot{Weekday: d, StartMinute: 18 * 60, EndMinute: 21 * 60})
	}
	if err := svc.SetAvailability(ctx, domain.Availability{StudentID: "est-1", Slots: slots, MaxDailyMinutes: 120, PreferredSessionMinutes: 60, Location: cba}); err != nil {
		t.Fatal(err)
	}
	return svc, repo, exam
}

// RN-08: la propuesta no reserva nada hasta que se confirma.
func TestProposePlan_NoReservaSesiones(t *testing.T) {
	svc, repo, exam := setup(t)
	plan, err := svc.ProposePlan(context.Background(), "est-1", exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != domain.PlanProposed || len(plan.Drafts) == 0 {
		t.Fatalf("se esperaba una propuesta con sesiones, llegó %+v", plan)
	}
	if len(repo.sessions) != 0 {
		t.Fatal("la propuesta no debería reservar sesiones")
	}
}

// CA-06: confirmar una propuesta válida reserva todas sus sesiones.
func TestConfirmPlan_ReservaSesiones(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	plan, _ := svc.ProposePlan(ctx, "est-1", exam.ID)
	res, err := svc.ConfirmPlan(ctx, "est-1", plan.ID, "clave-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sessions) != len(plan.Drafts) || len(repo.sessions) != len(plan.Drafts) {
		t.Fatalf("se esperaban %d sesiones reservadas, hay %d", len(plan.Drafts), len(repo.sessions))
	}
	if repo.plans[plan.ID].Status != domain.PlanConfirmed {
		t.Fatal("el plan debería quedar confirmado")
	}
}

// CA-10: un reintento con la misma clave devuelve el mismo resultado y no duplica.
func TestConfirmPlan_ReintentoIdempotente(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	plan, _ := svc.ProposePlan(ctx, "est-1", exam.ID)
	first, err := svc.ConfirmPlan(ctx, "est-1", plan.ID, "clave-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ConfirmPlan(ctx, "est-1", plan.ID, "clave-1")
	if err != nil {
		t.Fatalf("el reintento no debería fallar: %v", err)
	}
	if len(repo.sessions) != len(first.Sessions) || second.Sessions[0].ID != first.Sessions[0].ID {
		t.Fatal("el reintento duplicó sesiones o devolvió otro resultado")
	}
}

// CA-11: dos confirmaciones simultáneas del mismo plan (dos dispositivos, con
// claves distintas) reservan un único conjunto de sesiones.
func TestConfirmPlan_ConcurrenciaDesdeDosDispositivos(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	plan, _ := svc.ProposePlan(ctx, "est-1", exam.ID)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.ConfirmPlan(ctx, "est-1", plan.ID, fmt.Sprintf("dispositivo-%d", i))
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if !errors.Is(err, domain.ErrPlanNotProposed) {
			t.Fatalf("error inesperado: %v", err)
		}
	}
	if ok != 1 || len(repo.sessions) != len(plan.Drafts) {
		t.Fatalf("se esperaba una sola confirmación exitosa y %d sesiones, hubo %d y %d", len(plan.Drafts), ok, len(repo.sessions))
	}
}

// RN-15: un examen tiene como máximo un plan confirmado vigente.
func TestConfirmPlan_SegundoPlanDelMismoExamen(t *testing.T) {
	svc, _, exam := setup(t)
	ctx := context.Background()
	p1, _ := svc.ProposePlan(ctx, "est-1", exam.ID)
	if _, err := svc.ConfirmPlan(ctx, "est-1", p1.ID, "clave-1"); err != nil {
		t.Fatal(err)
	}
	p2, _ := svc.ProposePlan(ctx, "est-1", exam.ID)
	if len(p2.Drafts) == 0 {
		t.Skip("no quedó lugar para una segunda propuesta")
	}
	if _, err := svc.ConfirmPlan(ctx, "est-1", p2.ID, "clave-2"); !errors.Is(err, domain.ErrAlreadyConfirmed) {
		t.Fatalf("se esperaba ErrAlreadyConfirmed, llegó %v", err)
	}
}

func TestConfirmPlan_SinClaveDeIdempotencia(t *testing.T) {
	svc, _, exam := setup(t)
	plan, _ := svc.ProposePlan(context.Background(), "est-1", exam.ID)
	var verr *domain.ValidationError
	if _, err := svc.ConfirmPlan(context.Background(), "est-1", plan.ID, ""); !errors.As(err, &verr) {
		t.Fatalf("se esperaba error de validación por falta de Idempotency-Key, llegó %v", err)
	}
}

// CA-02: un estudiante no puede usar datos de otro.
func TestProposePlan_ExamenDeOtroEstudiante(t *testing.T) {
	svc, _, exam := setup(t)
	if _, err := svc.ProposePlan(context.Background(), "est-2", exam.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("se esperaba ErrNotFound, llegó %v", err)
	}
}

// RN-04: no se puede cargar un examen en el pasado.
func TestCreateExam_EnElPasado(t *testing.T) {
	svc, repo, _ := setup(t)
	var subID string
	for id := range repo.subjects {
		subID = id
	}
	_, err := svc.CreateExam(context.Background(), domain.Exam{StudentID: "est-1", SubjectID: subID, Type: domain.ExamFinal, StartsAt: now.AddDate(0, 0, -1)})
	var verr *domain.ValidationError
	if !errors.As(err, &verr) || verr.Violations[0].Rule != "RN-04" {
		t.Fatalf("se esperaba RN-04, llegó %v", err)
	}
}
