// Package cleanup borra los PDF huérfanos de los buckets de forum-service:
// summaries y public-documents (ADR-003, ADR-014).
//
// Publicar un resumen escribe en dos almacenamientos sin una transacción
// común: primero el PDF en MinIO y después el documento en MongoDB. Si la
// creación del documento falla y tampoco se puede borrar el archivo en ese
// momento, el PDF queda en el bucket sin ningún resumen que lo use. Esta
// tarea periódica los encuentra y los borra.
package cleanup

import (
	"context"
	"log/slog"
	"time"
)

// Valores por defecto (ADR-003). Se pueden bajar por variable de entorno para
// probarlo (FORUM_CLEANUP_INTERVAL y FORUM_CLEANUP_MIN_AGE).
const (
	DefaultInterval = time.Hour
	// DefaultMinAge evita borrar un PDF que se está subiendo en este momento y
	// todavía no tiene su documento: sólo se miran los de más de 1 hora.
	DefaultMinAge = time.Hour
	// roundTimeout es el tiempo máximo de una vuelta completa.
	roundTimeout = 5 * time.Minute
	batch        = 200
)

// Files es lo que la limpieza necesita de un bucket de MinIO.
type Files interface {
	Bucket() string
	List(ctx context.Context, prefix string, fn func(key string, modified time.Time) error) error
	Delete(ctx context.Context, key string) error
}

// Documents es lo que la limpieza necesita de MongoDB.
type Documents interface {
	// ExistingFileKeys dice cuáles claves tienen un documento, en cualquier estado.
	ExistingFileKeys(ctx context.Context, keys []string) (map[string]bool, error)
}

// Cleaner borra los PDF que no tienen ningún documento con ese file.key, en
// cada uno de los buckets. Las claves son únicas entre los dos buckets (el id
// del documento), así que alcanza con buscar la clave sin mirar el bucket.
//
// Con dos instancias de forum-service corriendo a la vez no pasa nada: las dos
// pueden querer borrar el mismo archivo, y borrarlo dos veces da lo mismo.
type Cleaner struct {
	Buckets  []Files
	Docs     Documents
	Logger   *slog.Logger
	Interval time.Duration
	MinAge   time.Duration
	// Prefix limita la limpieza a las claves que empiezan así (vacío: todo el
	// bucket). Lo usan los tests para no tocar archivos ajenos.
	Prefix string
	Now    func() time.Time
}

func (c *Cleaner) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Run limpia al arrancar y después cada Interval, hasta que se cancele ctx.
// Si MinIO o MongoDB no responden, lo loguea y lo vuelve a intentar en la
// vuelta siguiente, sin frenar el resto del servicio (ADR-010).
func (c *Cleaner) Run(ctx context.Context) {
	every := c.Interval
	if every <= 0 {
		every = DefaultInterval
	}
	for {
		if _, _, err := c.RunOnce(ctx); err != nil && ctx.Err() == nil {
			c.Logger.Warn("falló la limpieza de PDF huérfanos, se reintenta en la próxima vuelta",
				"error", err.Error(), "nextInMs", every.Milliseconds())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// RunOnce hace una vuelta: en cada bucket, recorre los PDF con más de MinAge
// de antigüedad y borra los que no tienen documento. Devuelve cuántos revisó y
// cuántos borró en total.
func (c *Cleaner) RunOnce(ctx context.Context) (reviewed, deleted int, err error) {
	ctx, cancel := context.WithTimeout(ctx, roundTimeout)
	defer cancel()

	minAge := c.MinAge
	if minAge < 0 {
		minAge = DefaultMinAge
	}
	started := c.now()
	for _, files := range c.Buckets {
		r, d, err := c.cleanBucket(ctx, files, started.Add(-minAge))
		reviewed, deleted = reviewed+r, deleted+d
		if err != nil {
			return reviewed, deleted, err
		}
		c.Logger.Info("limpieza de PDF huérfanos", "bucket", files.Bucket(), "reviewed", r, "deleted", d,
			"minAgeMs", minAge.Milliseconds(), "durationMs", time.Since(started).Milliseconds())
	}
	return reviewed, deleted, nil
}

// cleanBucket limpia un bucket: los PDF modificados antes de cutoff que no
// tienen documento.
func (c *Cleaner) cleanBucket(ctx context.Context, files Files, cutoff time.Time) (reviewed, deleted int, err error) {

	var pending []string
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		existing, err := c.Docs.ExistingFileKeys(ctx, pending)
		if err != nil {
			return err
		}
		for _, key := range pending {
			if existing[key] {
				continue
			}
			if err := files.Delete(ctx, key); err != nil {
				return err
			}
			deleted++
			c.Logger.Info("PDF huérfano borrado", "bucket", files.Bucket(), "key", key)
		}
		pending = pending[:0]
		return nil
	}

	err = files.List(ctx, c.Prefix, func(key string, modified time.Time) error {
		if modified.After(cutoff) {
			return nil // puede estar subiéndose ahora: se revisa en otra vuelta
		}
		reviewed++
		pending = append(pending, key)
		if len(pending) >= batch {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	return reviewed, deleted, err
}
