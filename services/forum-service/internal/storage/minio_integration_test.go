package storage_test

// Tests de integración contra un MinIO real (requisito 2.11 del enunciado), con
// el usuario propio de forum-service. Se ejecutan sólo si está definida
// FORUM_TEST_MINIO_ENDPOINT:
//
//	docker compose up -d minio minio-init
//	FORUM_TEST_MINIO_ENDPOINT=localhost:9000 go test ./internal/storage/

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/storage"
)

func newStore(t *testing.T) *storage.MinIO {
	t.Helper()
	endpoint := os.Getenv("FORUM_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("FORUM_TEST_MINIO_ENDPOINT no definida: se omiten los tests de integración con MinIO")
	}
	access, secret := os.Getenv("FORUM_TEST_MINIO_ACCESS_KEY"), os.Getenv("FORUM_TEST_MINIO_SECRET_KEY")
	if access == "" {
		access, secret = "forum-service", "forum-minio-local" // valores de .env.example
	}
	s, err := storage.NewMinIO(endpoint, access, secret, "summaries", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return s
}

func TestSubirLeerYBorrarUnPDF(t *testing.T) {
	store, ctx := newStore(t), context.Background()
	key := "test-" + strings.ToLower(rand.Text()) + ".pdf"
	data := []byte("%PDF-1.4\ncontenido de prueba\n%%EOF")

	if err := store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/pdf"); err != nil {
		t.Fatal(err)
	}
	r, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("se leyó %q, se esperaba %q", got, data)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("después de borrar: err = %v, se esperaba ErrNotFound", err)
	}
}
