// Package cached es el envoltorio de caché de forum-service (ADR-007): envuelve
// al repositorio y a la búsqueda con cache-aside, para el detalle de un
// resumen aprobado y las páginas del listado de aprobados. El service no sabe
// si la respuesta vino de la caché.
//
// La caché nunca es fuente de verdad ni un motivo de error: si Memcached no
// responde (timeout de 100 ms, ADR-010), se lee de MongoDB como si fuera un
// fallo (cache miss).
package cached

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/cache"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
)

// Vigencia de cada entrada (ADR-007, 4).
const (
	DetailTTL = 10 * time.Minute
	ListTTL   = 60 * time.Second
	GenTTL    = 24 * time.Hour
)

// Claves (ADR-007, 3). v1 es la versión del formato guardado: si cambia, se
// pasa a v2 y las entradas viejas dejan de leerse solas.
const (
	prefix = "forum:v1:"
	genKey = prefix + "list:gen"
	// maxKeyLen es el largo máximo de una clave de Memcached.
	maxKeyLen = 250
)

// Operaciones y escrituras, como aparecen en las métricas y los logs.
const (
	OpDetail = "detalle"
	OpList   = "listado"

	WriteApprove = "aprobar"
	WriteDelete  = "eliminar"
	WriteRate    = "calificar"
	// WriteIndex: el indexador llevó a Solr un cambio (ver InvalidateList).
	WriteIndex = "indexar"
)

// Repository envuelve al repositorio: el detalle se lee con cache-aside y las
// escrituras que cambian lo que se ve en el foro invalidan la caché después de
// que MongoDB las confirma (ADR-007, 4). El resto de los métodos pasa directo.
type Repository struct {
	service.Repository
	Cache   cache.Cache
	Metrics *Metrics
	Logger  *slog.Logger
	// DeleteRetries son las esperas antes de reintentar la invalidación de un
	// resumen eliminado, si Memcached no respondió (ADR-007, 5).
	DeleteRetries []time.Duration
}

// DefaultDeleteRetries: hasta 3 reintentos en los segundos siguientes.
var DefaultDeleteRetries = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// Search envuelve a la búsqueda: las páginas del listado (sin texto ni otros
// filtros que la materia) se leen con cache-aside. La búsqueda por texto no se
// cachea (ADR-007, 1).
type Search struct {
	Next service.SummarySearch
	Repo *Repository // comparte la caché, las métricas y la generación del listado
}

func detailKey(id string) string { return prefix + "summary:" + id }

// Get devuelve el resumen. Sólo los aprobados se guardan en la caché: un
// pendiente, rechazado o eliminado siempre se lee de MongoDB (ADR-007, 1).
func (r *Repository) Get(ctx context.Context, id string) (model.Summary, error) {
	key := detailKey(id)
	if !validKey(key) {
		return r.Repository.Get(ctx, id)
	}
	var s model.Summary
	hit, down := r.read(ctx, OpDetail, key, &s)
	if hit {
		return s, nil
	}
	s, err := r.Repository.Get(ctx, id)
	if err != nil {
		return s, err
	}
	// Si Memcached no respondió al leer, no se espera otro timeout para guardar.
	if s.Status == model.StatusApproved && !down {
		r.write(ctx, key, cacheable(s), DetailTTL)
	}
	return s, nil
}

// Moderate aprueba o rechaza. Aprobar cambia el listado: nueva generación. Un
// pendiente nunca está en la caché, así que el detalle no se toca.
func (r *Repository) Moderate(ctx context.Context, id string, mod model.Moderation) error {
	if err := r.Repository.Moderate(ctx, id, mod); err != nil {
		return err
	}
	if mod.To == model.StatusApproved {
		r.invalidateList(ctx, WriteApprove)
	}
	return nil
}

// Delete elimina y borra el detalle y el listado de la caché. Si Memcached no
// responde, el borrado del detalle se reintenta en segundo plano para no
// seguir mostrando un resumen eliminado (RN-24, ADR-007, 5).
func (r *Repository) Delete(ctx context.Context, id, by string, at time.Time, event model.OutboxEvent) error {
	if err := r.Repository.Delete(ctx, id, by, at, event); err != nil {
		return err
	}
	if !r.invalidateDetail(ctx, id, WriteDelete) {
		go r.retryDelete(context.WithoutCancel(ctx), id)
	}
	r.invalidateList(ctx, WriteDelete)
	return nil
}

