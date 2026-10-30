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
)

// Repository envuelve al repositorio: el detalle se lee con cache-aside. El
// resto de los métodos pasa directo.
type Repository struct {
	service.Repository
	Cache   cache.Cache
	Metrics *Metrics
	Logger  *slog.Logger
}

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
	if r.read(ctx, OpDetail, key, &s) {
		return s, nil
	}
	s, err := r.Repository.Get(ctx, id)
	if err != nil {
		return s, err
	}
	if s.Status == model.StatusApproved {
		r.write(ctx, key, cacheable(s), DetailTTL)
	}
	return s, nil
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
	if r.read(ctx, OpList, key, &page) {
		return page, nil
	}
	page, err := s.Next.Search(ctx, q)
	if err != nil {
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

// read busca la clave y la decodifica en v. Un fallo, un error o una entrada
// que no se puede leer cuentan como fallo: se lee de MongoDB.
func (r *Repository) read(ctx context.Context, op, key string, v any) bool {
	raw, err := r.Cache.Get(key)
	switch {
	case errors.Is(err, cache.ErrCacheMiss):
		r.Metrics.add(op, ResultMiss)
		return false
	case err != nil:
		r.Metrics.add(op, ResultError)
		r.cacheError(ctx, op, "get", err)
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		r.Metrics.add(op, ResultMiss)
		r.Logger.WarnContext(ctx, "entrada de la caché ilegible: se lee de MongoDB", "operation", op, "error", err.Error())
		return false
	}
	r.Metrics.add(op, ResultHit)
	return true
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
