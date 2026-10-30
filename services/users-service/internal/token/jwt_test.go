package token

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

const secret = "secreto-de-prueba-con-mas-de-32-caracteres"

func TestElTokenTieneElFormatoQueEsperaElGateway(t *testing.T) {
	s, err := NewSigner(secret, DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	raw, exp, err := s.Issue(model.User{ID: "u-1", Role: model.RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(now.Add(8 * time.Hour)) {
		t.Errorf("expiresAt = %v, se esperaban 8 horas", exp)
	}

	// Mismas reglas que services/api-gateway/internal/auth/jwt.go. La prueba
	// con el Verifier real del gateway está en su test de integración.
	claims := &Claims{}
	tok, err := jwt.NewParser(
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer("users-service"),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	).ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil })
	if err != nil {
		t.Fatalf("el token no pasa las reglas del gateway: %v", err)
	}
	if tok.Header["alg"] != "HS256" {
		t.Errorf("alg = %v, se esperaba HS256", tok.Header["alg"])
	}
	if claims.Subject != "u-1" || claims.Role != "admin" {
		t.Errorf("sub y role = %q %q", claims.Subject, claims.Role)
	}
	if claims.IssuedAt == nil || !claims.IssuedAt.Equal(now) {
		t.Errorf("iat = %v, se esperaba %v", claims.IssuedAt, now)
	}
}

func TestSecretoDebilNoSeAcepta(t *testing.T) {
	if _, err := NewSigner("corto", DefaultTTL); !errors.Is(err, ErrWeakSecret) {
		t.Fatalf("err = %v, se esperaba ErrWeakSecret", err)
	}
}
