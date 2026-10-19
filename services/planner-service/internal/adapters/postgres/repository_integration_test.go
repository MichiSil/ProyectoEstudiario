package postgres_test

// Tests de integración contra un PostgreSQL real (requisito 2.11 del
// enunciado). Se ejecutan sólo si está definida PLANNER_TEST_DATABASE_URL:
//
//	docker compose up -d postgres
//	PLANNER_TEST_DATABASE_URL=postgres://planner:planner@localhost:5432/planner go test ./internal/adapters/postgres/

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/postgres"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/rules"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/system"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

func newService(t *testing.T) (*app.Service, *pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("PLANNER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PLANNER_TEST_DATABASE_URL no definida: se omiten los tests de integración")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	repo := postgres.New(pool, system.IDs{})
	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("migrar: %v", err)
	}
	svc := &app.Service{Repo: repo, Generator: rules.Engine{}, Clock: system.Clock{}, IDs: system.IDs{}}

	// Cada test usa un estudiante nuevo para no depender de los datos de otros.
	student := "test-" + system.IDs{}.NewID()
	cba, _ := time.LoadLocation("America/Argentina/Cordoba")
	sub, err := svc.CreateSubject(ctx, domain.Subject{StudentID: student, Name: "Análisis II", Difficulty: domain.DifficultyHard})
	if err != nil {
		t.Fatal(err)
	}
	var slots []domain.Slot
	for d := time.Sunday; d <= time.Saturday; d++ {
		slots = append(slots, domain.Slot{Weekday: d, StartMinute: 7 * 60, EndMinute: 23 * 60})
	}
	if err := svc.SetAvailability(ctx, domain.Availability{StudentID: student, Slots: slots, MaxDailyMinutes: 120, PreferredSessionMinutes: 60, Location: cba}); err != nil {
		t.Fatal(err)
	}
	exam, err := svc.CreateExam(ctx, domain.Exam{StudentID: student, SubjectID: sub.ID, Type: domain.ExamParcial,
		StartsAt: time.Now().In(cba).AddDate(0, 0, 14), Topics: []string{"Integrales", "Series"}})
	if err != nil {
		t.Fatal(err)
	}
	return svc, pool, student + "|" + exam.ID
}

func split(s string) (student, exam string) {
	for i := range s {
		if s[i] == '|' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

func countSessions(t *testing.T, pool *pgxpool.Pool, student string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE student_id = $1`, student).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// CA-06 y CA-10: confirmar reserva las sesiones; repetir con la misma clave no duplica.
func TestIntegration_ConfirmarYReintentar(t *testing.T) {
	svc, pool, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()

	plan, err := svc.ProposePlan(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.ConfirmPlan(ctx, student, plan.ID, "clave-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := countSessions(t, pool, student); got != len(plan.Drafts) {
		t.Fatalf("se esperaban %d sesiones en la base, hay %d", len(plan.Drafts), got)
	}
	second, err := svc.ConfirmPlan(ctx, student, plan.ID, "clave-1")
	if err != nil {
		t.Fatalf("el reintento no debería fallar: %v", err)
	}
	if second.Sessions[0].ID != first.Sessions[0].ID || countSessions(t, pool, student) != len(plan.Drafts) {
		t.Fatal("el reintento duplicó sesiones o devolvió otro resultado")
	}
}

// CA-11: confirmaciones simultáneas del mismo plan reservan un único conjunto.
func TestIntegration_ConfirmacionesConcurrentes(t *testing.T) {
	svc, pool, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()
	plan, err := svc.ProposePlan(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.ConfirmPlan(ctx, student, plan.ID, fmt.Sprintf("dispositivo-%d", i))
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
	if ok != 1 || countSessions(t, pool, student) != len(plan.Drafts) {
		t.Fatalf("se esperaba 1 confirmación y %d sesiones; hubo %d y %d", len(plan.Drafts), ok, countSessions(t, pool, student))
	}
}

// RN-12: aunque la aplicación no validara, la base impide sesiones superpuestas.
func TestIntegration_RestriccionDeSuperposicion(t *testing.T) {
	_, pool, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO plans (id, student_id, exam_id, status, generated_by, drafts, created_at)
		VALUES ($1, $2, $3, 'confirmado', 'reglas', '[]', now())`, "plan-"+student, student, examID); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(48 * time.Hour)
	insert := func(id string, from time.Time) error {
		_, err := pool.Exec(ctx, `INSERT INTO sessions (id, plan_id, student_id, exam_id, starts_at, ends_at, topic, objective, status)
			VALUES ($1, $2, $3, $4, $5, $6, 't', 'o', 'pendiente')`, id, "plan-"+student, student, examID, from, from.Add(time.Hour))
		return err
	}
	if err := insert("s1-"+student, start); err != nil {
		t.Fatal(err)
	}
	if err := insert("s2-"+student, start.Add(30*time.Minute)); err == nil {
		t.Fatal("la base debería rechazar una sesión superpuesta")
	}
}
