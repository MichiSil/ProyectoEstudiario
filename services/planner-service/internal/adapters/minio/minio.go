// Package minio es el adaptador de app.MaterialFiles: guarda los PDF del
// material de las materias en MinIO, en el bucket materials, con el usuario
// propio de planner-service (ADR-003).
package minio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// Tiempos máximos contra MinIO, los mismos que forum-service (ADR-010).
const (
	UploadTimeout   = 10 * time.Second
	DownloadTimeout = 5 * time.Second
)

// Store guarda y lee los archivos de un bucket.
type Store struct {
	client *minio.Client
	bucket string
}

var _ app.MaterialFiles = (*Store)(nil)

// New conecta con MinIO.
func New(endpoint, accessKey, secretKey, bucket string, secure bool) (*Store, error) {
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: secure})
	if err != nil {
		return nil, err
	}
	return &Store{client: client, bucket: bucket}, nil
}

// Ping verifica que el bucket exista y se pueda acceder.
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return classify(err)
	}
	if !ok {
		return fmt.Errorf("%w: no existe el bucket %s", domain.ErrUnavailable, s.bucket)
	}
	return nil
}

func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, UploadTimeout)
	defer cancel()
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return classify(err)
}

// Get abre un archivo para leerlo. Quien lo llama tiene que cerrarlo.
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err == nil {
		// GetObject no va a MinIO hasta la primera lectura: Stat confirma que
		// el archivo existe antes de empezar a responder.
		_, err = obj.Stat()
	}
	if err != nil {
		cancel()
		if obj != nil {
			_ = obj.Close()
		}
		return nil, classify(err)
	}
	return &cancelOnClose{ReadCloser: obj, cancel: cancel}, nil
}

// Delete borra un archivo. Si no existe, no es un error.
func (s *Store) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	return classify(s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}))
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	defer c.cancel()
	return c.ReadCloser.Close()
}

// classify traduce los errores de MinIO: archivo inexistente → ErrNotFound;
// sin conexión, timeout o error del servidor → ErrUnavailable (503).
func classify(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" {
		return domain.ErrNotFound
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) || resp.StatusCode >= 500 {
		return fmt.Errorf("%w: MinIO: %v", domain.ErrUnavailable, err)
	}
	return err
}
