// Package repository es la capa de datos de users-service sobre PostgreSQL.
package repository

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

//go:embed schema.sql
var schema string

// DefaultTimeout es el tiempo máximo de cada consulta a PostgreSQL (ADR-010).
const DefaultTimeout = 3 * time.Second

// Postgres guarda los usuarios en la base `users`. Cada consulta tiene un
// tiempo máximo y, si la base no responde, devuelve model.ErrUnavailable, que
// la API informa como 503 (ADR-010).
type Postgres struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func New(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool, timeout: DefaultTimeout}
}

// Migrate aplica el esquema. Es idempotente.
func (p *Postgres) Migrate(ctx context.Context) error {
	return p.run(ctx, func(ctx context.Context) error {
		_, err := p.pool.Exec(ctx, schema)
		return err
	})
}

func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

func (p *Postgres) Create(ctx context.Context, u model.User) error {
	return p.run(ctx, func(ctx context.Context) error {
		_, err := p.pool.Exec(ctx,
			`INSERT INTO users (id, name, email, password_hash, role, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			u.ID, u.Name, u.Email, u.PasswordHash, u.Role, u.CreatedAt)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation sobre email (RN-01)
			return model.ErrEmailInUse
		}
		return err
	})
}

// UpdateRole cambia el rol de un usuario (se recalcula en cada login, RF-03).
func (p *Postgres) UpdateRole(ctx context.Context, id, role string) error {
	return p.run(ctx, func(ctx context.Context) error {
		tag, err := p.pool.Exec(ctx, `UPDATE users SET role = $2 WHERE id = $1`, id, role)
		if err == nil && tag.RowsAffected() == 0 {
			return model.ErrNotFound
		}
		return err
	})
}

func (p *Postgres) GetByEmail(ctx context.Context, email string) (model.User, error) {
	return p.getOne(ctx, `WHERE email = $1`, email)
}

func (p *Postgres) GetByID(ctx context.Context, id string) (model.User, error) {
	return p.getOne(ctx, `WHERE id = $1`, id)
}

func (p *Postgres) getOne(ctx context.Context, where string, arg string) (u model.User, err error) {
	err = p.run(ctx, func(ctx context.Context) error {
		err := p.pool.QueryRow(ctx,
			`SELECT id, name, email, password_hash, role, created_at FROM users `+where, arg).
			Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	})
	return u, err
}

func (p *Postgres) run(ctx context.Context, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return classify(fn(ctx))
}

// classify distingue los errores de disponibilidad de la base (503) de los del
// pedido, igual que planner-service.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var connErr *pgconn.ConnectError
	var netErr net.Error
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, context.DeadlineExceeded), pgconn.Timeout(err):
		return fmt.Errorf("%w: la base no respondió a tiempo: %v", model.ErrUnavailable, err)
	case errors.As(err, &connErr), errors.As(err, &netErr), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return fmt.Errorf("%w: no se pudo conectar con la base: %v", model.ErrUnavailable, err)
	case errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "57") || strings.HasPrefix(pgErr.Code, "08")):
		// Clase 57: la base se está apagando o reiniciando. Clase 08: error de conexión.
		return fmt.Errorf("%w: la base no está disponible: %v", model.ErrUnavailable, err)
	}
	return err
}
