package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		unavailable bool
	}{
		{"timeout", context.DeadlineExceeded, true},
		{"base apagándose (57P01)", &pgconn.PgError{Code: "57P01"}, true},
		{"error de conexión (08006)", fmt.Errorf("query: %w", &pgconn.PgError{Code: "08006"}), true},
		{"conexión cortada", io.ErrUnexpectedEOF, true},
		{"violación de restricción", &pgconn.PgError{Code: "23505"}, false},
		{"no encontrado", domain.ErrNotFound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errors.Is(classify(tc.err), domain.ErrUnavailable); got != tc.unavailable {
				t.Fatalf("classify(%v): no disponible = %v, se esperaba %v", tc.err, got, tc.unavailable)
			}
		})
	}
}
