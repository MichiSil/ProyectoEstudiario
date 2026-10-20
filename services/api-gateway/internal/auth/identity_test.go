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
