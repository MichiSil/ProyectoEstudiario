// Package token emite los JWT que valida el api-gateway
// (services/api-gateway/internal/auth/jwt.go). El formato no se puede cambiar
// sin cambiar también el gateway: HS256 firmado con JWT_SECRET, emisor
// "users-service", el id del usuario en "sub", el rol en "role", e "iat" y "exp".
package token

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

// Issuer es el emisor que el gateway exige en los tokens.
const Issuer = "users-service"

// DefaultTTL es la vigencia de un token.
const DefaultTTL = 8 * time.Hour

// MinSecretLen es el largo mínimo del secreto HS256, el mismo que exige el gateway.
const MinSecretLen = 32

// ErrWeakSecret indica que JWT_SECRET falta o es demasiado corto.
var ErrWeakSecret = errors.New("JWT_SECRET falta o tiene menos de 32 caracteres")

// Claims son los datos del JWT, con los mismos nombres que lee el gateway.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// Signer firma los tokens de los usuarios.
type Signer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewSigner falla si el secreto es débil, para que el servicio no arranque
// emitiendo tokens fáciles de falsificar.
func NewSigner(secret string, ttl time.Duration) (*Signer, error) {
	if len(secret) < MinSecretLen {
		return nil, ErrWeakSecret
	}
	return &Signer{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

// Issue firma el JWT del usuario y devuelve cuándo vence.
func (s *Signer) Issue(u model.User) (string, time.Time, error) {
	now := s.now()
	exp := now.Add(s.ttl)
	claims := Claims{
		Role: u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   u.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	// exp se guarda en segundos: se devuelve con esa misma precisión.
	return signed, exp.Truncate(time.Second), nil
}
