package controller_test

// Test de contrato: valida las respuestas reales de /v1/* y /health contra
// docs/contracts/documents-v1.yaml con kin-openapi. Si una respuesta (de éxito
// o de error) no cumple el contrato, el test falla.
//
// Por defecto prueba la API en memoria. Con FORUM_CONTRACT_URL prueba un
// forum-service levantado (por ejemplo, con docker compose):
//
//	FORUM_CONTRACT_URL=http://localhost:8083 FORUM_CONTRACT_API_KEY=clave-de-prueba-grupo4 go test -run Contrato ./internal/controller/

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/apikey"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/controller"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/ratelimit"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service/memory"
)

const contractPath = "../../../../docs/contracts/documents-v1.yaml"

// contractHost es el servidor del contrato con el que se buscan las rutas.
const contractHost = "http://localhost:8083"

var (
	testPDF = []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\n%%EOF\n")
	logger  = slog.New(slog.NewJSONHandler(io.Discard, nil))
)

type contractEnv struct {
	t        *testing.T
	baseURL  string
	key      string
	otherKey string // vacío si no se puede probar con una segunda API key
	svc      *service.Service
	router   routers.Router
	live     bool
}

func keyHash(k string) string {
	sum := sha256.Sum256([]byte(k))
	return hex.EncodeToString(sum[:])
}

func newContractEnv(t *testing.T) *contractEnv {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(contractPath)
	if err != nil {
		t.Fatalf("no se pudo leer el contrato: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("el contrato no es un OpenAPI válido: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	openapi3filter.RegisterBodyDecoder("application/problem+json", openapi3filter.JSONBodyDecoder)
	openapi3filter.RegisterBodyDecoder("application/pdf", openapi3filter.FileBodyDecoder)

	env := &contractEnv{t: t, router: router}
	if base := os.Getenv("FORUM_CONTRACT_URL"); base != "" {
		env.baseURL, env.key, env.live = strings.TrimRight(base, "/"), os.Getenv("FORUM_CONTRACT_API_KEY"), true
		if env.key == "" {
			t.Fatal("con FORUM_CONTRACT_URL hace falta FORUM_CONTRACT_API_KEY")
		}
		return env
	}

	store, files := memory.NewStore(), memory.NewFiles()
	env.svc = &service.Service{Repo: store, Search: store, Files: files}
	env.key, env.otherKey = "clave-grupo4", "clave-grupo7"
	keys, err := apikey.Parse("grupo4:" + keyHash(env.key) + ",grupo7:" + keyHash(env.otherKey))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /health", controller.Health(store))
	(&controller.V1{Svc: env.svc, Keys: keys, Limiter: ratelimit.New(), Logger: logger}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env.baseURL = srv.URL
	return env
}

// do hace el pedido y valida la respuesta contra el contrato.
func (e *contractEnv) do(req *http.Request) (*http.Response, []byte) {
	e.t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// La ruta se busca con la URL del servidor del contrato.
	routeReq := req.Clone(context.Background())
	routeReq.URL, _ = url.Parse(contractHost + req.URL.RequestURI())
	route, params, err := e.router.FindRoute(routeReq)
	if err != nil {
		e.t.Fatalf("%s %s no está en el contrato: %v", req.Method, req.URL.Path, err)
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request: routeReq, PathParams: params, Route: route,
			Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		},
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   io.NopCloser(bytes.NewReader(body)),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true, // un código que el contrato no documenta es un error
			MultiError:            true,
		},
	}
	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		e.t.Errorf("%s %s → %d no cumple el contrato:\n%v\ncuerpo: %s", req.Method, req.URL.Path, resp.StatusCode, err, body)
	}
	return resp, body
}

func (e *contractEnv) get(path, key string) (*http.Response, []byte) {
	req, _ := http.NewRequest(http.MethodGet, e.baseURL+path, nil)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	return e.do(req)
}

type form struct {
	fields   map[string]string
	file     []byte
	fileType string
}

