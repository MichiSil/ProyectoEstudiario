package club

// Test de contrato del Club de Beneficios (enunciado 4.1, ADR-009, 6). Lee el
// contrato OpenAPI del Grupo 1 y comprueba que el adaptador sigue siendo
// compatible con él:
//
//  1. los pedidos que arma el adaptador cumplen el contrato: la operación
//     existe, mandamos todos los campos obligatorios, ningún campo que el
//     contrato no conozca (additionalProperties: false) y los encabezados
//     obligatorios (X-API-Key, Idempotency-Key);
//  2. el adaptador entiende las respuestas: los ejemplos del contrato se leen
//     sin error, y los campos que leemos siguen siendo obligatorios en la
//     respuesta (si el Grupo 1 los vuelve opcionales o los quita, falla);
//  3. los códigos de error que traducimos siguen documentados.
//
// Por defecto usa la copia que implementamos, en
// docs/contracts/consumidos/club-beneficios-v2.yaml. Con CLUB_CONTRACT_LIVE=1
// baja además el contrato publicado en GitHub y corre las mismas
// comprobaciones, para detectar un cambio incompatible del proveedor:
//
//	CLUB_CONTRACT_LIVE=1 go test -run Contrato ./internal/club/

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	contractCopy = "../../../../docs/contracts/consumidos/club-beneficios-v2.yaml"
	contractLive = "https://raw.githubusercontent.com/martiipastore/Proyecto-ARQUI-SW---GIMNASIO/main/docs/contracts/benefits-api.v2.yaml"
)

type spec map[string]any

func loadSpec(t *testing.T, data []byte) spec {
	t.Helper()
	// Se decodifica en un map común: con el tipo spec, yaml.v3 usaría spec
	// también para los mapas anidados.
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("el contrato no es YAML válido: %v", err)
	}
	return spec(m)
}

func contracts(t *testing.T) map[string]spec {
	t.Helper()
	data, err := os.ReadFile(contractCopy)
	if err != nil {
		t.Fatalf("leer la copia del contrato: %v", err)
	}
	out := map[string]spec{"copia": loadSpec(t, data)}
	if os.Getenv("CLUB_CONTRACT_LIVE") == "1" {
		client := &http.Client{Timeout: 15 * time.Second}
		res, err := client.Get(contractLive)
		if err != nil {
			t.Fatalf("bajar el contrato publicado: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("bajar el contrato publicado: %s", res.Status)
		}
		live, _ := io.ReadAll(res.Body)
		out["publicado"] = loadSpec(t, live)
	}
	return out
}

// node recorre el documento por claves y resuelve los $ref internos.
func (s spec) node(v any, keys ...string) map[string]any {
	cur := s.resolve(v)
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = s.resolve(m[k])
	}
	m, _ := cur.(map[string]any)
	return m
}

func (s spec) resolve(v any) any {
	for {
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return v
		}
		var cur any = map[string]any(s)
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			cur = cur.(map[string]any)[part]
		}
		v = cur
	}
}

func (s spec) operation(t *testing.T, method, path string) map[string]any {
	t.Helper()
	op := s.node(map[string]any(s), "paths", path, method)
	if op == nil {
		t.Fatalf("el contrato ya no tiene %s %s", strings.ToUpper(method), path)
	}
	return op
}