// Rate califica y borra el detalle, para que el promedio nuevo se vea
// enseguida. El listado no se invalida: vence solo a los 60 s. El orden por
// calificación sale de Solr, que recibe las calificaciones recién en la
// reconciliación (hasta 5 min, ADR-006), así que invalidarlo casi no cambiaba
// lo que se ve, y la medición mostró que bajaba mucho los aciertos del
// listado (ADR-007, actualización).
func (r *Repository) Rate(ctx context.Context, summaryID, userID string, score int, at time.Time) (model.Rating, error) {
	rating, err := r.Repository.Rate(ctx, summaryID, userID, score, at)
	if err != nil {
		return rating, err
	}
	r.invalidateDetail(ctx, summaryID, WriteRate)
	return rating, nil
}

// InvalidateList cambia la generación del listado. El listado sale de Solr, que
// se pone al día unos segundos después de MongoDB (ADR-006): si alguien pide
// el listado en ese rato, la página que se guarda todavía no tiene el
// resumen recién aprobado. Por eso el indexador vuelve a invalidar cuando el
// cambio ya se ve en Solr, y un aprobado no tarda en aparecer el TTL entero.
func (r *Repository) InvalidateList(ctx context.Context) {
	r.invalidateList(ctx, WriteIndex)
}

// Search devuelve una página de aprobados. Sólo el listado (por materia, orden
// y página) pasa por la caché.
func (s *Search) Search(ctx context.Context, q model.SearchQuery) (model.Page, error) {
	if q.Text != "" || len(q.Tags) > 0 || q.MinRating > 0 {
		return s.Next.Search(ctx, q)
	}
	r := s.Repo
	gen, ok := r.generation(ctx)
	if !ok {
		return s.Next.Search(ctx, q)
	}
	key := listKey(gen, q)
	var page model.Page
	hit, down := r.read(ctx, OpList, key, &page)
	if hit {
		return page, nil
	}
	page, err := s.Next.Search(ctx, q)
	if err != nil || down {
		return page, err
	}
	cp := page
	cp.Items = make([]model.Summary, 0, len(page.Items))
	for _, it := range page.Items {
		if it.Status != model.StatusApproved {
			return page, nil // nunca se guarda una página con algo no aprobado
		}
		cp.Items = append(cp.Items, cacheable(it))
	}
	r.write(ctx, key, cp, ListTTL)
	return page, nil
}

// listKey arma forum:v1:list:{gen}:{subject}:{sort}:{page}:{size}. La materia
// ya viene normalizada (NormalizeKey) y se codifica para que la clave no
// tenga espacios ni caracteres de control; si queda demasiado larga, se usa
// su hash.
func listKey(gen string, q model.SearchQuery) string {
	tail := ":" + string(q.Sort) + ":" + strconv.Itoa(q.Page) + ":" + strconv.Itoa(q.Size)
	subject := url.QueryEscape(q.Category)
	key := prefix + "list:" + gen + ":" + subject + tail
	if len(key) > maxKeyLen {
		sum := sha256.Sum256([]byte(q.Category))
		key = prefix + "list:" + gen + ":h-" + hex.EncodeToString(sum[:]) + tail
	}
	return key
}

// generation lee la generación actual del listado. Si no está (venció o
// Memcached se reinició), crea una nueva. Si Memcached no responde, devuelve
// false y el listado se lee sin caché.
func (r *Repository) generation(ctx context.Context) (string, bool) {
	raw, err := r.Cache.Get(genKey)
	switch {
	case err == nil:
		return string(raw), true
	case errors.Is(err, cache.ErrCacheMiss):
		gen := newGeneration()
		if err := r.Cache.Set(genKey, []byte(gen), GenTTL); err != nil {
			r.cacheError(ctx, OpList, "set", err)
			return "", false
		}
		return gen, true
	default:
		r.Metrics.add(OpList, ResultError)
		r.cacheError(ctx, OpList, "get", err)
		return "", false
	}
}

