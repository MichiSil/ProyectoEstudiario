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

// RF-04: borrar una materia cancela sus exámenes y planes y libera las
// sesiones pendientes; las completadas se conservan en la base, pero la
// materia deja de verse con todo lo suyo.
func TestIntegration_BorrarMateria(t *testing.T) {
	svc, pool, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()

	plan, err := svc.ProposePlan(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ConfirmPlan(ctx, student, plan.ID, "clave-borrar")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET status = 'completada' WHERE id = $1`, res.Sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	exam, err := svc.Repo.GetExam(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteSubject(ctx, student, exam.SubjectID); err != nil {
		t.Fatalf("borrar la materia: %v", err)
	}
	subjects, _ := svc.ListSubjects(ctx, student)
	exams, _ := svc.ListExams(ctx, student)
	sessions, _ := svc.ListSessions(ctx, student)
	if len(subjects) != 0 || len(exams) != 0 || len(sessions) != 0 {
		t.Errorf("después de borrar se ven %d materias, %d exámenes y %d sesiones", len(subjects), len(exams), len(sessions))
	}
	if got := countSessions(t, pool, student); got != 1 {
		t.Errorf("en la base tienen que quedar sólo la sesión completada, quedan %d", got)
	}
	var planStatus, examStatus string
	_ = pool.QueryRow(ctx, `SELECT status FROM plans WHERE id = $1`, plan.ID).Scan(&planStatus)
	_ = pool.QueryRow(ctx, `SELECT status FROM exams WHERE id = $1`, examID).Scan(&examStatus)
	if planStatus != "cancelado" || examStatus != "cancelado" {
		t.Errorf("plan %q y examen %q, se esperaban cancelados", planStatus, examStatus)
	}
	if err := svc.DeleteSubject(ctx, student, exam.SubjectID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("borrar dos veces: se esperaba ErrNotFound, llegó %v", err)
	}
}

// RN-16: cambiar los temas desactualiza el plan, y rehacerlo a pedido también;
// el plan nuevo reemplaza las sesiones pendientes del anterior.
func TestIntegration_RehacerPlan(t *testing.T) {
	svc, pool, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()

	if _, err := svc.Replan(ctx, student, examID); !errors.Is(err, domain.ErrNoConfirmedPlan) {
		t.Fatalf("sin plan: se esperaba ErrNoConfirmedPlan, llegó %v", err)
	}
	first, _ := svc.ProposePlan(ctx, student, examID)
	if _, err := svc.ConfirmPlan(ctx, student, first.ID, "clave-1"); err != nil {
		t.Fatal(err)
	}
	topics := []string{"Integrales", "Series", "Ecuaciones diferenciales"}
	e, err := svc.UpdateExam(ctx, student, examID, app.ExamChanges{Topics: &topics})
	if err != nil || e.PlanStatus != domain.PlanOutdated {
		t.Fatalf("cambiar los temas: estado %q, error %v", e.PlanStatus, err)
	}
	second, _ := svc.ProposePlan(ctx, student, examID)
	res, err := svc.ConfirmPlan(ctx, student, second.ID, "clave-2")
	if err != nil {
		t.Fatalf("confirmar el plan nuevo: %v", err)
	}
	if got := countSessions(t, pool, student); got != len(res.Sessions) {
		t.Fatalf("tendrían que quedar las %d sesiones del plan nuevo, hay %d", len(res.Sessions), got)
	}

	// A pedido, sin cambiar el examen.
	if e, err := svc.Replan(ctx, student, examID); err != nil || e.PlanStatus != domain.PlanOutdated {
		t.Fatalf("rehacer a pedido: estado %q, error %v", e.PlanStatus, err)
	}
	if got := countSessions(t, pool, student); got != len(res.Sessions) {
		t.Fatalf("hasta confirmar el nuevo, las sesiones siguen: hay %d de %d", got, len(res.Sessions))
	}
}
