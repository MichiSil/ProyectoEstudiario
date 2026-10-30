package mcpserver_test

// Tests del agente por MCP (SPEC 10.1) contra PostgreSQL real. Se ejecutan sólo
// si está definida PLANNER_TEST_DATABASE_URL, como los de adapters/postgres.

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/mcpserver"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/postgres"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/rules"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/system"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

func newService(t *testing.T) *app.Service {
	t.Helper()
	url := os.Getenv("PLANNER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PLANNER_TEST_DATABASE_URL no definida: se omiten los tests del agente por MCP")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	repo := postgres.New(pool, system.IDs{})
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &app.Service{Repo: repo, Generator: rules.Engine{}, Clock: system.Clock{}, IDs: system.IDs{}}
}

// estudianteConExamen crea un estudiante con una materia, disponibilidad y un examen en 14 días.
func estudianteConExamen(t *testing.T, svc *app.Service) (string, domain.Exam) {
	t.Helper()
	ctx := context.Background()
	student := "mcp-" + system.IDs{}.NewID()
	cba, _ := time.LoadLocation("America/Argentina/Cordoba")
	sub, err := svc.CreateSubject(ctx, domain.Subject{StudentID: student, Name: "Física II", Difficulty: domain.DifficultyMedium})
	if err != nil {
		t.Fatal(err)
	}
	var slots []domain.Slot
	for d := time.Sunday; d <= time.Saturday; d++ {
		slots = append(slots, domain.Slot{Weekday: d, StartMinute: 8 * 60, EndMinute: 22 * 60})
	}
	if err := svc.SetAvailability(ctx, domain.Availability{StudentID: student, Slots: slots, MaxDailyMinutes: 120, PreferredSessionMinutes: 60, Location: cba}); err != nil {
		t.Fatal(err)
	}
	exam, err := svc.CreateExam(ctx, domain.Exam{StudentID: student, SubjectID: sub.ID, Type: domain.ExamParcial,
		StartsAt: time.Now().In(cba).AddDate(0, 0, 14), Topics: []string{"Ondas", "Óptica"}})
	if err != nil {
		t.Fatal(err)
	}
	return student, exam
}

// conectar abre una sesión MCP del agente en nombre de un estudiante.
func conectar(t *testing.T, svc *app.Service, student string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := mcpserver.New(svc, student).Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "agente-de-prueba", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func llamar(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if out != nil && !res.IsError {
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: respuesta inesperada %s", tool, raw)
		}
	}
	return res
}

// Sólo existen las tres herramientas del SPEC: ninguna confirma (RN-37).
func TestMCP_HerramientasDisponibles(t *testing.T) {
	svc := newService(t)
	student, _ := estudianteConExamen(t, svc)
	cs := conectar(t, svc, student)

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	if len(names) != 3 || !names["listar_materias"] || !names["listar_examenes"] || !names["proponer_plan"] {
		t.Fatalf("herramientas inesperadas: %v", names)
	}
}

// CA-28: listar_examenes y proponer_plan devuelven una propuesta "propuesto" y no se reserva nada.
func TestMCP_ProponerSinReservar(t *testing.T) {
	svc := newService(t)
	student, exam := estudianteConExamen(t, svc)
	cs := conectar(t, svc, student)

	var materias struct {
		Materias []struct{ Nombre string } `json:"materias"`
	}
	llamar(t, cs, "listar_materias", map[string]any{}, &materias)
	if len(materias.Materias) != 1 || materias.Materias[0].Nombre != "Física II" {
		t.Fatalf("materias inesperadas: %+v", materias)
	}

	var examenes struct {
		Examenes []struct {
			ID      string   `json:"id"`
			Materia string   `json:"materia"`
			Temas   []string `json:"temas"`
		} `json:"examenes"`
	}
	llamar(t, cs, "listar_examenes", map[string]any{}, &examenes)
	if len(examenes.Examenes) != 1 || examenes.Examenes[0].ID != exam.ID || examenes.Examenes[0].Materia != "Física II" {
		t.Fatalf("exámenes inesperados: %+v", examenes)
	}

	var prop struct {
		PlanID   string `json:"planId"`
		Estado   string `json:"estado"`
		Sesiones []any  `json:"sesiones"`
	}
	llamar(t, cs, "proponer_plan", map[string]any{"examenId": exam.ID}, &prop)
	if prop.Estado != "propuesto" || len(prop.Sesiones) == 0 {
		t.Fatalf("se esperaba una propuesta con sesiones, llegó %+v", prop)
	}
	booked, err := svc.ListSessions(context.Background(), student)
	if err != nil {
		t.Fatal(err)
	}
	if len(booked) != 0 {
		t.Fatalf("la propuesta del agente no debería reservar sesiones y hay %d", len(booked))
	}
}

// CA-29: en nombre de un estudiante, el agente no ve ni planifica los exámenes de otro.
func TestMCP_NoAccedeADatosDeOtro(t *testing.T) {
	svc := newService(t)
	_, examAjeno := estudianteConExamen(t, svc)
	otro, _ := estudianteConExamen(t, svc)
	cs := conectar(t, svc, otro)

	var examenes struct {
		Examenes []struct {
			ID string `json:"id"`
		} `json:"examenes"`
	}
	llamar(t, cs, "listar_examenes", map[string]any{}, &examenes)
	for _, e := range examenes.Examenes {
		if e.ID == examAjeno.ID {
			t.Fatal("el agente vio un examen de otro estudiante")
		}
	}
	res := llamar(t, cs, "proponer_plan", map[string]any{"examenId": examAjeno.ID}, nil)
	if !res.IsError {
		t.Fatal("proponer un plan para un examen ajeno tendría que fallar")
	}
}