// newGeneration es un valor nuevo y único. No se usa "leer y sumar uno": si
// dos instancias invalidan a la vez, cualquiera de las dos escrituras deja la
// generación anterior sin uso, que es lo único que importa (ADR-007, 4).
func newGeneration() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + rand.Text()[:6]
}

// read busca la clave y la decodifica en v (hit). Un fallo, un error o una
// entrada que no se puede leer cuentan como fallo: se lee de MongoDB. down
// dice que Memcached no respondió: así cada pedido espera como mucho un
// timeout de 100 ms (ADR-007, 5).
func (r *Repository) read(ctx context.Context, op, key string, v any) (hit, down bool) {
	raw, err := r.Cache.Get(key)
	switch {
	case errors.Is(err, cache.ErrCacheMiss):
		r.Metrics.add(op, ResultMiss)
		return false, false
	case err != nil:
		r.Metrics.add(op, ResultError)
		r.cacheError(ctx, op, "get", err)
		return false, true
	}
	if err := json.Unmarshal(raw, v); err != nil {
		r.Metrics.add(op, ResultMiss)
		r.Logger.WarnContext(ctx, "entrada de la caché ilegible: se lee de MongoDB", "operation", op, "error", err.Error())
		return false, false
	}
	r.Metrics.add(op, ResultHit)
	return true, false
}

// write guarda v. Si falla, se ignora: el usuario ya tiene su respuesta.
func (r *Repository) write(ctx context.Context, key string, v any, ttl time.Duration) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	if err := r.Cache.Set(key, raw, ttl); err != nil {
		r.cacheError(ctx, "", "set", err)
	}
}

func (r *Repository) invalidateDetail(ctx context.Context, id, write string) bool {
	key := detailKey(id)
	if !validKey(key) {
		return true // nunca se guardó
	}
	if err := r.Cache.Delete(key); err != nil {
		r.Metrics.invalidation(write, ResultError)
		r.cacheError(ctx, OpDetail, "delete", err)
		return false
	}
	r.Metrics.invalidation(write, ResultOK)
	return true
}

func (r *Repository) invalidateList(ctx context.Context, write string) {
	if err := r.Cache.Set(genKey, []byte(newGeneration()), GenTTL); err != nil {
		// El listado viejo vence solo a los 60 s, el plazo de RN-26.
		r.Metrics.invalidation(write, ResultError)
		r.cacheError(ctx, OpList, "invalidate", err)
		return
	}
	r.Metrics.invalidation(write, ResultOK)
}

// retryDelete reintenta borrar el detalle de un resumen eliminado. Si igual
// falla, queda un ERROR con el id: el detalle puede seguir en la caché hasta
// 10 min, pero el PDF no se entrega porque la descarga verifica el estado en
// MongoDB (ADR-007, 5).
func (r *Repository) retryDelete(ctx context.Context, id string) {
	waits := r.DeleteRetries
	if waits == nil {
		waits = DefaultDeleteRetries
	}
	for _, wait := range waits {
		time.Sleep(wait)
		if r.invalidateDetail(ctx, id, WriteDelete) {
			r.Logger.InfoContext(ctx, "detalle de un resumen eliminado borrado de la caché al reintentar", "summaryId", id)
			return
		}
	}
	r.Logger.ErrorContext(ctx, "no se pudo borrar de la caché el detalle de un resumen eliminado", "summaryId", id)
}

func (r *Repository) cacheError(ctx context.Context, op, action string, err error) {
	r.Logger.WarnContext(ctx, "la caché no respondió: se sigue sin caché", "operation", op, "action", action, "error", err.Error())
}

// cacheable es la copia que se guarda: sin el outbox (no hace falta para leer)
// ni el mail del autor (un dato personal que nunca sale en las respuestas).
func cacheable(s model.Summary) model.Summary {
	s.Outbox = nil
	s.Author.Email = ""
	return s
}

// validKey descarta claves que Memcached no acepta: largas, con espacios o
// caracteres de control. Por ejemplo, un id inventado en la URL.
func validKey(key string) bool {
	if len(key) > maxKeyLen {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] <= ' ' || key[i] == 0x7f {
			return false
		}
	}
	return true
}
