package cached_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/cache"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/cached"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service/memory"
)

// counting cuenta las lecturas que llegan a la "base".
type counting struct {
	*memory.Store
	mu            sync.Mutex
	gets, searchs int
}

func (c *counting) Get(ctx context.Context, id string) (model.Summary, error) {
	c.mu.Lock()
	c.gets++
	c.mu.Unlock()
	return c.Store.Get(ctx, id)
}

func (c *counting) Search(ctx context.Context, q model.SearchQuery) (model.Page, error) {
	c.mu.Lock()
	c.searchs++
	c.mu.Unlock()
	return c.Store.Search(ctx, q)
}

func (c *counting) reads() (gets, searchs int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gets, c.searchs
}

var (
	student = service.Viewer{UserID: "u-1", Name: "Ana"}
	admin   = service.Viewer{UserID: "adm", Admin: true}
	anyone  = service.Viewer{UserID: "u-3"}
)

type env struct {
	svc     *service.Service
	db      *counting
	repo    *cached.Repository
	metrics *cached.Metrics
}

func newEnv(t *testing.T, c cache.Cache) *env {
	t.Helper()
	db := &counting{Store: memory.NewStore()}
	m := &cached.Metrics{}
	repo := &cached.Repository{Repository: db, Cache: c, Metrics: m,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return &env{
		svc: &service.Service{Repo: repo, Search: &cached.Search{Next: db, Repo: repo}, Files: memory.NewFiles()},
		db:  db, repo: repo, metrics: m,
	}
}

// approved publica un resumen de student y lo aprueba.
func (e *env) approved(t *testing.T, title, category string) string {
	t.Helper()
	ctx := context.Background()
	doc, err := e.svc.Publish(ctx, service.NewDocument{
		Title: title, Category: category,
		Author: model.Author{UserID: student.UserID, Name: student.Name, Email: "ana@example.com"},
		Source: model.SourceEstudiario,
	}, service.File{Name: "a.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4 hola")})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Approve(ctx, admin, doc.ID); err != nil {
		t.Fatal(err)
	}
	return doc.ID
}

func (e *env) list(t *testing.T, category string) model.Page {
	t.Helper()
	q := model.SearchQuery{Category: model.NormalizeKey(category), Page: 1, Size: 20}
	p, err := e.svc.SearchApproved(context.Background(), q)
	if err != nil {
		t.Fatalf("listado: %v", err)
	}
	return p
}

func ids(p model.Page) []string {
	out := []string{}
	for _, s := range p.Items {
		out = append(out, s.ID)
	}
	return out
}

func TestDetalleAprobadoSeLeeDeLaCache(t *testing.T) {
	e := newEnv(t, cache.NewLocal())
	id := e.approved(t, "Integrales dobles", "Análisis II")
	gets0, _ := e.db.reads()

	for i := range 3 {
		doc, err := e.svc.Get(context.Background(), anyone, id)
		if err != nil || doc.Title != "Integrales dobles" {
			t.Fatalf("detalle = %+v, %v", doc, err)
		}
		if i > 0 && doc.Author.Email != "" {
			t.Error("el mail del autor no se guarda en la caché")
		}
	}
	if gets, _ := e.db.reads(); gets-gets0 != 1 {
		t.Errorf("lecturas a la base = %d, se esperaba 1 (las otras dos, de la caché)", gets-gets0)
	}
	if hits := e.metrics.Count(cached.OpDetail, cached.ResultHit); hits != 2 {
		t.Errorf("aciertos = %d, se esperaban 2", hits)
	}
}

// Sólo se guardan aprobados: un pendiente se lee siempre de la base.
func TestPendienteNoSeGuarda(t *testing.T) {
	e := newEnv(t, cache.NewLocal())
	doc, err := e.svc.Publish(context.Background(), service.NewDocument{
		Title: "Pendiente", Category: "Física I", Author: model.Author{UserID: student.UserID, Name: "Ana"},
		Source: model.SourceEstudiario,
	}, service.File{ContentType: "application/pdf", Data: []byte("%PDF-1.4")})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := e.svc.Get(context.Background(), student, doc.ID); err != nil {
			t.Fatal(err)
		}
	}
	if gets, _ := e.db.reads(); gets != 2 {
		t.Errorf("lecturas a la base = %d, se esperaban 2", gets)
	}
	if hits := e.metrics.Count(cached.OpDetail, cached.ResultHit); hits != 0 {
		t.Errorf("aciertos = %d, un pendiente no se cachea", hits)
	}
}

func TestListadoSeLeeDeLaCache(t *testing.T) {
	e := newEnv(t, cache.NewLocal())
	e.approved(t, "Integrales dobles", "Análisis II")
	e.approved(t, "Leyes de Newton", "Física I")

	for range 3 {
		if p := e.list(t, ""); len(p.Items) != 2 {
			t.Fatalf("listado = %v", ids(p))
		}
	}
	if p := e.list(t, "física i"); len(p.Items) != 1 {
		t.Fatalf("listado de Física I = %v", ids(p))
	}
	if _, searchs := e.db.reads(); searchs != 2 {
		t.Errorf("búsquedas en la base = %d, se esperaban 2 (una por materia)", searchs)
	}
	if hits := e.metrics.Count(cached.OpList, cached.ResultHit); hits != 2 {
		t.Errorf("aciertos = %d, se esperaban 2", hits)
	}
}

// La búsqueda por texto no se cachea (la resuelve Solr, ADR-007, 1).
func TestBusquedaPorTextoNoSeCachea(t *testing.T) {
	e := newEnv(t, cache.NewLocal())
	e.approved(t, "Integrales dobles", "Análisis II")
	for range 2 {
		if _, err := e.svc.SearchApproved(context.Background(), model.SearchQuery{Text: "integrales", Page: 1, Size: 20}); err != nil {
			t.Fatal(err)
		}
	}
	if _, searchs := e.db.reads(); searchs != 2 {
		t.Errorf("búsquedas en la base = %d, se esperaban 2", searchs)
	}
}

// La caché nula (la de los tests y la medición "sin caché") siempre va a la base.
func TestCacheNulaSiempreVaALaBase(t *testing.T) {
	e := newEnv(t, cache.NewNoop())
	id := e.approved(t, "Integrales dobles", "Análisis II")
	gets0, _ := e.db.reads()
	for range 3 {
		if _, err := e.svc.Get(context.Background(), anyone, id); err != nil {
			t.Fatal(err)
		}
		e.list(t, "")
	}
	gets, searchs := e.db.reads()
	if gets-gets0 != 3 || searchs != 3 {
		t.Errorf("lecturas = %d detalle y %d listado, se esperaban 3 y 3", gets-gets0, searchs)
	}
}
