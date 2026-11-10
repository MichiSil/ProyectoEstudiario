package minio_test

// Tests de integración contra un MinIO real, con el usuario propio de
// planner-service. Se ejecutan sólo si está definida PLANNER_TEST_MINIO_ENDPOINT.
// Los puertos de MinIO no se publican fuera de Docker, así que se corren en un
// contenedor conectado a la red de docker compose:
//
//	docker run --rm --network estudiario_default -v "$PWD/services/planner-service:/src" -w /src \
//	  -e PLANNER_TEST_MINIO_ENDPOINT=minio:9000 golang:1.26-alpine go test ./internal/adapters/minio/

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/minio"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

func newStore(t *testing.T, bucket string) *minio.Store {
	t.Helper()
	endpoint := os.Getenv("PLANNER_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("PLANNER_TEST_MINIO_ENDPOINT no definida: se omiten los tests de integración con MinIO")
	}
	access, secret := os.Getenv("PLANNER_TEST_MINIO_ACCESS_KEY"), os.Getenv("PLANNER_TEST_MINIO_SECRET_KEY")
	if access == "" {
		access, secret = "planner-service", "planner-minio-local" // valores de .env.example
	}
	s, err := minio.New(endpoint, access, secret, bucket, false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSubirLeerYBorrarUnPDF(t *testing.T) {
	store, ctx := newStore(t, "materials"), context.Background()
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	key := "test-" + strings.ToLower(rand.Text()) + ".pdf"
	data := []byte("%PDF-1.4\nmaterial de prueba\n%%EOF")
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
		t.Fatalf("se leyó %q", got)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("después de borrar: err = %v, se esperaba ErrNotFound", err)
	}
}

// El usuario de planner-service sólo puede usar materials (ADR-003): no lee
// ni escribe los PDF del foro.
func TestSoloElBucketMaterials(t *testing.T) {
	foro, ctx := newStore(t, "summaries"), context.Background()
	data := []byte("%PDF-1.4")
	if err := foro.Put(ctx, "test-ajeno.pdf", bytes.NewReader(data), int64(len(data)), "application/pdf"); err == nil {
		_ = foro.Delete(ctx, "test-ajeno.pdf")
		t.Fatal("el usuario de planner-service pudo escribir en el bucket summaries")
	}
}
