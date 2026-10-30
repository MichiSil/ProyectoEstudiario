package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

// memRepo es un repositorio en memoria que respeta el mail único (RN-01).
type memRepo struct {
	mu    sync.Mutex
	users map[string]model.User
}

func newMemRepo() *memRepo { return &memRepo{users: map[string]model.User{}} }

func (m *memRepo) Create(_ context.Context, u model.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.users {
		if x.Email == u.Email {
			return model.ErrEmailInUse
		}
	}
	m.users[u.ID] = u
	return nil
}

func (m *memRepo) GetByEmail(_ context.Context, email string) (model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return model.User{}, model.ErrNotFound
}

func (m *memRepo) GetByID(_ context.Context, id string) (model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return model.User{}, model.ErrNotFound
	}
	return u, nil
}

type fakeTokens struct{}

func (fakeTokens) Issue(u model.User) (string, time.Time, error) {
	return "token-de-" + u.ID, time.Now().Add(time.Hour), nil
}

func newService(admins string) (*Service, *memRepo) {
	repo := newMemRepo()
	return &Service{Repo: repo, Tokens: fakeTokens{}, AdminEmails: ParseAdminEmails(admins), BcryptCost: bcrypt.MinCost}, repo
}

var ctx = context.Background()

func TestRegistroGuardaElHashYNoLaContrasena(t *testing.T) {
	svc, repo := newService("")
	u, err := svc.Register(ctx, RegisterInput{Name: " Flor ", Email: " Flor@Mail.com ", Password: "secreta123"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Flor" || u.Email != "flor@mail.com" {
		t.Errorf("nombre y mail = %q %q, se esperaban sin espacios y el mail en minúsculas", u.Name, u.Email)
	}
	if u.Role != model.RoleStudent {
		t.Errorf("rol = %q, se esperaba estudiante", u.Role)
	}
	guardado := repo.users[u.ID]
	if guardado.PasswordHash == "secreta123" {
		t.Fatal("la contraseña se guardó en texto plano (RN-02)")
	}
	if bcrypt.CompareHashAndPassword([]byte(guardado.PasswordHash), []byte("secreta123")) != nil {
		t.Error("el hash guardado no corresponde a la contraseña")
	}
}

func TestRegistroConMailRepetidoFalla(t *testing.T) {
	svc, _ := newService("")
	if _, err := svc.Register(ctx, RegisterInput{Name: "Ana", Email: "ana@mail.com", Password: "secreta123"}); err != nil {
		t.Fatal(err)
	}
	// Mismo mail con otras mayúsculas: también está repetido (RN-01).
	_, err := svc.Register(ctx, RegisterInput{Name: "Otra Ana", Email: "ANA@mail.com", Password: "otraclave1"})
	if !errors.Is(err, model.ErrEmailInUse) {
		t.Fatalf("err = %v, se esperaba ErrEmailInUse", err)
	}
}

func TestRegistroValidaLosDatos(t *testing.T) {
	casos := []struct {
		nombre string
		in     RegisterInput
		campo  string
	}{
		{"contraseña corta (RN-02)", RegisterInput{Name: "Ana", Email: "ana@mail.com", Password: "1234567"}, "password"},
		{"contraseña de más de 72 bytes", RegisterInput{Name: "Ana", Email: "ana@mail.com", Password: string(make([]byte, 73))}, "password"},
		{"sin nombre", RegisterInput{Name: "  ", Email: "ana@mail.com", Password: "secreta123"}, "name"},
		{"mail inválido", RegisterInput{Name: "Ana", Email: "ana", Password: "secreta123"}, "email"},
		{"mail con nombre visible", RegisterInput{Name: "Ana", Email: "Ana <ana@mail.com>", Password: "secreta123"}, "email"},
		{"mail sin dominio completo", RegisterInput{Name: "Ana", Email: "ana@localhost", Password: "secreta123"}, "email"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			svc, repo := newService("")
			_, err := svc.Register(ctx, c.in)
			var verr *model.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("err = %v, se esperaba ValidationError", err)
			}
			if len(verr.Fields) != 1 || verr.Fields[0].Field != c.campo {
				t.Errorf("campos = %+v, se esperaba un error en %s", verr.Fields, c.campo)
			}
			if len(repo.users) != 0 {
				t.Error("se guardó un usuario inválido")
			}
		})
	}
}

func TestRolAdminSoloSiElMailEstaEnAdminEmails(t *testing.T) {
	svc, _ := newService(" Admin@Estudiario.local , otra@admin.com,")

	admin, err := svc.Register(ctx, RegisterInput{Name: "Admin", Email: "admin@estudiario.local", Password: "secreta123"})
	if err != nil {
		t.Fatal(err)
	}
	if admin.Role != model.RoleAdmin {
		t.Errorf("rol = %q, se esperaba admin (RF-03)", admin.Role)
	}

	estudiante, err := svc.Register(ctx, RegisterInput{Name: "Ana", Email: "ana@mail.com", Password: "secreta123"})
	if err != nil {
		t.Fatal(err)
	}
	if estudiante.Role != model.RoleStudent {
		t.Errorf("rol = %q, se esperaba estudiante", estudiante.Role)
	}
}

func TestLoginCorrectoDevuelveElToken(t *testing.T) {
	svc, _ := newService("")
	u, err := svc.Register(ctx, RegisterInput{Name: "Ana", Email: "ana@mail.com", Password: "secreta123"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(ctx, " ANA@mail.com", "secreta123")
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "token-de-"+u.ID || res.User.ID != u.ID {
		t.Errorf("resultado = %+v, se esperaba el token del usuario %s", res, u.ID)
	}
}

func TestLoginInvalidoDaElMismoErrorExistaONoElMail(t *testing.T) {
	svc, _ := newService("")
	if _, err := svc.Register(ctx, RegisterInput{Name: "Ana", Email: "ana@mail.com", Password: "secreta123"}); err != nil {
		t.Fatal(err)
	}

	_, errClave := svc.Login(ctx, "ana@mail.com", "otraclave1")
	_, errMail := svc.Login(ctx, "nadie@mail.com", "secreta123")

	if !errors.Is(errClave, model.ErrInvalidCredentials) || !errors.Is(errMail, model.ErrInvalidCredentials) {
		t.Fatalf("errores = %v / %v, se esperaba ErrInvalidCredentials en los dos casos", errClave, errMail)
	}
	if errClave.Error() != errMail.Error() {
		t.Error("los mensajes distinguen si el mail existe")
	}
}

func TestMeDevuelveElUsuario(t *testing.T) {
	svc, _ := newService("")
	u, _ := svc.Register(ctx, RegisterInput{Name: "Ana", Email: "ana@mail.com", Password: "secreta123"})

	got, err := svc.Me(ctx, u.ID)
	if err != nil || got.Email != "ana@mail.com" {
		t.Fatalf("Me = %+v, %v", got, err)
	}
	if _, err := svc.Me(ctx, "no-existe"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("err = %v, se esperaba ErrNotFound", err)
	}
}
