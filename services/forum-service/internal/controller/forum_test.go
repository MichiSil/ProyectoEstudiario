package controller_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/controller"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service/memory"
)

const internalToken = "secreto-interno-de-prueba-con-32-caracteres"

func forumHandler() http.Handler {
	store := memory.NewStore()
	mux := http.NewServeMux()
	(&controller.Forum{
		Svc: &service.Service{Repo: store, Search: store, Files: memory.NewFiles()},
		DB:  store, Logger: logger, InternalToken: internalToken,
	}).Register(mux)
	return mux
}

func forumReq(t *testing.T, h http.Handler, method, path string, body *bytes.Buffer, contentType, user, role string, withToken bool) (int, map[string]any) {
	t.Helper()
	if body == nil {
		body = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if withToken {
		req.Header.Set("X-Internal-Token", internalToken)
	}
	if user != "" {
		req.Header.Set("X-User-Id", user)
	}
	if role != "" {
		req.Header.Set("X-User-Role", role)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func uploadBody(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "Integrales dobles")
	_ = mw.WriteField("category", "Análisis II")
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="resumen.pdf"`)
	h.Set("Content-Type", "application/pdf")
	w, _ := mw.CreatePart(h)
	_, _ = w.Write(testPDF)
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestForoExigeElSecretoDelGateway(t *testing.T) {
	h := forumHandler()
	if code, body := forumReq(t, h, "GET", "/summaries/mine", nil, "", "ana", "", false); code != 401 || body["code"] != "UNAUTHENTICATED" {
		t.Errorf("sin secreto interno: %d %v", code, body["code"])
	}
	if code, _ := forumReq(t, h, "GET", "/summaries/mine", nil, "", "", "", true); code != 401 {
		t.Errorf("sin X-User-Id: %d", code)
	}
	if code, _ := forumReq(t, h, "GET", "/summaries/mine", nil, "", "ana", "", true); code != 200 {
		t.Errorf("con secreto e identidad: %d", code)
	}
}

func TestRutasDeAdminDevuelven403AUnEstudiante(t *testing.T) {
	h := forumHandler()
	for _, r := range []struct{ method, path string }{
		{"GET", "/admin/summaries/pending"},
		{"POST", "/admin/summaries/abc12345/approve"},
		{"POST", "/admin/summaries/abc12345/reject"},
		{"DELETE", "/admin/summaries/abc12345"},
	} {
		if code, body := forumReq(t, h, r.method, r.path, nil, "", "ana", "estudiante", true); code != 403 || body["code"] != "FORBIDDEN" {
			t.Errorf("%s %s: %d %v, se esperaba 403 FORBIDDEN", r.method, r.path, code, body["code"])
		}
	}
	if code, _ := forumReq(t, h, "GET", "/admin/summaries/pending", nil, "", "admin-1", "admin", true); code != 200 {
		t.Errorf("un admin en pendientes: %d", code)
	}
}

func TestSubirYModerarPorLaAPIDelForo(t *testing.T) {
	h := forumHandler()
	body, ct := uploadBody(t)
	code, doc := forumReq(t, h, "POST", "/summaries", body, ct, "ana", "estudiante", true)
	if code != 201 || doc["status"] != "pendiente" || doc["source"] != "estudiario" {
		t.Fatalf("subir: %d %v", code, doc)
	}
	id := doc["id"].(string)
	if author := doc["author"].(map[string]any); author["externalId"] != nil || author["name"] == "" {
		t.Errorf("autor = %v", author)
	}

	reject := bytes.NewBufferString(`{"reason":""}`)
	if code, out := forumReq(t, h, "POST", "/admin/summaries/"+id+"/reject", reject, "application/json", "admin-1", "admin", true); code != 422 || out["code"] != "VALIDATION_ERROR" {
		t.Errorf("rechazar sin motivo: %d %v (CA-17)", code, out["code"])
	}
	if code, out := forumReq(t, h, "POST", "/admin/summaries/"+id+"/approve", nil, "", "admin-1", "admin", true); code != 200 || out["status"] != "aprobado" {
		t.Errorf("aprobar: %d %v", code, out["status"])
	}
	if code, out := forumReq(t, h, "POST", "/admin/summaries/"+id+"/approve", nil, "", "admin-1", "admin", true); code != 409 || out["code"] != "ALREADY_MODERATED" {
		t.Errorf("aprobar dos veces: %d %v (RN-23)", code, out["code"])
	}
	rate := bytes.NewBufferString(`{"score":5}`)
	if code, out := forumReq(t, h, "PUT", "/summaries/"+id+"/rating", rate, "application/json", "ana", "estudiante", true); code != 422 || out["code"] != "OWN_SUMMARY" {
		t.Errorf("calificar el propio: %d %v (CA-23)", code, out["code"])
	}
	if code, out := forumReq(t, h, "GET", "/summaries?subject=analisis%20ii", nil, "", "beto", "estudiante", true); code != 200 || out["total"] != float64(1) {
		t.Errorf("listado de aprobados: %d %v", code, out["total"])
	}
}
