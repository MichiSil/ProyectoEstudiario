package auth

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "secreto-de-prueba-con-mas-de-32-caracteres"

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

func mustVerifier(t *testing.T) *Verifier {
	t.Helper()
	v, err := NewVerifier(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func mustSign(t *testing.T, secret, userID, role string, ttl time.Duration) string {
	t.Helper()
	tok, err := Sign(secret, userID, role, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// serve manda un pedido con el encabezado Authorization dado (vacío = sin token)
// y un X-User-Id falso, y devuelve la respuesta y la identidad que vio el servicio.
func serve(t *testing.T, authorization string) (*httptest.ResponseRecorder, string, string, bool) {
	t.Helper()
	var userID, role string
	var llamado bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llamado = true
		userID = r.Header.Get(UserIDHeader)
		role = r.Header.Get(UserRoleHeader)
		if r.Header.Get("Authorization") != "" {
			t.Error("el token llegó al servicio interno")
		}
	})
	req := httptest.NewRequest(http.MethodGet, "/api/planner/sessions", nil)
	req.Header.Set(UserIDHeader, "otro-estudiante")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	Require(mustVerifier(t), testLogger, next).ServeHTTP(rec, req)
	return rec, userID, role, llamado
}

func TestTokenValidoCompletaLaIdentidadDesdeElToken(t *testing.T) {
	tok := mustSign(t, testSecret, "estudiante-1", RoleStudent, time.Hour)
	rec, userID, role, llamado := serve(t, "Bearer "+tok)

	if !llamado || rec.Code != http.StatusOK {
		t.Fatalf("status = %d, llamado = %v; se esperaba que pasara", rec.Code, llamado)
	}
	if userID != "estudiante-1" || role != RoleStudent {
		t.Errorf("identidad = %q/%q, se esperaba estudiante-1/estudiante (la del token, no la del encabezado)", userID, role)
	}
}

func TestPedidosSinTokenValidoDevuelven401(t *testing.T) {
	otroSecreto := "otro-secreto-distinto-de-mas-de-32-caracteres"
	sinFirma := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{
		Role:             RoleStudent,
		RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Subject: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	})
	none, _ := sinFirma.SignedString(jwt.UnsafeAllowNoneSignatureType)
	otroEmisor, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Role:             RoleStudent,
		RegisteredClaims: jwt.RegisteredClaims{Issuer: "otro", Subject: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}).SignedString([]byte(testSecret))
	sinVencimiento, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Role:             RoleStudent,
		RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Subject: "x"},
	}).SignedString([]byte(testSecret))

	casos := map[string]string{
		"sin encabezado":           "",
		"esquema que no es Bearer": "Basic dXNlcjpwYXNz",
		"Bearer vacío":             "Bearer ",
		"basura":                   "Bearer no-es-un-jwt",
		"firmado con otro secreto": "Bearer " + mustSign(t, otroSecreto, "x", RoleStudent, time.Hour),
		"vencido":                  "Bearer " + mustSign(t, testSecret, "x", RoleStudent, -time.Hour),
		"sin sujeto":               "Bearer " + mustSign(t, testSecret, "", RoleStudent, time.Hour),
		"rol desconocido":          "Bearer " + mustSign(t, testSecret, "x", "root", time.Hour),
		"alg none":                 "Bearer " + none,
		"otro emisor":              "Bearer " + otroEmisor,
		"sin vencimiento":          "Bearer " + sinVencimiento,
	}
	for nombre, authorization := range casos {
		t.Run(nombre, func(t *testing.T) {
			rec, _, _, llamado := serve(t, authorization)
			if llamado {
				t.Fatal("el pedido llegó al servicio sin un token válido")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, se esperaba 401", rec.Code)
			}
			if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Error("falta WWW-Authenticate: Bearer")
			}
			var body map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("el cuerpo no es JSON: %v", err)
			}
			if body["code"] != "UNAUTHENTICATED" {
				t.Errorf("code = %v, se esperaba UNAUTHENTICATED", body["code"])
			}
		})
	}
}

func TestSecretoCortoNoSeAcepta(t *testing.T) {
	if _, err := NewVerifier("corto"); err == nil {
		t.Error("NewVerifier aceptó un secreto de menos de 32 caracteres")
	}
	if _, err := Sign("corto", "x", RoleStudent, time.Hour); err == nil {
		t.Error("Sign aceptó un secreto de menos de 32 caracteres")
	}
}

// signProfile firma un token con nombre y mail, como los de users-service.
func signProfile(t *testing.T, name, email string) string {
	t.Helper()
	now := time.Now()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Role: RoleStudent, Name: name, Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: Issuer, Subject: "ana-1",
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// identityAfterRequire devuelve los encabezados de identidad que le llegan al servicio.
func identityAfterRequire(t *testing.T, token string, clientHeaders map[string]string) (status int, got map[string]string) {
	t.Helper()
	got = map[string]string{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range []string{UserIDHeader, UserRoleHeader, UserNameHeader, UserEmailHeader} {
			got[h] = r.Header.Get(h)
		}
	})
	req := httptest.NewRequest(http.MethodGet, "/api/forum/summaries/mine", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range clientHeaders {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	Require(mustVerifier(t), testLogger, next).ServeHTTP(rec, req)
	return rec.Code, got
}

func TestNombreYMailDelTokenLleganCodificados(t *testing.T) {
	status, got := identityAfterRequire(t, signProfile(t, "Ana Pérez Ñandú", "ana+foro@mail.com"), nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got[UserNameHeader] != "Ana+P%C3%A9rez+%C3%91and%C3%BA" || got[UserEmailHeader] != "ana%2Bforo%40mail.com" {
		t.Errorf("encabezados = %q / %q, se esperaban nombre y mail codificados", got[UserNameHeader], got[UserEmailHeader])
	}
	name, _ := url.QueryUnescape(got[UserNameHeader])
	email, _ := url.QueryUnescape(got[UserEmailHeader])
	if name != "Ana Pérez Ñandú" || email != "ana+foro@mail.com" {
		t.Errorf("decodificados = %q / %q", name, email)
	}
}

func TestNombreYMailDelClienteSeReemplazanOBorran(t *testing.T) {
	spoof := map[string]string{UserNameHeader: "Otra%20Persona", UserEmailHeader: "otra%40mail.com"}

	_, got := identityAfterRequire(t, signProfile(t, "Ana Pérez", "ana@mail.com"), spoof)
	if got[UserNameHeader] != "Ana+P%C3%A9rez" || got[UserEmailHeader] != "ana%40mail.com" {
		t.Errorf("con token con nombre: llegó %q / %q, se esperaban los del token", got[UserNameHeader], got[UserEmailHeader])
	}

	// Un token sin name ni email (de antes o de devtoken): lo del cliente se borra igual.
	status, got := identityAfterRequire(t, mustSign(t, testSecret, "ana-1", RoleStudent, time.Hour), spoof)
	if status != http.StatusOK || got[UserIDHeader] != "ana-1" {
		t.Fatalf("token sin nombre ni mail: status = %d, X-User-Id = %q; se esperaba que siguiera andando", status, got[UserIDHeader])
	}
	if got[UserNameHeader] != "" || got[UserEmailHeader] != "" {
		t.Errorf("token sin nombre ni mail: llegaron %q / %q del cliente, se esperaba que se borraran", got[UserNameHeader], got[UserEmailHeader])
	}
}
