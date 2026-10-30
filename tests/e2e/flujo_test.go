package e2e

// Flujo principal de punta a punta (SPEC 10.2, CA-30), siempre a través del
// api-gateway y con el JWT de users-service, como lo usa el frontend.

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"
)

type user struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type subject struct {
	ID string `json:"id"`
}

type exam struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type proposal struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	GeneratedBy string   `json:"generatedBy"`
	Warnings    []string `json:"warnings"`
	Sessions    []struct {
		Start string `json:"start"`
	} `json:"sessions"`
}

type session struct {
	ID     string `json:"id"`
	PlanID string `json:"planId"`
	ExamID string `json:"examId"`
	Status string `json:"status"`
}

type confirmation struct {
	PlanID   string    `json:"planId"`
	Status   string    `json:"status"`
	Sessions []session `json:"sessions"`
}

// estudiante registra y loguea a un estudiante nuevo (paso 1).
func estudiante(t *testing.T) (*client, user) {
	t.Helper()
	c := newClient(t)
	email := fmt.Sprintf("e2e-%d@estudiario.test", time.Now().UnixNano())
	c.expect(http.StatusCreated, "POST", "/api/users/register",
		map[string]string{"name": "Estudiante E2E", "email": email, "password": "clave-e2e-segura"}, nil)

	var login struct {
		Token string `json:"token"`
		User  user   `json:"user"`
	}
	c.expect(http.StatusOK, "POST", "/api/users/login",
		map[string]string{"email": email, "password": "clave-e2e-segura"}, nil).decode(t, &login)
	if login.Token == "" || login.User.Role != "estudiante" {
		t.Fatalf("login sin token o con rol inesperado: %+v", login.User)
	}
	c.token = login.Token

	var me user
	c.expect(http.StatusOK, "GET", "/api/users/me", nil, nil).decode(t, &me)
	if me.ID != login.User.ID {
		t.Fatalf("/me devolvió otro usuario: %+v", me)
	}
	return c, me
}

// materiaExamenYDisponibilidad carga los datos del paso 2 y devuelve el examen.
func materiaExamenYDisponibilidad(t *testing.T, c *client, nombre string, dias int) exam {
	t.Helper()
	var sub subject
	c.expect(http.StatusCreated, "POST", "/api/planner/subjects",
		map[string]any{"name": nombre, "professors": []string{"Pérez"}, "difficulty": 3}, nil).decode(t, &sub)

	slots := []map[string]any{}
	for d := 0; d <= 6; d++ {
		slots = append(slots, map[string]any{"weekday": d, "startTime": "07:00", "endTime": "23:00"})
	}
	c.expect(http.StatusNoContent, "PUT", "/api/planner/availability", map[string]any{
		"weeklySlots": slots, "maxDailyMinutes": 180, "preferredSessionMinutes": 60, "timezone": "America/Argentina/Cordoba",
	}, nil)

	cba, _ := time.LoadLocation("America/Argentina/Cordoba")
	startsAt := time.Now().In(cba).AddDate(0, 0, dias).Truncate(time.Hour).Format(time.RFC3339)
	var ex exam
	c.expect(http.StatusCreated, "POST", "/api/planner/exams", map[string]any{
		"subjectId": sub.ID, "type": "parcial", "startsAt": startsAt, "topics": []string{"Integrales", "Series"},
	}, nil).decode(t, &ex)
	if ex.Status != "programado" {
		t.Fatalf("el examen tendría que quedar programado, quedó %q", ex.Status)
	}
	return ex
}

func sesiones(t *testing.T, c *client) []session {
	t.Helper()
	var list []session
	c.expect(http.StatusOK, "GET", "/api/planner/sessions", nil, nil).decode(t, &list)
	return list
}

