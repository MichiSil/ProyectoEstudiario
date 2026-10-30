package repository_test

// Tests de integración contra un PostgreSQL real (requisito 2.11 del
// enunciado). Se ejecutan sólo si está definida USERS_TEST_DATABASE_URL:
//
//	docker compose up -d postgres users-db-init
//	USERS_TEST_DATABASE_URL=postgres://users_svc:users-local@localhost:5432/users go test ./internal/repository/

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/repository"
)

func newRepo(t *testing.T) *repository.Postgres {
	t.Helper()
	url := os.Getenv("USERS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("USERS_TEST_DATABASE_URL no definida: se omiten los tests de integración")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	repo := repository.New(pool)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("migrar: %v", err)
	}
	return repo
}

// newUser arma un usuario con un mail nuevo, para no depender de los datos de otros tests.
func newUser() model.User {
	id := strings.ToLower(rand.Text())
	return model.User{
		ID: id, Name: "Test", Email: "test-" + id + "@prueba.com",
		PasswordHash: "$2a$10$hash-de-prueba", Role: model.RoleStudent,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestGuardarYLeerUnUsuario(t *testing.T) {
	repo, ctx := newRepo(t), context.Background()
	u := newUser()
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	porMail, err := repo.GetByEmail(ctx, u.Email)
	if err != nil {
		t.Fatal(err)
	}
	porID, err := repo.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []model.User{porMail, porID} {
		if got.ID != u.ID || got.Email != u.Email || got.PasswordHash != u.PasswordHash || got.Role != u.Role || !got.CreatedAt.Equal(u.CreatedAt) {
			t.Errorf("leído = %+v, guardado = %+v", got, u)
		}
	}
}

func TestLaBaseRechazaElMailRepetido(t *testing.T) {
	repo, ctx := newRepo(t), context.Background()
	u := newUser()
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	otro := newUser()
	otro.Email = u.Email
	if err := repo.Create(ctx, otro); !errors.Is(err, model.ErrEmailInUse) {
		t.Fatalf("err = %v, se esperaba ErrEmailInUse (RN-01, restricción UNIQUE)", err)
	}
}

func TestUsuarioInexistente(t *testing.T) {
	repo, ctx := newRepo(t), context.Background()
	if _, err := repo.GetByEmail(ctx, "nadie-"+strings.ToLower(rand.Text())+"@prueba.com"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetByEmail: err = %v, se esperaba ErrNotFound", err)
	}
	if _, err := repo.GetByID(ctx, "no-existe"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetByID: err = %v, se esperaba ErrNotFound", err)
	}
}

func TestLaBaseNoAceptaRolesDesconocidos(t *testing.T) {
	repo, ctx := newRepo(t), context.Background()
	u := newUser()
	u.Role = "superusuario"
	if err := repo.Create(ctx, u); err == nil {
		t.Fatal("la base aceptó un rol que no existe")
	}
}

func TestCambiarElRol(t *testing.T) {
	repo, ctx := newRepo(t), context.Background()
	u := newUser()
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateRole(ctx, u.ID, model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, u.ID)
	if err != nil || got.Role != model.RoleAdmin {
		t.Fatalf("rol = %q, err = %v; se esperaba admin", got.Role, err)
	}
	if err := repo.UpdateRole(ctx, "no-existe", model.RoleAdmin); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("err = %v, se esperaba ErrNotFound", err)
	}
}