func strs(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// checkRequest valida un cuerpo que arma el adaptador contra el esquema del pedido.
func (s spec) checkRequest(t *testing.T, op map[string]any, body any) {
	t.Helper()
	schema := s.node(op, "requestBody", "content", "application/json", "schema")
	if schema == nil {
		t.Fatal("la operación ya no recibe un cuerpo JSON")
	}
	raw, _ := json.Marshal(body)
	var sent map[string]any
	_ = json.Unmarshal(raw, &sent)
	props := s.node(schema, "properties")
	for _, req := range strs(schema["required"]) {
		if _, ok := sent[req]; !ok {
			t.Errorf("el contrato exige %q y el adaptador no lo manda", req)
		}
	}
	for k, v := range sent {
		p := s.node(props, k)
		if p == nil {
			t.Errorf("el adaptador manda %q, que el contrato no conoce", k)
			continue
		}
		if pat, ok := p["pattern"].(string); ok {
			if str, ok := v.(string); ok && !regexp.MustCompile(pat).MatchString(str) {
				t.Errorf("%q = %q no cumple el patrón %s", k, str, pat)
			}
		}
		if n, ok := v.(float64); ok {
			if minimum, ok := p["minimum"].(int); ok && n < float64(minimum) {
				t.Errorf("%q = %v es menor que el mínimo %d", k, n, minimum)
			}
		}
	}
}

// checkHeaders comprueba que el adaptador manda los encabezados obligatorios.
func (s spec) checkHeaders(t *testing.T, op map[string]any, sends []string) {
	t.Helper()
	params, _ := op["parameters"].([]any)
	for _, p := range params {
		param := s.node(p)
		if param["in"] == "header" && param["required"] == true && !slices.Contains(sends, param["name"].(string)) {
			t.Errorf("el contrato exige el encabezado %v y el adaptador no lo manda", param["name"])
		}
	}
	if len(s.node(map[string]any(s), "components", "securitySchemes", "ApiKeyAuth")) == 0 {
		t.Error("el contrato ya no autentica con ApiKeyAuth")
	}
}

// checkResponse comprueba que los campos que leemos sigan siendo obligatorios
// y que el adaptador entienda los ejemplos de la respuesta.
func (s spec) checkResponse(t *testing.T, op map[string]any, status string, reads []string, parse func([]byte) error) {
	t.Helper()
	content := s.node(op, "responses", status, "content", "application/json")
	if content == nil {
		t.Fatalf("el contrato ya no documenta la respuesta %s", status)
	}
	schema := s.node(content, "schema")
	required := strs(schema["required"])
	for _, f := range reads {
		if !slices.Contains(required, f) {
			t.Errorf("respuesta %s: el adaptador lee %q pero el contrato ya no lo garantiza", status, f)
		}
	}
	examples := s.node(content, "examples")
	if len(examples) == 0 {
		t.Fatalf("respuesta %s sin ejemplos para probar", status)
	}
	for name := range examples {
		raw, _ := json.Marshal(s.node(examples, name)["value"])
		if err := parse(raw); err != nil {
			t.Errorf("respuesta %s, ejemplo %q: el adaptador no la entiende: %v", status, name, err)
		}
	}
}

func (s spec) checkErrors(t *testing.T, op map[string]any, statuses ...string) {
	t.Helper()
	for _, st := range statuses {
		if s.node(op, "responses", st) == nil {
			t.Errorf("el contrato ya no documenta el error %s, que el adaptador traduce", st)
		}
	}
}

func TestContratoClub(t *testing.T) {
	for name, s := range contracts(t) {
		t.Run(name, func(t *testing.T) {
			if v := s.node(map[string]any(s), "info")["version"].(string); !strings.HasPrefix(v, "2.") {
				t.Fatalf("versión del contrato %s: el adaptador implementa la 2.x", v)
			}

			t.Run("registrar", func(t *testing.T) {
				op := s.operation(t, "post", "/v2/external-register")
				s.checkHeaders(t, op, []string{"X-API-Key", "Idempotency-Key", "X-Correlation-Id"})
				s.checkRequest(t, op, registrationBody{Name: "Ana Pérez", Email: "ana@example.test", ExternalUserID: "estudiario-abc123", EventID: "vinculo-abc123"})
				s.checkResponse(t, op, "202", []string{"eventId", "externalUserId", "status"}, func(b []byte) error {
					var r receiptBody
					if err := json.Unmarshal(b, &r); err != nil {
						return err
					}
					_, err := r.toReceipt()
					return err
				})
				s.checkErrors(t, op, "400", "401", "403", "409", "429", "503")
			})

			t.Run("consultar registro", func(t *testing.T) {
				op := s.operation(t, "get", "/v2/external-registrations/{eventId}")
				s.checkResponse(t, op, "200", []string{"eventId", "externalUserId", "status"}, func(b []byte) error {
					var r receiptBody
					if err := json.Unmarshal(b, &r); err != nil {
						return err
					}
					_, err := r.toReceipt()
					return err
				})
				s.checkErrors(t, op, "401", "404", "503")
			})

			t.Run("acreditar", func(t *testing.T) {
				op := s.operation(t, "post", "/v2/accounts/{externalUserId}/credits")
				s.checkHeaders(t, op, []string{"X-API-Key", "Idempotency-Key", "X-Correlation-Id"})
				s.checkRequest(t, op, creditBody{EventID: "resumen-aprobado-abc", Points: 50, Reason: "Resumen aprobado en Estudiario", Reference: "abc"})
				s.checkResponse(t, op, "201", []string{"movementId", "points", "reason", "occurredAt"}, func(b []byte) error {
					var o operationBody
					return json.Unmarshal(b, &o)
				})
				s.checkErrors(t, op, "400", "401", "403", "404", "409", "429", "503")
			})

			t.Run("saldo", func(t *testing.T) {
				op := s.operation(t, "get", "/v2/accounts/{externalUserId}/balance")
				s.checkResponse(t, op, "200", []string{"balance", "updatedAt"}, func(b []byte) error {
					var o balanceBody
					if err := json.Unmarshal(b, &o); err != nil {
						return err
					}
					if o.Balance == nil {
						return errString("sin balance")
					}
					return nil
				})
				s.checkErrors(t, op, "401", "404", "503")
			})

			t.Run("movimientos", func(t *testing.T) {
				op := s.operation(t, "get", "/v2/accounts/{externalUserId}/movements")
				s.checkResponse(t, op, "200", []string{"items"}, func(b []byte) error {
					var o movementPageBody
					return json.Unmarshal(b, &o)
				})
				item := s.node(op, "responses", "200", "content", "application/json", "schema", "properties", "items", "items")
				for _, f := range []string{"id", "points", "reason", "occurredAt"} {
					if !slices.Contains(strs(item["required"]), f) {
						t.Errorf("el movimiento ya no garantiza %q", f)
					}
				}
			})
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// Las rutas y los encabezados que arma el cliente son los del contrato: si el
// cliente cambia de ruta por error, este test lo detecta aunque el YAML no cambie.
func TestContratoRutasDelCliente(t *testing.T) {
	s := contracts(t)["copia"]
	var paths []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	})
	ctx := t.Context()
	_, _ = c.Register(ctx, Registration{EventID: "e"})
	_, _ = c.Registration(ctx, "e")
	_, _ = c.Credit(ctx, "estudiario-u1", Credit{EventID: "e"})
	_, _ = c.Balance(ctx, "estudiario-u1")
	_, _ = c.Movements(ctx, "estudiario-u1", 10)

	templates := map[string]string{
		"POST /v2/external-register":               "/v2/external-register",
		"GET /v2/external-registrations/e":         "/v2/external-registrations/{eventId}",
		"POST /v2/accounts/estudiario-u1/credits":  "/v2/accounts/{externalUserId}/credits",
		"GET /v2/accounts/estudiario-u1/balance":   "/v2/accounts/{externalUserId}/balance",
		"GET /v2/accounts/estudiario-u1/movements": "/v2/accounts/{externalUserId}/movements",
	}
	for _, p := range paths {
		tpl, ok := templates[p]
		if !ok {
			t.Errorf("el cliente llamó a %s, que no es una ruta esperada", p)
			continue
		}
		s.operation(t, strings.ToLower(strings.SplitN(p, " ", 2)[0]), tpl)
	}
	if len(paths) != len(templates) {
		t.Errorf("llamadas = %v", paths)
	}
}

// El contrato pide un UUID en X-Correlation-Id. Nuestros correlationId no
// siempre lo son: el adaptador sólo manda los que cumplen el formato (lo
// encontró la jornada de fallas, docs/POSTMORTEM.md).
func TestContratoCorrelationIdSoloUUID(t *testing.T) {
	for name, s := range contracts(t) {
		t.Run(name, func(t *testing.T) {
			param := s.node(map[string]any(s), "components", "parameters", "CorrelationId")
			schema := s.node(param, "schema")
			if schema == nil {
				t.Skip("el contrato ya no define X-Correlation-Id")
			}
			example, _ := param["example"].(string)
			if schema["format"] == "uuid" {
				if !uuidPattern.MatchString(example) {
					t.Errorf("el adaptador no mandaría el ejemplo del contrato %q", example)
				}
				if uuidPattern.MatchString("herapbdhhae5pg3bjt5f2uf4hj") {
					t.Error("el adaptador mandaría un correlationId que no es UUID")
				}
			}
		})
	}
}