// Pasos 1 a 5: registro, datos, propuesta, confirmación y reintento idempotente.
func TestFlujoPrincipal(t *testing.T) {
	c, _ := estudiante(t)
	ex := materiaExamenYDisponibilidad(t, c, "Análisis II", 14)

	// 3. Propuesta: queda "propuesto" y no reserva nada todavía (RN-08).
	var prop proposal
	c.expect(http.StatusCreated, "POST", "/api/planner/exams/"+ex.ID+"/proposals", nil, nil).decode(t, &prop)
	if prop.Status != "propuesto" || len(prop.Sessions) == 0 {
		t.Fatalf("se esperaba una propuesta con sesiones, llegó %+v", prop)
	}
	if n := len(sesiones(t, c)); n != 0 {
		t.Fatalf("la propuesta no debería reservar sesiones y hay %d", n)
	}
	t.Logf("propuesta %s armada por %q con %d sesiones", prop.ID, prop.GeneratedBy, len(prop.Sessions))

	// 4. Confirmación: las sesiones quedan reservadas en el calendario (CA-06).
	key := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	var first confirmation
	c.expect(http.StatusOK, "POST", "/api/planner/plans/"+prop.ID+"/confirm", nil,
		map[string]string{"Idempotency-Key": key}).decode(t, &first)
	if first.Status != "confirmado" || len(first.Sessions) != len(prop.Sessions) {
		t.Fatalf("se esperaban %d sesiones confirmadas, llegó %+v", len(prop.Sessions), first)
	}
	booked := sesiones(t, c)
	if len(booked) != len(first.Sessions) {
		t.Fatalf("el calendario tendría que tener %d sesiones y tiene %d", len(first.Sessions), len(booked))
	}
	for _, s := range booked {
		if s.PlanID != prop.ID || s.ExamID != ex.ID || s.Status != "pendiente" {
			t.Fatalf("sesión reservada inesperada: %+v", s)
		}
	}

	// 5. Reintento con la misma Idempotency-Key: mismo resultado, sin duplicar (CA-10).
	var retry confirmation
	c.expect(http.StatusOK, "POST", "/api/planner/plans/"+prop.ID+"/confirm", nil,
		map[string]string{"Idempotency-Key": key}).decode(t, &retry)
	if len(retry.Sessions) != len(first.Sessions) || retry.Sessions[0].ID != first.Sessions[0].ID {
		t.Fatalf("el reintento devolvió otro resultado: %+v", retry)
	}
	if n := len(sesiones(t, c)); n != len(first.Sessions) {
		t.Fatalf("el reintento duplicó sesiones: hay %d y tendría que haber %d", n, len(first.Sessions))
	}
}

// Paso 6: con la IA no disponible, la propuesta llega armada por el motor de
// reglas con el aviso AI_FALLBACK (CA-05). Necesita el sistema levantado con
// tests/e2e/docker-compose.e2e.yml, que apunta la IA a una dirección que no responde.
func TestPropuestaConIANoDisponible(t *testing.T) {
	if os.Getenv("E2E_AI_DOWN") != "1" {
		t.Skip("E2E_AI_DOWN no es 1: levantá el sistema con tests/e2e/docker-compose.e2e.yml para probar la IA caída")
	}
	c, _ := estudiante(t)
	ex := materiaExamenYDisponibilidad(t, c, "Física I", 10)

	start := time.Now()
	var prop proposal
	c.expect(http.StatusCreated, "POST", "/api/planner/exams/"+ex.ID+"/proposals", nil, nil).decode(t, &prop)
	if prop.GeneratedBy != "reglas" || !slices.Contains(prop.Warnings, "AI_FALLBACK") {
		t.Fatalf("se esperaba una propuesta de reglas con AI_FALLBACK, llegó %+v", prop)
	}
	if len(prop.Sessions) == 0 {
		t.Fatal("la propuesta de reglas tendría que tener sesiones")
	}
	t.Logf("propuesta por reglas con AI_FALLBACK en %s", time.Since(start).Round(time.Millisecond))
}

// RN-03: un estudiante no ve ni planifica los datos de otro.
func TestAislamientoEntreEstudiantes(t *testing.T) {
	a, _ := estudiante(t)
	ex := materiaExamenYDisponibilidad(t, a, "Química", 12)

	b, _ := estudiante(t)
	b.expect(http.StatusNotFound, "POST", "/api/planner/exams/"+ex.ID+"/proposals", nil, nil)
	var exams []exam
	b.expect(http.StatusOK, "GET", "/api/planner/exams", nil, nil).decode(t, &exams)
	if len(exams) != 0 {
		t.Fatalf("el otro estudiante no debería ver exámenes ajenos y ve %d", len(exams))
	}
}

// Sin token, el gateway rechaza el pedido (ADR-001).
func TestSinTokenRechaza(t *testing.T) {
	c := newClient(t)
	c.expect(http.StatusUnauthorized, "GET", "/api/planner/sessions", nil, nil)
	c.expect(http.StatusUnauthorized, "GET", "/api/planner/sessions", nil, map[string]string{"X-User-Id": "alguien"})
}
