package auth

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
