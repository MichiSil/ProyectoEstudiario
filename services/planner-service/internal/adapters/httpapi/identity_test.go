package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testInternalToken = "token-interno-de-prueba-de-mas-de-32-caracteres"

func TestIdentidadSoloSiVieneDelGateway(t *testing.T) {
	casos := []struct {
		nombre         string
		configurado    string
		token, userID  string
		status         int
		llegaAlHandler bool
	}{
		{"con token interno e identidad", testInternalToken, testInternalToken, "estudiante-1", http.StatusOK, true},
		{"llamado directo sin token interno", testInternalToken, "", "estudiante-1", http.StatusUnauthorized, false},
		{"token interno equivocado", testInternalToken, "adivinado", "estudiante-1", http.StatusUnauthorized, false},
		{"servicio sin token configurado", "", "", "estudiante-1", http.StatusUnauthorized, false},
		{"token interno sin identidad", testInternalToken, testInternalToken, "", http.StatusUnauthorized, false},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			h := &Handler{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), InternalToken: c.configurado}
			var recibido string
			handler := h.withStudent(func(w http.ResponseWriter, r *http.Request, studentID string) {
				recibido = studentID
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
			if c.token != "" {
				req.Header.Set(InternalTokenHeader, c.token)
			}
			if c.userID != "" {
				req.Header.Set("X-User-Id", c.userID)
			}
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != c.status {
				t.Fatalf("status = %d, se esperaba %d", rec.Code, c.status)
			}
			if c.llegaAlHandler != (recibido != "") {
				t.Fatalf("llegó al handler = %v, se esperaba %v", recibido != "", c.llegaAlHandler)
			}
			if c.llegaAlHandler && recibido != c.userID {
				t.Errorf("studentID = %q, se esperaba %q", recibido, c.userID)
			}
			if c.status == http.StatusUnauthorized {
				var body map[string]any
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("el cuerpo no es JSON: %v", err)
				}
				if body["code"] != "UNAUTHENTICATED" {
					t.Errorf("code = %v, se esperaba UNAUTHENTICATED", body["code"])
				}
			}
		})
	}
}
