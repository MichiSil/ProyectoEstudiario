package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// availabilityRepo guarda sólo la disponibilidad; el resto de app.PlanRepository
// no se usa en este test.
type availabilityRepo struct {
	app.PlanRepository
	saved map[string]domain.Availability
}

func (r *availabilityRepo) SaveAvailability(_ context.Context, a domain.Availability) error {
	r.saved[a.StudentID] = a
	return nil
}

func (r *availabilityRepo) GetAvailability(_ context.Context, student string) (domain.Availability, error) {
	a, ok := r.saved[student]
	if !ok {
		return a, domain.ErrNotFound
	}
	return a, nil
}

// RF-07: la disponibilidad que se guarda con PUT se lee igual con GET, así el
// frontend no depende de lo que haya guardado el navegador.
func TestDisponibilidadSeLeeIgualQueSeGuardo(t *testing.T) {
	h := &Handler{
		Svc:           &app.Service{Repo: &availabilityRepo{saved: map[string]domain.Availability{}}},
		Logger:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		InternalToken: testInternalToken,
	}
	mux := h.Routes()
	call := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/availability", bytes.NewBufferString(body))
		req.Header.Set(InternalTokenHeader, testInternalToken)
		req.Header.Set("X-User-Id", "est-1")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := call(http.MethodGet, "")
	var problem map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&problem)
	if rec.Code != http.StatusNotFound || problem["code"] != "AVAILABILITY_NOT_SET" {
		t.Fatalf("sin disponibilidad: status %d, code %v; se esperaba 404 AVAILABILITY_NOT_SET", rec.Code, problem["code"])
	}

	in := `{"weeklySlots":[{"weekday":6,"startTime":"08:00","endTime":"20:00"},{"weekday":1,"startTime":"18:30","endTime":"21:00"}],` +
		`"maxDailyMinutes":180,"preferredSessionMinutes":60,"timezone":"America/Argentina/Cordoba"}`
	if rec := call(http.MethodPut, in); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT: status %d: %s", rec.Code, rec.Body)
	}
	rec = call(http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: status %d: %s", rec.Code, rec.Body)
	}
	var got availabilityDTO
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := availabilityDTO{
		// Ordenadas por día de la semana y hora.
		Slots:                   []slotDTO{{1, "18:30", "21:00"}, {6, "08:00", "20:00"}},
		MaxDailyMinutes:         180,
		PreferredSessionMinutes: 60,
		Timezone:                "America/Argentina/Cordoba",
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Errorf("GET devolvió %s, se esperaba %s", gotJSON, wantJSON)
	}
}
