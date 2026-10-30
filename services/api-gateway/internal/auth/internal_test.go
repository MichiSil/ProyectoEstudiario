package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testInternalToken = "token-interno-de-prueba-de-mas-de-32-caracteres"

func TestWithInternalTokenReemplazaElDelCliente(t *testing.T) {
	var recibido string
	h, err := WithInternalToken(testInternalToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recibido = r.Header.Get(InternalTokenHeader)
	}))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/planner/sessions", nil)
	req.Header.Set(InternalTokenHeader, "adivinado-por-el-cliente")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if recibido != testInternalToken {
		t.Errorf("X-Internal-Token recibido = %q, se esperaba el del gateway", recibido)
	}
}

func TestWithInternalTokenRechazaUnTokenCorto(t *testing.T) {
	if _, err := WithInternalToken("corto", http.NotFoundHandler()); err == nil {
		t.Error("se aceptó un INTERNAL_TOKEN de menos de 32 caracteres")
	}
}
