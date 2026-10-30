package auth

// Test de integración: el Verifier del gateway acepta el token que emite
// users-service de verdad (no uno firmado acá). Se ejecuta sólo si está
// definida USERS_SERVICE_URL, con el sistema levantado y el mismo JWT_SECRET:
//
//	docker compose up -d
//	USERS_SERVICE_URL=http://localhost:8081 JWT_SECRET=$(grep ^JWT_SECRET= ../../.env | cut -d= -f2) go test ./internal/auth/

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestElVerifierAceptaElTokenDeUsersService(t *testing.T) {
	base := os.Getenv("USERS_SERVICE_URL")
	if base == "" {
		t.Skip("USERS_SERVICE_URL no definida: se omite el test de integración con users-service")
	}
	v, err := NewVerifier(os.Getenv("JWT_SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	post := func(path string, body any) *http.Response {
		t.Helper()
		b, _ := json.Marshal(body)
		resp, err := client.Post(base+path, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	email := "gateway-" + strings.ToLower(rand.Text()) + "@prueba.com"
	resp := post("/register", map[string]string{"name": "Gateway", "email": email, "password": "secreta123"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("registro: status = %d", resp.StatusCode)
	}

	resp = post("/login", map[string]string{"email": email, "password": "secreta123"})
	defer resp.Body.Close()
	var login struct {
		Token string `json:"token"`
		User  struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("login: status = %d, err = %v", resp.StatusCode, err)
	}

	claims, err := v.Verify(login.Token)
	if err != nil {
		t.Fatalf("el Verifier del gateway rechazó el token de users-service: %v", err)
	}
	if claims.Subject != login.User.ID || claims.Role != RoleStudent {
		t.Errorf("claims = sub %q, role %q; se esperaba sub %q y rol estudiante", claims.Subject, claims.Role, login.User.ID)
	}
	if claims.ExpiresAt == nil || time.Until(claims.ExpiresAt.Time) < 7*time.Hour {
		t.Errorf("exp = %v, se esperaba una vigencia de 8 horas", claims.ExpiresAt)
	}
}