func (e *contractEnv) register(key, idem string, f form) (*http.Response, []byte) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range f.fields {
		_ = mw.WriteField(k, v)
	}
	if f.file != nil {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="file"; filename="guia.pdf"`)
		h.Set("Content-Type", f.fileType)
		w, _ := mw.CreatePart(h)
		_, _ = w.Write(f.file)
	}
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, e.baseURL+"/v1/documents", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	return e.do(req)
}

func guide(category string) form {
	return form{
		fields: map[string]string{
			"title": "Guía de trekking en los glaciares", "category": category, "tags": "trekking,invierno",
			"authorName": "Lucía Fernández", "authorExternalId": "viajero-1842", "description": "Recorridos y refugios",
		},
		file: testPDF, fileType: "application/pdf",
	}
}

func newKey() string { return "contrato-" + strings.ToLower(rand.Text()) }

func expect(t *testing.T, resp *http.Response, body []byte, status int, code string) map[string]any {
	t.Helper()
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode != status {
		t.Fatalf("status = %d, se esperaba %d: %s", resp.StatusCode, status, body)
	}
	if code != "" && out["code"] != code {
		t.Errorf("code = %v, se esperaba %s", out["code"], code)
	}
	return out
}

func TestContratoRegistroYConsulta(t *testing.T) {
	e := newContractEnv(t)
	category := "Patagonia " + strings.ToLower(rand.Text())[:6]
	idem := newKey()

	// Registro: 202 con el documento pendiente y Location.
	resp, body := e.register(e.key, idem, guide(category))
	doc := expect(t, resp, body, http.StatusAccepted, "")
	id, _ := doc["id"].(string)
	if doc["status"] != "pendiente" || resp.Header.Get("Location") != "/v1/documents/"+id {
		t.Errorf("registro: status = %v, Location = %q", doc["status"], resp.Header.Get("Location"))
	}
	if resp.Header.Get("RateLimit-Limit") != "60" {
		t.Errorf("falta RateLimit-Limit: %v", resp.Header)
	}

	// Reintento: mismo documento con Idempotent-Replayed.
	resp, body = e.register(e.key, idem, guide(category))
	again := expect(t, resp, body, http.StatusAccepted, "")
	if again["id"] != id || resp.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("reintento: id = %v, Idempotent-Replayed = %q", again["id"], resp.Header.Get("Idempotent-Replayed"))
	}

	// Misma clave con otro cuerpo: 422 IDEMPOTENCY_KEY_REUSED.
	other := guide(category)
	other.fields["title"] = "Otra guía distinta"
	resp, body = e.register(e.key, idem, other)
	expect(t, resp, body, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED")

	// El pendiente lo ve quien lo registró, en el detalle y en submissions, con su externalId.
	resp, body = e.get("/v1/documents/"+id, e.key)
	detail := expect(t, resp, body, http.StatusOK, "")
	if author, _ := detail["author"].(map[string]any); author["externalId"] != "viajero-1842" {
		t.Errorf("la API key dueña no recibe author.externalId: %v", detail["author"])
	}
	resp, body = e.get("/v1/submissions?status=pendiente&page=1&size=50", e.key)
	expect(t, resp, body, http.StatusOK, "")

	// No aparece en la búsqueda mientras está pendiente.
	resp, body = e.get("/v1/documents?category="+url.QueryEscape(category), e.key)
	if page := expect(t, resp, body, http.StatusOK, ""); page["total"] != float64(0) {
		t.Errorf("un pendiente aparece en la búsqueda: %v", page["total"])
	}

	// Descarga del propio.
	resp, body = e.get("/v1/documents/"+id+"/file", e.key)
	expect(t, resp, body, http.StatusOK, "")
	if !bytes.Equal(body, testPDF) || resp.Header.Get("Content-Type") != "application/pdf" {
		t.Errorf("descarga: Content-Type = %q, %d bytes", resp.Header.Get("Content-Type"), len(body))
	}

	if e.live {
		return // aprobar necesita un administrador: se prueba en memoria
	}

	// Otra API key no lo ve mientras está pendiente (RN-39, CA-33).
	resp, body = e.get("/v1/documents/"+id, e.otherKey)
	expect(t, resp, body, http.StatusNotFound, "NOT_FOUND")

	// Aprobado: lo ven todos, sin el externalId para los demás, y aparece en la búsqueda.
	if err := e.svc.Approve(context.Background(), service.Viewer{UserID: "admin", Admin: true}, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Rate(context.Background(), service.Viewer{UserID: "est"}, id, 4); err != nil {
		t.Fatal(err)
	}
	resp, body = e.get("/v1/documents/"+id, e.otherKey)
	approved := expect(t, resp, body, http.StatusOK, "")
	if author, _ := approved["author"].(map[string]any); author["externalId"] != nil {
		t.Errorf("otra API key recibe author.externalId: %v", approved["author"])
	}
	resp, body = e.get("/v1/documents?q=glaciares&category="+url.QueryEscape(category)+"&tag=trekking&minRating=3&sort=rating&page=1&size=20", e.otherKey)
	if page := expect(t, resp, body, http.StatusOK, ""); page["total"] != float64(1) {
		t.Errorf("búsqueda de un aprobado: total = %v", page["total"])
	}
}

func TestContratoErrores(t *testing.T) {
	e := newContractEnv(t)

	resp, body := e.get("/v1/documents", "")
	expect(t, resp, body, http.StatusUnauthorized, "UNAUTHENTICATED")
	resp, body = e.get("/v1/documents", "clave-que-no-existe")
	expect(t, resp, body, http.StatusUnauthorized, "UNAUTHENTICATED")

	resp, body = e.get("/v1/documents/noexiste12345", e.key)
	expect(t, resp, body, http.StatusNotFound, "NOT_FOUND")
	resp, body = e.get("/v1/documents/noexiste12345/file", e.key)
	expect(t, resp, body, http.StatusNotFound, "NOT_FOUND")

	resp, body = e.get("/v1/documents?size=51", e.key)
	expect(t, resp, body, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	resp, body = e.get("/v1/submissions?status=eliminado", e.key)
	expect(t, resp, body, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	// Registro: sin Idempotency-Key, sin archivo, con datos inválidos, no PDF y demasiado grande.
	resp, body = e.register(e.key, "", guide("Patagonia"))
	expect(t, resp, body, http.StatusBadRequest, "MALFORMED_REQUEST")
	noFile := guide("Patagonia")
	noFile.file = nil
	resp, body = e.register(e.key, newKey(), noFile)
	expect(t, resp, body, http.StatusBadRequest, "MALFORMED_REQUEST")
	invalid := guide("P")
	invalid.fields["title"] = "ab"
	resp, body = e.register(e.key, newKey(), invalid)
	expect(t, resp, body, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	notPDF := guide("Patagonia")
	notPDF.file, notPDF.fileType = []byte("hola"), "text/plain"
	resp, body = e.register(e.key, newKey(), notPDF)
	expect(t, resp, body, http.StatusUnsupportedMediaType, "UNSUPPORTED_FILE_TYPE")
	big := guide("Patagonia")
	big.file = append(append([]byte{}, testPDF...), make([]byte, service.MaxFileSize)...)
	resp, body = e.register(e.key, newKey(), big)
	expect(t, resp, body, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE")

	resp, body = e.get("/health", "")
	expect(t, resp, body, http.StatusOK, "")
}

func TestContratoCupo(t *testing.T) {
	e := newContractEnv(t)
	if e.live {
		t.Skip("no se agota el cupo de la API key de un servicio real")
	}
	var resp *http.Response
	var body []byte
	for i := 0; i <= 60; i++ {
		resp, body = e.get("/v1/submissions", e.otherKey)
	}
	expect(t, resp, body, http.StatusTooManyRequests, "RATE_LIMITED")
	if resp.Header.Get("Retry-After") == "" || resp.Header.Get("RateLimit-Remaining") != "0" {
		t.Errorf("429 sin Retry-After o RateLimit-Remaining: %v", resp.Header)
	}
}
