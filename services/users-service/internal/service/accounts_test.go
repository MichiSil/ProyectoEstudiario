package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/mail"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

// accountsRepo agrega a memRepo los eventos procesados.
type accountsRepo struct {
	*memRepo
	mu     sync.Mutex
	events map[string][2]string // eventId → {status, userId}
}

func newAccountsRepo() *accountsRepo {
	return &accountsRepo{memRepo: newMemRepo(), events: map[string][2]string{}}
}

func (r *accountsRepo) GetProcessedEvent(_ context.Context, id string) (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.events[id]
	return e[0], e[1], nil
}

func (r *accountsRepo) SaveProcessedEvent(_ context.Context, id, _, status, userID string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if userID == "" {
		userID = r.events[id][1]
	}
	r.events[id] = [2]string{status, userID}
	return nil
}

type fakeMailer struct {
	sent []mail.Mail
	err  error
}

func (m *fakeMailer) Send(_ context.Context, msg mail.Mail) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

var guia = ExternalAuthor{EventID: "ev-1", Name: "Lucía Fernández", Email: "Lucia@Viajes.com", Source: "agencia_de_viajes", DocumentTitle: "Guía de Bariloche"}

func newAccounts(repo *accountsRepo, m *fakeMailer) *Accounts {
	return &Accounts{Repo: repo, Mailer: m, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		AppURL: "https://estudiario.example", BcryptCost: bcrypt.MinCost}
}

// passwordIn saca la contraseña del cuerpo del mail.
func passwordIn(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`Contraseña: (\S+)`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("el mail no trae la contraseña:\n%s", body)
	}
	return m[1]
}

func TestAutorDeLaAgenciaRecibeCuentaYContraseña(t *testing.T) {
	repo, mailer := newAccountsRepo(), &fakeMailer{}
	if err := newAccounts(repo, mailer).CreateExternalAuthor(context.Background(), guia); err != nil {
		t.Fatal(err)
	}
	u, err := repo.GetByEmail(context.Background(), "lucia@viajes.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.Source != "agencia_de_viajes" || u.Role != model.RoleStudent {
		t.Errorf("usuario = %+v", u)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].To != "lucia@viajes.com" || !strings.Contains(mailer.sent[0].Body, "Guía de Bariloche") {
		t.Fatalf("mails = %+v", mailer.sent)
	}
	pass := passwordIn(t, mailer.sent[0].Body)
	if len(pass) != generatedPasswordLen || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pass)) != nil {
		t.Fatal("la contraseña del mail no es la de la cuenta")
	}
	if strings.Contains(u.PasswordHash, pass) {
		t.Fatal("se guardó la contraseña en claro")
	}
}

func TestEventoRepetidoNoMandaOtroMail(t *testing.T) {
	repo, mailer := newAccountsRepo(), &fakeMailer{}
	a := newAccounts(repo, mailer)
	for range 3 {
		if err := a.CreateExternalAuthor(context.Background(), guia); err != nil {
			t.Fatal(err)
		}
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("mandó %d mails", len(mailer.sent))
	}
}

func TestSiYaTieneCuentaNoSeCreaOtraNiSeMandaContraseña(t *testing.T) {
	repo, mailer := newAccountsRepo(), &fakeMailer{}
	_ = repo.Create(context.Background(), model.User{ID: "u9", Name: "Lucía", Email: "lucia@viajes.com", PasswordHash: "x", Role: model.RoleStudent})
	if err := newAccounts(repo, mailer).CreateExternalAuthor(context.Background(), guia); err != nil {
		t.Fatal(err)
	}
	if len(mailer.sent) != 0 || len(repo.users) != 1 || repo.users["u9"].PasswordHash != "x" {
		t.Fatalf("mails = %d, usuarios = %d", len(mailer.sent), len(repo.users))
	}
}

// Si el mail falla, el reintento manda una contraseña nueva a la misma cuenta.
func TestSiFallaElMailElReintentoMandaOtraContraseña(t *testing.T) {
	repo, mailer := newAccountsRepo(), &fakeMailer{err: errors.New("mailpit caído")}
	a := newAccounts(repo, mailer)
	if err := a.CreateExternalAuthor(context.Background(), guia); err == nil {
		t.Fatal("tenía que fallar")
	}
	mailer.err = nil
	if err := a.CreateExternalAuthor(context.Background(), guia); err != nil {
		t.Fatal(err)
	}
	if len(repo.users) != 1 || len(mailer.sent) != 1 {
		t.Fatalf("usuarios = %d, mails = %d", len(repo.users), len(mailer.sent))
	}
	u, _ := repo.GetByEmail(context.Background(), "lucia@viajes.com")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(passwordIn(t, mailer.sent[0].Body))) != nil {
		t.Fatal("la contraseña del mail no es la de la cuenta")
	}
}

func TestMailInvalidoEsErrorDeValidacion(t *testing.T) {
	in := guia
	in.Email = "no-es-un-mail"
	var verr *model.ValidationError
	if err := newAccounts(newAccountsRepo(), &fakeMailer{}).CreateExternalAuthor(context.Background(), in); !errors.As(err, &verr) {
		t.Fatalf("err = %v", err)
	}
}

func TestCambiarContraseña(t *testing.T) {
	repo := newMemRepo()
	s := &Service{Repo: repo, BcryptCost: bcrypt.MinCost}
	u, err := s.Register(context.Background(), RegisterInput{Name: "Ana", Email: "ana@x.com", Password: "vieja-123"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword(context.Background(), u.ID, "otra-cosa", "nueva-123"); !errors.Is(err, model.ErrInvalidCredentials) {
		t.Fatalf("con la actual mal: err = %v", err)
	}
	if err := s.ChangePassword(context.Background(), u.ID, "vieja-123", "corta"); err == nil {
		t.Fatal("aceptó una contraseña corta")
	}
	if err := s.ChangePassword(context.Background(), u.ID, "vieja-123", "nueva-123"); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(repo.users[u.ID].PasswordHash), []byte("nueva-123")) != nil {
		t.Fatal("no cambió la contraseña")
	}
}
