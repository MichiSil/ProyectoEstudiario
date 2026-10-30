package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStripIdentityDescartaLaIdentidadDelCliente(t *testing.T) {
	var userID, role string
	var llamado bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llamado = true
		userID = r.Header.Get(UserIDHeader)
		role = r.Header.Get(UserRoleHeader)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/planner/sessions", nil)
	req.Header.Set(UserIDHeader, "otro-estudiante")
	req.Header.Set(UserRoleHeader, "admin")
	StripIdentity(next).ServeHTTP(httptest.NewRecorder(), req)

	if !llamado {
		t.Fatal("el pedido no llegó al siguiente handler")
	}
	if userID != "" || role != "" {
		t.Errorf("llegaron X-User-Id=%q y X-User-Role=%q, se esperaba que el gateway los borrara", userID, role)
	}
	if req.Header.Get(UserIDHeader) != "otro-estudiante" {
		t.Error("StripIdentity modificó el pedido original en lugar de una copia")
	}
}

func TestStripIdentityDescartaNombreYMailDelCliente(t *testing.T) {
	var name, email string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, email = r.Header.Get(UserNameHeader), r.Header.Get(UserEmailHeader)
	})
	// Ruta pública (registro o login): igual se borra lo que mande el cliente.
	req := httptest.NewRequest(http.MethodPost, "/api/users/login", nil)
	req.Header.Set(UserNameHeader, "Otra%20Persona")
	req.Header.Set(UserEmailHeader, "otra%40mail.com")
	StripIdentity(next).ServeHTTP(httptest.NewRecorder(), req)

	if name != "" || email != "" {
		t.Errorf("llegaron X-User-Name=%q y X-User-Email=%q, se esperaba que el gateway los borrara", name, email)
	}
}
