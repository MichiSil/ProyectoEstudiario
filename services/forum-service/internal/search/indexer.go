package search

import (
	"context"
	"log/slog"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// Source es lo que el indexador lee de MongoDB.
type Source interface {
	// ChangedSince devuelve los resúmenes modificados desde since, en cualquier estado.
	ChangedSince(ctx context.Context, since time.Time) ([]model.Summary, error)
	// EachApproved recorre todos los resúmenes aprobados.
	EachApproved(ctx context.Context, fn func(model.Summary) error) error
}

// Valores por defecto del indexador (ADR-006, actualización del 2026-10-30).
const (
	DefaultSyncInterval      = 5 * time.Second
	DefaultReconcileInterval = 5 * time.Minute
	// overlap es cuánto hacia atrás se vuelve a mirar en cada sincronización,
	// para no perder cambios escritos por otra instancia con el reloj un poco
	// atrasado o que terminaron de guardarse durante la lectura anterior.
	// Volver a indexar un resumen deja el índice igual, así que repetir no cuesta.
	overlap = 30 * time.Second
	batch   = 200
)

// Indexer mantiene el core summaries igual a MongoDB:
//
//   - cada SyncInterval lee de MongoDB los resúmenes modificados (por
//     updatedAt): si están aprobados los agrega o reemplaza, y si no, los
//     borra del índice;
//   - cada ReconcileInterval compara todo MongoDB con todo el índice y corrige
//     lo que falte, sobre o tenga otra calificación.
//
// Nunca usa datos viejos: siempre indexa el estado actual del resumen, así que
// procesar dos veces lo mismo, o en otro orden, deja el índice igual. Con dos
// instancias de forum-service las dos indexan, y el resultado es el mismo.
type Indexer struct {
	Solr              *Solr
	Source            Source
	Logger            *slog.Logger
	SyncInterval      time.Duration
	ReconcileInterval time.Duration
	Now               func() time.Time
}

func (ix *Indexer) now() time.Time {
	if ix.Now != nil {
		return ix.Now().UTC()
	}
	return time.Now().UTC()
}

// Run prepara el esquema y sincroniza hasta que se cancele ctx. Si Solr no
// está, espera y reintenta sin frenar el resto del servicio: mientras tanto
// la búsqueda responde 503 y el foro sigue andando (ADR-010).
func (ix *Indexer) Run(ctx context.Context) {
	syncEvery, reconcileEvery := ix.SyncInterval, ix.ReconcileInterval
	if syncEvery <= 0 {
		syncEvery = DefaultSyncInterval
	}
	if reconcileEvery <= 0 {
		reconcileEvery = DefaultReconcileInterval
	}

	wait := time.Second
	for {
		err := ix.Solr.EnsureSchema(ctx)
		if err == nil {
			break
		}
		ix.Logger.Warn("Solr no disponible para aplicar el esquema, reintentando", "waitMs", wait.Milliseconds(), "error", err.Error())
		if !sleep(ctx, wait) {
			return
		}
		wait = min(wait*2, 30*time.Second)
	}
	ix.Logger.Info("esquema de Solr aplicado")

	// Al arrancar, una reconciliación completa pone al día todo lo que pasó
	// mientras el servicio estuvo apagado. Después alcanza con los cambios.
	watermark := ix.now()
	if err := ix.Reconcile(ctx); err != nil {
		ix.Logger.Warn("falló la reconciliación inicial del índice", "error", err.Error())
		watermark = time.Time{} // la próxima sincronización mira todo
	}
	lastReconcile := ix.now()

	ticker := time.NewTicker(syncEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if ix.now().Sub(lastReconcile) >= reconcileEvery {
			if err := ix.Reconcile(ctx); err != nil {
				ix.Logger.Warn("falló la reconciliación del índice", "error", err.Error())
			} else {
				lastReconcile = ix.now()
			}
		}
		started := ix.now()
		if err := ix.Sync(ctx, watermark.Add(-overlap)); err != nil {
			// No se avanza la marca: la próxima vuelta reintenta lo mismo.
			ix.Logger.Warn("falló la sincronización del índice", "error", err.Error())
			continue
		}
		watermark = started
	}
}

// Sync indexa los resúmenes modificados desde since. Los que cambiaron
// antes de since + overlap ya se indexaron en la vuelta anterior y se repiten
// sólo por seguridad: no se cuentan en el log ni en el retraso.
func (ix *Indexer) Sync(ctx context.Context, since time.Time) error {
	changed, err := ix.Source.ChangedSince(ctx, since)
	if err != nil {
		return err
	}
	var upserts []Doc
	var deletes []string
	var indexed, removed int
	var maxLag time.Duration
	now, fresh := ix.now(), since.Add(overlap)
	for _, s := range changed {
		isNew := !s.UpdatedAt.Before(fresh)
		if s.Status == model.StatusApproved {
			upserts = append(upserts, ToDoc(s))
			if isNew {
				indexed++
				if s.PublishedAt != nil {
					maxLag = max(maxLag, now.Sub(*s.PublishedAt))
				}
			}
		} else {
			deletes = append(deletes, s.ID)
			if isNew {
				removed++
			}
		}
	}
	if err := ix.upsert(ctx, upserts); err != nil {
		return err
	}
	if err := ix.Solr.Delete(ctx, deletes); err != nil {
		return err
	}
	if indexed+removed > 0 {
		// lagMs: cuánto pasó desde la aprobación más vieja de esta tanda hasta
		// que se mandó a Solr (sin los ≤ 5 s de commitWithin). Sirve para medir
		// RN-26: un aprobado aparece en menos de 1 minuto.
		ix.Logger.Info("índice sincronizado", "indexed", indexed, "removed", removed, "lagMs", maxLag.Milliseconds())
	}
	return nil
}

// Reconcile compara MongoDB con el índice y corrige las diferencias. Devuelve
// error sin corregir nada si no puede leer alguno de los dos.
func (ix *Indexer) Reconcile(ctx context.Context) error {
	indexed, err := ix.Solr.All(ctx)
	if err != nil {
		return err
	}
	var added, updated int
	var pending []Doc
	err = ix.Source.EachApproved(ctx, func(s model.Summary) error {
		hit, ok := indexed[s.ID]
		delete(indexed, s.ID)
		switch {
		case !ok:
			added++
		case hit.RatingCount != s.Rating.Count || float32(hit.RatingAvg) != float32(s.Rating.Average):
			// La calificación sólo llega al índice por acá (ADR-006: hasta 5 min).
			updated++
		default:
			return nil
		}
		pending = append(pending, ToDoc(s))
		if len(pending) >= batch {
			err := ix.Solr.Upsert(ctx, pending)
			pending = pending[:0]
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := ix.Solr.Upsert(ctx, pending); err != nil {
		return err
	}
	// Lo que quedó en el índice ya no está aprobado en MongoDB.
	stale := make([]string, 0, len(indexed))
	for id := range indexed {
		stale = append(stale, id)
	}
	if err := ix.Solr.Delete(ctx, stale); err != nil {
		return err
	}
	ix.Logger.Info("índice reconciliado", "added", added, "updated", updated, "removed", len(stale))
	return nil
}

// Rebuild vacía el índice y lo vuelve a armar desde MongoDB (ADR-006, 4):
// cuando se pierde el volumen de Solr o cambia el esquema.
func (ix *Indexer) Rebuild(ctx context.Context) (int, error) {
	if err := ix.Solr.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	if err := ix.Solr.DeleteAll(ctx); err != nil {
		return 0, err
	}
	total := 0
	var pending []Doc
	err := ix.Source.EachApproved(ctx, func(s model.Summary) error {
		total++
		pending = append(pending, ToDoc(s))
		if len(pending) >= batch {
			err := ix.Solr.Upsert(ctx, pending)
			pending = pending[:0]
			return err
		}
		return nil
	})
	if err != nil {
		return total, err
	}
	if err := ix.Solr.Upsert(ctx, pending); err != nil {
		return total, err
	}
	return total, ix.Solr.Commit(ctx)
}

func (ix *Indexer) upsert(ctx context.Context, docs []Doc) error {
	for start := 0; start < len(docs); start += batch {
		if err := ix.Solr.Upsert(ctx, docs[start:min(start+batch, len(docs))]); err != nil {
			return err
		}
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}
