package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/postgres"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/system"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// Con la base caída (nadie escucha en el puerto), las operaciones fallan
// rápido con domain.ErrUnavailable en lugar de un error genérico (ADR-010).
// No necesita un PostgreSQL real: corre siempre.
func TestGuarded_BaseCaidaDevuelveErrUnavailable(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://planner:planner@127.0.0.1:1/planner?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := postgres.Guarded{Repo: postgres.New(pool, system.IDs{}), Timeout: 2 * time.Second}

	start := time.Now()
	_, err = repo.GetSubject(context.Background(), "est-1", "mat-1")
	if !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("se esperaba ErrUnavailable, llegó %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("tardó %s: debería cortar antes del timeout", elapsed)
	}
}

// Un error del dominio no se confunde con una caída de la base.
func TestGuarded_NoEncontradoSigueSiendoNotFound(t *testing.T) {
	_, pool, ids := newService(t)
	student, _ := split(ids)
	repo := postgres.Guarded{Repo: postgres.New(pool, system.IDs{}), Timeout: postgres.DefaultTimeout}
	if _, err := repo.GetSubject(context.Background(), student, "no-existe"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("se esperaba ErrNotFound, llegó %v", err)
	}
}
