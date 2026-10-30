// Package cleanup borra los PDF huérfanos del bucket summaries (ADR-003).
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

// Files es lo que la limpieza necesita de MinIO.
type Files interface {
	List(ctx context.Context, prefix string, fn func(key string, modified time.Time) error) error
	Delete(ctx context.Context, key string) error
}

// Documents es lo que la limpieza necesita de MongoDB.
type Documents interface {
	// ExistingFileKeys dice cuáles claves tienen un documento, en cualquier estado.
	ExistingFileKeys(ctx context.Context, keys []string) (map[string]bool, error)
}

// Cleaner borra los PDF que no tienen ningún documento con ese file.key.
//
// Con dos instancias de forum-service corriendo a la vez no pasa nada: las dos
// pueden querer borrar el mismo archivo, y borrarlo dos veces da lo mismo.
type Cleaner struct {
	Files    Files
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

// RunOnce hace una vuelta: recorre los PDF con más de MinAge de antigüedad y
// borra los que no tienen documento. Devuelve cuántos revisó y cuántos borró.
func (c *Cleaner) RunOnce(ctx context.Context) (reviewed, deleted int, err error) {
	ctx, cancel := context.WithTimeout(ctx, roundTimeout)
	defer cancel()

	minAge := c.MinAge
	if minAge < 0 {
		minAge = DefaultMinAge
	}
	started := c.now()
	cutoff := started.Add(-minAge)

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
			if err := c.Files.Delete(ctx, key); err != nil {
				return err
			}
			deleted++
			c.Logger.Info("PDF huérfano borrado", "key", key)
		}
		pending = pending[:0]
		return nil
	}

	err = c.Files.List(ctx, c.Prefix, func(key string, modified time.Time) error {
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
	if err != nil {
		return reviewed, deleted, err
	}
	c.Logger.Info("limpieza de PDF huérfanos", "reviewed", reviewed, "deleted", deleted,
		"minAgeMs", minAge.Milliseconds(), "durationMs", time.Since(started).Milliseconds())
	return reviewed, deleted, nil
}
