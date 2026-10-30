// Package storage guarda los PDF de los resúmenes en MinIO (bucket summaries,
// ADR-003), a través de su API compatible con S3.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// Tiempos máximos contra MinIO (ADR-010).
const (
	UploadTimeout   = 10 * time.Second
	DownloadTimeout = 5 * time.Second
)

// MinIO guarda y lee los archivos de un bucket.
type MinIO struct {
	client *minio.Client
	bucket string
}

// NewMinIO conecta con MinIO con el usuario propio de forum-service.
func NewMinIO(endpoint, accessKey, secretKey, bucket string, secure bool) (*MinIO, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, err
	}
	return &MinIO{client: client, bucket: bucket}, nil
}

// Ping verifica que el bucket exista y se pueda acceder.
func (m *MinIO) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	ok, err := m.client.BucketExists(ctx, m.bucket)
	if err != nil {
		return classify(err)
	}
	if !ok {
		return fmt.Errorf("%w: no existe el bucket %s", model.ErrUnavailable, m.bucket)
	}
	return nil
}

// Put sube un archivo completo.
func (m *MinIO) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, UploadTimeout)
	defer cancel()
	_, err := m.client.PutObject(ctx, m.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return classify(err)
}

// Get abre un archivo para leerlo. Quien lo llama tiene que cerrarlo.
func (m *MinIO) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err == nil {
		// GetObject no va a MinIO hasta la primera lectura: Stat confirma que el
		// archivo existe antes de empezar a responder.
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
func (m *MinIO) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	return classify(m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{}))
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
		return model.ErrNotFound
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) || resp.StatusCode >= 500 {
		return fmt.Errorf("%w: MinIO: %v", model.ErrUnavailable, err)
	}
	return err
}
