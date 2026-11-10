package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// materialRepo guarda sólo materias y material; el resto de app.PlanRepository
// no se usa en este test.
type materialRepo struct {
	app.PlanRepository
	materials map[string]domain.Material
}

func (r *materialRepo) GetSubject(_ context.Context, student, id string) (domain.Subject, error) {
	if student != "est-1" || id != "s-1" {
		return domain.Subject{}, domain.ErrNotFound
	}
	return domain.Subject{ID: id, StudentID: student}, nil
}

func (r *materialRepo) CreateMaterial(_ context.Context, m domain.Material) error {
	r.materials[m.ID] = m
	return nil
}

func (r *materialRepo) GetMaterial(_ context.Context, student, id string) (domain.Material, error) {
	m, ok := r.materials[id]
	if !ok || m.StudentID != student {
		return domain.Material{}, domain.ErrNotFound
	}
	return m, nil
}

type fileStore map[string][]byte

func (f fileStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	f[key] = b
	return err
}

func (f fileStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f[key])), nil
}

func (f fileStore) Delete(_ context.Context, key string) error {
	delete(f, key)
	return nil
}

type seqID struct{ n int }

func (s *seqID) NewID() string { s.n++; return "m-" + string(rune('0'+s.n)) }

type fixedNow struct{}

func (fixedNow) Now() time.Time { return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) }

// POST /subjects/{id}/materials acepta multipart con un PDF (y sigue aceptando
// JSON), y GET /materials/{id}/file lo descarga sólo para el dueño (RN-03).
func TestMaterialConArchivo(t *testing.T) {
	files := fileStore{}
	h := &Handler{
		Svc:           &app.Service{Repo: &materialRepo{materials: map[string]domain.Material{}}, Files: files, IDs: &seqID{}, Clock: fixedNow{}},
		Logger:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		InternalToken: testInternalToken,
	}
	mux := h.Routes()
	do := func(req *http.Request, student string) *httptest.ResponseRecorder {
		req.Header.Set(InternalTokenHeader, testInternalToken)
		req.Header.Set("X-User-Id", student)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	pdf := []byte("%PDF-1.4\nTP4\n%%EOF")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("title", "TP4")
	part, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="file"; filename="tp4.pdf"`},
		"Content-Type":        {"application/pdf"},
	})
	_, _ = part.Write(pdf)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/subjects/s-1/materials", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := do(req, "est-1")
	var created materialDTO
	_ = json.NewDecoder(rec.Body).Decode(&created)
	if rec.Code != http.StatusCreated || created.File == nil || created.File.DownloadPath != "/materials/"+created.ID+"/file" || created.Kind != "teorico" {
		t.Fatalf("subir con PDF: status %d, %+v", rec.Code, created)
	}

	rec = do(httptest.NewRequest(http.MethodGet, created.File.DownloadPath, nil), "est-1")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pdf) || rec.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("descargar: status %d, %q", rec.Code, rec.Body.Bytes())
	}
	if rec := do(httptest.NewRequest(http.MethodGet, created.File.DownloadPath, nil), "est-2"); rec.Code != http.StatusNotFound {
		t.Errorf("otro estudiante descarga: status %d, se esperaba 404", rec.Code)
	}

	// Sin archivo sigue aceptando JSON, como usa el frontend.
	req = httptest.NewRequest(http.MethodPost, "/subjects/s-1/materials", bytes.NewBufferString(`{"kind":"practico","title":"Guía 2"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = do(req, "est-1")
	created = materialDTO{}
	_ = json.NewDecoder(rec.Body).Decode(&created)
	if rec.Code != http.StatusCreated || created.File != nil || created.Kind != "practico" {
		t.Fatalf("JSON sin archivo: status %d, %+v", rec.Code, created)
	}
}
