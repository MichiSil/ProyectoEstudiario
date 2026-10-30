package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/observability"
)

// Issuer es el emisor que deben declarar los JWT: users-service (ADR-001).
const Issuer = "users-service"

// MinSecretLen es el largo mínimo del secreto HS256 (32 bytes = 256 bits).
const MinSecretLen = 32

// Roles válidos del SPEC.
const (
	RoleStudent = "estudiante"
	RoleAdmin   = "admin"
)

// ErrWeakSecret indica que JWT_SECRET falta o es demasiado corto.
var ErrWeakSecret = errors.New("JWT_SECRET falta o tiene menos de 32 caracteres")

// Claims son los datos del JWT que usa el gateway: el id del usuario va en
// "sub", el rol en "role" y, desde que users-service los emite, el nombre y el
// mail en "name" y "email". Los tokens sin name ni email (los de antes o los
// de devtoken) siguen siendo válidos.
type Claims struct {
	Role  string `json:"role"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	jwt.RegisteredClaims
}

// Verifier valida los JWT firmados con HS256 por users-service.
type Verifier struct {
	secret []byte
	parser *jwt.Parser
}

// NewVerifier arma el validador. Falla si el secreto es débil, para que el
// gateway no arranque aceptando tokens fáciles de falsificar.
func NewVerifier(secret string) (*Verifier, error) {
	if len(secret) < MinSecretLen {
		return nil, ErrWeakSecret
	}
	return &Verifier{
		secret: []byte(secret),
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(Issuer),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(30*time.Second),
		),
	}, nil
}

// Verify valida la firma, el emisor, el vencimiento, el sujeto y el rol.
func (v *Verifier) Verify(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := v.parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return v.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return nil, errors.New("el token no tiene sujeto (sub)")
	}
	if claims.Role != RoleStudent && claims.Role != RoleAdmin {
		return nil, errors.New("el token tiene un rol desconocido")
	}
	return claims, nil
}

// Sign firma un JWT con el mismo formato que valida Verify. Lo usa el comando
// devtoken mientras users-service no emita tokens, y los tests.
func Sign(secret, userID, role string, ttl time.Duration) (string, error) {
	if len(secret) < MinSecretLen {
		return "", ErrWeakSecret
	}
	now := time.Now()
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// Require exige un JWT válido en "Authorization: Bearer <token>". Si es válido,
// completa X-User-Id y X-User-Role (y X-User-Name y X-User-Email si el token
// los trae) con los datos del token, después de borrar lo que haya mandado el
// cliente. Si no, responde 401 sin llamar al servicio.
func Require(v *Verifier, logger *slog.Logger, next http.Handler) http.Handler {
	return StripIdentity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			unauthenticated(w, r, "falta el token de acceso (Authorization: Bearer)")
			return
		}
		claims, err := v.Verify(raw)
		if err != nil {
			// Nunca se loguea el token (ADR-011), sólo el motivo.
			logger.WarnContext(r.Context(), "JWT rechazado", "reason", err.Error())
			unauthenticated(w, r, "el token de acceso no es válido o venció")
			return
		}
		r.Header.Set(UserIDHeader, claims.Subject)
		r.Header.Set(UserRoleHeader, claims.Role)
		if claims.Name != "" {
			r.Header.Set(UserNameHeader, url.QueryEscape(claims.Name))
		}
		if claims.Email != "" {
			r.Header.Set(UserEmailHeader, url.QueryEscape(claims.Email))
		}
		r.Header.Del("Authorization") // los servicios internos no necesitan el token
		next.ServeHTTP(w, r)
	}))
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// unauthenticated responde 401 en formato Problem Details (ADR-005, 2.2).
func unauthenticated(w http.ResponseWriter, r *http.Request, detail string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="estudiario"`)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":          "about:blank",
		"title":         "No autenticado",
		"status":        http.StatusUnauthorized,
		"detail":        detail,
		"code":          "UNAUTHENTICATED",
		"correlationId": observability.CorrelationID(r.Context()),
	})
}
