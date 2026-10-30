package cleanup_test

// Test de integración de la limpieza de PDF huérfanos contra MinIO y MongoDB
// reales. Se ejecuta sólo si están definidas FORUM_TEST_MONGO_URI y
// FORUM_TEST_MINIO_ENDPOINT. Como sus puertos no se publican fuera de Docker,
// se corre en un contenedor conectado a la red de docker compose (ver el README):
//
//	docker run --rm --network estudiario_default -v "$PWD/services/forum-service:/src" -w /src \
//	  -e FORUM_TEST_MONGO_URI='mongodb://forum_svc:forum-local@mongo:27017/forum?authSource=forum' \
//	  -e FORUM_TEST_MINIO_ENDPOINT=minio:9000 golang:1.26-alpine go test ./internal/cleanup/

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/cleanup"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/repository"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/storage"
)

// testAuthor marca los documentos de este test. Es distinta de la de los tests
// del repositorio, que corren en paralelo y borran los suyos al terminar.
const testAuthor = "test-limpieza"

func setup(t *testing.T) (*repository.Mongo, *storage.MinIO, string) {
	t.Helper()
	uri, endpoint := os.Getenv("FORUM_TEST_MONGO_URI"), os.Getenv("FORUM_TEST_MINIO_ENDPOINT")
	if uri == "" || endpoint == "" {
		t.Skip("FORUM_TEST_MONGO_URI o FORUM_TEST_MINIO_ENDPOINT no definidas: se omite el test de integración de la limpieza")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("forum")
	repo := repository.New(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Collection("summaries").DeleteMany(context.Background(), bson.D{{Key: "author.name", Value: testAuthor}})
		_ = client.Disconnect(context.Background())
	})

	access, secret := os.Getenv("FORUM_TEST_MINIO_ACCESS_KEY"), os.Getenv("FORUM_TEST_MINIO_SECRET_KEY")
	if access == "" {
		access, secret = "forum-service", "forum-minio-local" // valores de .env.example
	}
	files, err := storage.NewMinIO(endpoint, access, secret, "summaries", false)
	if err != nil {
		t.Fatal(err)
	}
	// Prefijo propio: la limpieza no toca archivos de otros tests ni del foro.
	prefix := "test-limpieza-" + strings.ToLower(rand.Text()) + "/"
	t.Cleanup(func() {
		_ = files.List(context.Background(), prefix, func(key string, _ time.Time) error {
			return files.Delete(context.Background(), key)
		})
	})
	return repo, files, prefix
}

func put(t *testing.T, files *storage.MinIO, key string) {
	t.Helper()
	data := []byte("%PDF-1.4 prueba")
	if err := files.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), "application/pdf"); err != nil {
		t.Fatal(err)
	}
}

func exists(files *storage.MinIO, key string) bool {
	r, err := files.Get(context.Background(), key)
	if err == nil {
		_ = r.Close()
		return true
	}
	if !errors.Is(err, model.ErrNotFound) {
		panic(err)
	}
	return false
}

func withDocument(t *testing.T, repo *repository.Mongo, key string, status model.Status) {
	t.Helper()
	now := time.Now().UTC()
	doc := model.Summary{
		ID: strings.ToLower(rand.Text()), Title: "Prueba de limpieza", Category: "Pruebas", CategoryKey: "pruebas",
		Author: model.Author{UserID: "u", Name: testAuthor}, Source: model.SourceEstudiario, Status: status,
		File: model.FileRef{Key: key, ContentType: "application/pdf", SizeBytes: 15}, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
}

func TestLimpiezaBorraSoloLosPDFSinDocumento(t *testing.T) {
	repo, files, prefix := setup(t)
	huerfano, conDoc, eliminado := prefix+"huerfano.pdf", prefix+"con-documento.pdf", prefix+"eliminado.pdf"
	for _, k := range []string{huerfano, conDoc, eliminado} {
		put(t, files, k)
	}
	withDocument(t, repo, conDoc, model.StatusApproved)
	withDocument(t, repo, eliminado, model.StatusDeleted) // un eliminado conserva su documento y su PDF

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// Con la antigüedad mínima de 1 hora, los recién subidos no se tocan.
	c := &cleanup.Cleaner{Files: files, Docs: repo, Logger: logger, MinAge: time.Hour, Prefix: prefix}
	if reviewed, deleted, err := c.RunOnce(context.Background()); err != nil || reviewed != 0 || deleted != 0 {
		t.Fatalf("recién subidos: revisados %d, borrados %d, err %v; no se tenía que revisar ninguno", reviewed, deleted, err)
	}
	if !exists(files, huerfano) {
		t.Fatal("se borró un PDF recién subido")
	}

	// Una hora después, el huérfano se borra y los que tienen documento quedan.
	c.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	reviewed, deleted, err := c.RunOnce(context.Background())
	if err != nil || reviewed != 3 || deleted != 1 {
		t.Fatalf("revisados %d, borrados %d, err %v; se esperaban 3 revisados y 1 borrado", reviewed, deleted, err)
	}
	if exists(files, huerfano) {
		t.Error("el PDF huérfano sigue en el bucket")
	}
	if !exists(files, conDoc) || !exists(files, eliminado) {
		t.Error("se borró un PDF que tiene documento (aprobado o eliminado)")
	}

	// Otra vuelta (u otra instancia a la vez) no cambia nada.
	if _, deleted, err := c.RunOnce(context.Background()); err != nil || deleted != 0 {
		t.Errorf("segunda vuelta: borrados %d, err %v", deleted, err)
	}
}
