package controller_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/controller"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service/memory"
)

// publishAs sube un resumen con la identidad que pondría el gateway y devuelve
// la respuesta cruda y el resumen guardado.
func publishAs(t *testing.T, name, email, formName string) (string, *memory.Store, string) {
	t.Helper()
	store := memory.NewStore()
	mux := http.NewServeMux()
	(&controller.Forum{Svc: &service.Service{Repo: store, Search: store, Files: memory.NewFiles()}, DB: store, Logger: logger, InternalToken: internalToken}).Register(mux)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "Integrales dobles")
	_ = mw.WriteField("category", "Análisis II")
	if formName != "" {
		_ = mw.WriteField("authorName", formName)
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="r.pdf"`)
	h.Set("Content-Type", "application/pdf")
	w, _ := mw.CreatePart(h)
	_, _ = w.Write(testPDF)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/summaries", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Internal-Token", internalToken)
	req.Header.Set("X-User-Id", "ana-1")
	if name != "" {
		req.Header.Set("X-User-Name", url.QueryEscape(name))
	}
	if email != "" {
		req.Header.Set("X-User-Email", url.QueryEscape(email))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publicar: %d %s", rec.Code, rec.Body)
	}
	body, _ := io.ReadAll(rec.Body)
	p, _ := store.ListByUser(context.Background(), "ana-1", 1, 1)
	return string(body), store, p.Items[0].ID
}

func TestElAutorSaleDelTokenYElMailNoSeMuestra(t *testing.T) {
	body, store, id := publishAs(t, "Ana Pérez", "ana@mail.com", "Otro Nombre")
	if !strings.Contains(body, `"name":"Ana Pérez"`) {
		t.Errorf("la respuesta no muestra el nombre del token: %s", body)
	}
	if strings.Contains(body, "ana@mail.com") || strings.Contains(body, `"email"`) {
		t.Errorf("la respuesta expone el mail del autor: %s", body)
	}
	doc, _ := store.Get(context.Background(), id)
	if doc.Author.Name != "Ana Pérez" || doc.Author.Email != "ana@mail.com" {
		t.Errorf("guardado = %+v, se esperaban nombre y mail del token", doc.Author)
	}
}

func TestSinNombreEnElTokenSeUsaElFormularioOElGenerico(t *testing.T) {
	_, store, id := publishAs(t, "", "", "Ana del formulario")
	if doc, _ := store.Get(context.Background(), id); doc.Author.Name != "Ana del formulario" || doc.Author.Email != "" {
		t.Errorf("sin nombre en el token: autor = %+v", doc.Author)
	}
	_, store, id = publishAs(t, "", "", "")
	if doc, _ := store.Get(context.Background(), id); doc.Author.Name != "Estudiante de Estudiario" {
		t.Errorf("sin nombre en ningún lado: autor = %q", doc.Author.Name)
	}
}
