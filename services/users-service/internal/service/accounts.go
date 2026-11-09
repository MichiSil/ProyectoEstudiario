package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/mail"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

// Estados de un evento document.external-registered en processed_events.
const (
	eventAccountCreated = "cuenta_creada" // la cuenta existe; falta mandar el mail
	eventDone           = "terminado"
)

// generatedPasswordLen es el largo de la contraseña generada: 16 caracteres
// base32 al azar (80 bits), más de lo que pide RN-02.
const generatedPasswordLen = 16

// AccountsRepository es lo que necesitan las cuentas de autores externos.
type AccountsRepository interface {
	Create(ctx context.Context, u model.User) error
	GetByEmail(ctx context.Context, email string) (model.User, error)
	GetByID(ctx context.Context, id string) (model.User, error)
	UpdatePassword(ctx context.Context, id, hash string) error
	GetProcessedEvent(ctx context.Context, eventID string) (status, userID string, err error)
	SaveProcessedEvent(ctx context.Context, eventID, eventType, status, userID string, at time.Time) error
}

// Mailer manda un mail.
type Mailer interface {
	Send(ctx context.Context, m mail.Mail) error
}

// ExternalAuthor son los datos de document.external-registered que importan acá.
type ExternalAuthor struct {
	EventID       string
	Name          string
	Email         string
	Source        string // por ejemplo agencia_de_viajes
	DocumentTitle string
}

// Accounts crea las cuentas de los autores de documentos registrados por un
// sistema externo (ADR-014, actualización del 2026-11-09): cuando la agencia de
// viajes registra una guía con el mail de quien la escribió, esa persona
// recibe una cuenta en Estudiario y un mail con una contraseña generada.
type Accounts struct {
	Repo   AccountsRepository
	Mailer Mailer
	Logger *slog.Logger
	// AppURL es la dirección del frontend que va en el mail.
	AppURL     string
	BcryptCost int
	Now        func() time.Time
}

func (a *Accounts) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

// CreateExternalAuthor procesa un evento document.external-registered. Es
// idempotente por eventId:
//
//   - si el mail ya tiene cuenta, no crea nada ni manda contraseña (la persona
//     ya puede entrar con la suya);
//   - si no, crea la cuenta con una contraseña al azar y le manda el mail;
//   - si el mail falla, el evento se reintenta: como la contraseña anterior
//     nunca llegó, se genera otra y se reemplaza el hash. La contraseña nunca
//     se guarda ni se loguea, sólo su hash.
func (a *Accounts) CreateExternalAuthor(ctx context.Context, in ExternalAuthor) error {
	log := a.Logger.With("eventId", in.EventID, "source", in.Source)
	status, userID, err := a.Repo.GetProcessedEvent(ctx, in.EventID)
	if err != nil {
		return err
	}
	if status == eventDone {
		log.InfoContext(ctx, "evento ya procesado, se ignora")
		return nil
	}

	email := normalizeEmail(in.Email)
	name := strings.TrimSpace(in.Name)
	if !validEmail(email) {
		return &model.ValidationError{Fields: []model.FieldError{{Field: "author.email", Message: "el mail no es válido"}}}
	}
	if name == "" || utf8.RuneCountInString(name) > MaxNameLen {
		return &model.ValidationError{Fields: []model.FieldError{{Field: "author.name", Message: "el nombre falta o es demasiado largo"}}}
	}

	var u model.User
	if status == eventAccountCreated {
		// Un intento anterior creó la cuenta pero no pudo mandar el mail.
		if u, err = a.Repo.GetByID(ctx, userID); err != nil {
			return err
		}
	} else {
		existing, err := a.Repo.GetByEmail(ctx, email)
		switch {
		case err == nil:
			log.InfoContext(ctx, "el autor ya tiene cuenta en Estudiario: no se crea otra", "userId", existing.ID)
			return a.Repo.SaveProcessedEvent(ctx, in.EventID, "document.external-registered", eventDone, existing.ID, a.now())
		case !errors.Is(err, model.ErrNotFound):
			return err
		}
		u = model.User{
			ID: strings.ToLower(rand.Text()), Name: name, Email: email,
			// Un placeholder que no coincide con ninguna contraseña: el hash
			// real se guarda abajo, justo antes de mandar el mail.
			PasswordHash: "!", Role: model.RoleStudent, Source: in.Source, CreatedAt: a.now(),
		}
		if err := a.Repo.Create(ctx, u); errors.Is(err, model.ErrEmailInUse) {
			// Se registró con ese mail entre la consulta y el alta.
			return a.Repo.SaveProcessedEvent(ctx, in.EventID, "document.external-registered", eventDone, "", a.now())
		} else if err != nil {
			return err
		}
		if err := a.Repo.SaveProcessedEvent(ctx, in.EventID, "document.external-registered", eventAccountCreated, u.ID, a.now()); err != nil {
			return err
		}
		log.InfoContext(ctx, "cuenta creada para el autor de un documento externo", "userId", u.ID)
	}

	password := rand.Text()[:generatedPasswordLen]
	cost := a.BcryptCost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return err
	}
	if err := a.Repo.UpdatePassword(ctx, u.ID, string(hash)); err != nil {
		return err
	}
	if err := a.Mailer.Send(ctx, a.welcomeMail(u, in, password)); err != nil {
		return fmt.Errorf("mandar el mail con la contraseña: %w", err)
	}
	log.InfoContext(ctx, "mail con la contraseña enviado", "userId", u.ID)
	return a.Repo.SaveProcessedEvent(ctx, in.EventID, "document.external-registered", eventDone, u.ID, a.now())
}

func (a *Accounts) welcomeMail(u model.User, in ExternalAuthor, password string) mail.Mail {
	title := strings.TrimSpace(in.DocumentTitle)
	if title == "" {
		title = "tu documento"
	} else {
		title = "«" + title + "»"
	}
	body := fmt.Sprintf(`Hola, %s:

La agencia de viajes compartió %s en el foro de Estudiario. Un administrador lo revisa antes de publicarlo.

Creamos una cuenta para que puedas entrar a Estudiario, ver tus documentos y usar el resto de la app:

  Dirección: %s
  Mail:      %s
  Contraseña: %s

Te recomendamos cambiar la contraseña la primera vez que entres (Ajustes → Cambiar contraseña).

Si no esperabas este mail, podés ignorarlo.

Estudiario
`, u.Name, title, a.AppURL, u.Email, password)
	return mail.Mail{
		MessageID: in.EventID,
		To:        u.Email,
		ToName:    u.Name,
		Subject:   "Tu cuenta en Estudiario",
		Body:      body,
	}
}

// ChangePassword cambia la contraseña del usuario si la actual es correcta.
func (s *Service) ChangePassword(ctx context.Context, userID, current, next string) error {
	switch {
	case utf8.RuneCountInString(next) < MinPasswordLen:
		return &model.ValidationError{Fields: []model.FieldError{{Field: "newPassword", Message: "la contraseña tiene que tener al menos 8 caracteres"}}}
	case len(next) > MaxPasswordLen:
		return &model.ValidationError{Fields: []model.FieldError{{Field: "newPassword", Message: "la contraseña tiene como máximo 72 bytes"}}}
	}
	u, err := s.Repo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return model.ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), s.cost())
	if err != nil {
		return err
	}
	return s.Repo.UpdatePassword(ctx, userID, string(hash))
}
