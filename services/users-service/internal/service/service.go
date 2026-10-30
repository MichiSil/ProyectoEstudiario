// Package service es la capa de reglas de users-service: registro, inicio de
// sesión y datos del usuario (SPEC, sección 5).
package service

import (
	"context"
	"crypto/rand"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

// Límites de los datos del registro.
const (
	MinPasswordLen = 8  // RN-02
	MaxPasswordLen = 72 // bcrypt no usa más de 72 bytes
	MaxNameLen     = 100
)

// Repository es lo que la capa de servicio necesita de la persistencia.
type Repository interface {
	Create(ctx context.Context, u model.User) error
	GetByEmail(ctx context.Context, email string) (model.User, error)
	GetByID(ctx context.Context, id string) (model.User, error)
	UpdateRole(ctx context.Context, id, role string) error
}

// TokenIssuer firma el JWT de un usuario y devuelve cuándo vence.
type TokenIssuer interface {
	Issue(u model.User) (token string, expiresAt time.Time, err error)
}

// Service implementa los casos de uso de cuentas.
type Service struct {
	Repo   Repository
	Tokens TokenIssuer
	// AdminEmails son los mails con rol admin (RF-03: el rol no se elige, se
	// asigna por configuración). Se aplica al registrarse y en cada login. En minúsculas.
	AdminEmails map[string]bool
	// BcryptCost es el costo de bcrypt; 0 usa bcrypt.DefaultCost.
	BcryptCost int
}

// ParseAdminEmails convierte la variable ADMIN_EMAILS (mails separados por
// coma) en el conjunto que usa Service.
func ParseAdminEmails(list string) map[string]bool {
	out := map[string]bool{}
	for _, e := range strings.Split(list, ",") {
		if e = normalizeEmail(e); e != "" {
			out[e] = true
		}
	}
	return out
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// RegisterInput son los datos del registro (RF-01).
type RegisterInput struct {
	Name     string
	Email    string
	Password string
}

// Register crea un usuario con el rol que corresponde a su mail (ver roleFor).
// La contraseña se guarda sólo como hash bcrypt (RN-02).
func (s *Service) Register(ctx context.Context, in RegisterInput) (model.User, error) {
	name := strings.TrimSpace(in.Name)
	email := normalizeEmail(in.Email)

	var fields []model.FieldError
	switch {
	case name == "":
		fields = append(fields, model.FieldError{Field: "name", Message: "el nombre es obligatorio"})
	case utf8.RuneCountInString(name) > MaxNameLen:
		fields = append(fields, model.FieldError{Field: "name", Message: "el nombre tiene como máximo 100 caracteres"})
	}
	if !validEmail(email) {
		fields = append(fields, model.FieldError{Field: "email", Message: "el mail no es válido"})
	}
	switch {
	case utf8.RuneCountInString(in.Password) < MinPasswordLen:
		fields = append(fields, model.FieldError{Field: "password", Message: "la contraseña tiene que tener al menos 8 caracteres"})
	case len(in.Password) > MaxPasswordLen:
		fields = append(fields, model.FieldError{Field: "password", Message: "la contraseña tiene como máximo 72 bytes"})
	}
	if len(fields) > 0 {
		return model.User{}, &model.ValidationError{Fields: fields}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), s.cost())
	if err != nil {
		return model.User{}, err
	}
	role := s.roleFor(email)
	u := model.User{
		ID:           strings.ToLower(rand.Text()),
		Name:         name,
		Email:        email,
		PasswordHash: string(hash),
		Role:         role,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.Repo.Create(ctx, u); err != nil {
		return model.User{}, err
	}
	return u, nil
}

// validEmail acepta sólo una dirección simple (nombre@dominio), sin nombre
// visible ni espacios.
func validEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email && strings.Contains(email[strings.Index(email, "@"):], ".")
}

// LoginResult es lo que devuelve un inicio de sesión correcto.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	User      model.User
}

// Login verifica el mail y la contraseña y emite el JWT (RF-02). Si el mail no
// existe o la contraseña no coincide devuelve el mismo error,
// model.ErrInvalidCredentials.
func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	u, err := s.Repo.GetByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, model.ErrNotFound) {
		// Se compara igual contra un hash, así la respuesta tarda lo mismo
		// exista o no el mail y no se puede averiguar cuáles están registrados.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return LoginResult{}, model.ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return LoginResult{}, model.ErrInvalidCredentials
	}
	// El rol se recalcula en cada login: si se agrega o se saca un mail de
	// ADMIN_EMAILS, vale desde el próximo inicio de sesión, sin tocar la base a
	// mano. Se guarda para que GET /me muestre el mismo rol que el token.
	if role := s.roleFor(u.Email); role != u.Role {
		if err := s.Repo.UpdateRole(ctx, u.ID, role); err != nil {
			return LoginResult{}, err
		}
		u.Role = role
	}
	token, exp, err := s.Tokens.Issue(u)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, ExpiresAt: exp, User: u}, nil
}

// Me devuelve los datos del usuario identificado por el gateway.
func (s *Service) Me(ctx context.Context, userID string) (model.User, error) {
	return s.Repo.GetByID(ctx, userID)
}

// roleFor devuelve admin si el mail está en AdminEmails y estudiante si no.
func (s *Service) roleFor(email string) string {
	if s.AdminEmails[email] {
		return model.RoleAdmin
	}
	return model.RoleStudent
}

func (s *Service) cost() int {
	if s.BcryptCost == 0 {
		return bcrypt.DefaultCost
	}
	return s.BcryptCost
}

// dummyHash es el hash bcrypt de una contraseña cualquiera, con el costo por
// defecto, para igualar el tiempo del login cuando el mail no existe.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("contraseña-que-no-usa-nadie"), bcrypt.DefaultCost)
