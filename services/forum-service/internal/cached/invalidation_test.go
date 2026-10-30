package cached_test

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/cache"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/cached"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
)

var rater = service.Viewer{UserID: "u-2", Name: "Beto"}

func contains(list []string, id string) bool {
	for _, s := range list {
		if s == id {
			return true
		}
	}
	return false
}

// RN-24: con el detalle y el listado ya en la caché, un resumen eliminado deja
// de verse en cuanto se elimina.
func TestEliminadoNuncaSeSigueViendo(t *testing.T) {
	e := newEnv(t, cache.NewLocal())
	id := e.approved(t, "Integrales dobles", "Análisis II")
	keep := e.approved(t, "Leyes de Newton", "Física I")
	ctx := context.Background()

	// Se llenan la caché del detalle y de las páginas.
	for range 2 {
		if _, err := e.svc.Get(ctx, anyone, id); err != nil {
			t.Fatal(err)
		}
		e.list(t, "")
		e.list(t, "análisis ii")
	}

	if err := e.svc.Delete(ctx, admin, id); err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.Get(ctx, anyone, id); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("detalle de un eliminado = %v, se esperaba ErrNotFound", err)
	}
	if got := ids(e.list(t, "")); contains(got, id) || !contains(got, keep) {
		t.Errorf("listado después de eliminar = %v", got)
	}
	if got := ids(e.list(t, "análisis ii")); contains(got, id) {
		t.Errorf("listado de la materia después de eliminar = %v", got)
	}
	if n := e.metrics.Invalidations(cached.WriteDelete, cached.ResultOK); n != 2 {
		t.Errorf("invalidaciones al eliminar = %d, se esperaban 2 (detalle y listado)", n)
	}
}

// Aprobar cambia la generación: el nuevo aparece aunque la página estuviera en la caché.
func TestAprobadoApareceEnElListado(t *testing.T) {
	e := newEnv(t, cache.NewLocal())
	first := e.approved(t, "Integrales dobles", "Análisis II")
	if got := ids(e.list(t, "")); len(got) != 1 || got[0] != first {
		t.Fatalf("listado = %v", got)
	}
	e.list(t, "") // ahora sí, de la caché

	second := e.approved(t, "Leyes de Newton", "Física I")
	if got := ids(e.list(t, "")); !contains(got, second) {
		t.Errorf("listado después de aprobar = %v, falta %s", got, second)
	}
}

// Rechazar no toca la caché: un rechazado nunca estuvo en el listado.
func TestRechazarNoInvalida(t *testing.T) {
	e := newEnv(t, cache.NewLocal())
	doc, err := e.svc.Publish(context.Background(), service.NewDocument{
		Title: "Rechazable", Category: "Física I", Author: model.Author{UserID: student.UserID, Name: "Ana"},
		Source: model.SourceEstudiario,
	}, service.File{ContentType: "application/pdf", Data: []byte("%PDF-1.4")})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Reject(context.Background(), admin, doc.ID, "está incompleto"); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{cached.WriteApprove, cached.WriteDelete, cached.WriteRate} {
		if n := e.metrics.Invalidations(w, cached.ResultOK); n != 0 {
			t.Errorf("invalidaciones de %s = %d", w, n)
		}
	}
}

// Calificar borra el detalle: el promedio nuevo se ve enseguida. El listado
// no se invalida (vence solo a los 60 s).
func TestCalificarActualizaElDetalle(t *testing.T) {
	e := newEnv(t, cache.NewLocal())
	id := e.approved(t, "Integrales dobles", "Análisis II")
	ctx := context.Background()
	if _, err := e.svc.Get(ctx, anyone, id); err != nil {
		t.Fatal(err)
	}
	e.list(t, "")
	_, searchs0 := e.db.reads()

	if _, err := e.svc.Rate(ctx, rater, id, 4); err != nil {
		t.Fatal(err)
	}

	doc, err := e.svc.Get(ctx, anyone, id)
	if err != nil || doc.Rating.Count != 1 || doc.Rating.Average != 4 {
		t.Errorf("detalle después de calificar = %+v, %v", doc.Rating, err)
	}
	e.list(t, "")
	if _, searchs := e.db.reads(); searchs != searchs0 {
		t.Errorf("calificar no invalida el listado: se esperaba leerlo de la caché")
	}
	if n := e.metrics.Invalidations(cached.WriteRate, cached.ResultOK); n != 1 {
		t.Errorf("invalidaciones al calificar = %d, se esperaba 1 (el detalle)", n)
	}
}

// broken es una caché que nunca responde, como Memcached caído.
type broken struct{ calls atomic.Int64 }

var errDown = errors.New("memcache: connect timeout")

func (b *broken) Name() string { return "broken" }
func (b *broken) Get(string) ([]byte, error) {
	b.calls.Add(1)
	return nil, errDown
}
func (b *broken) Set(string, []byte, time.Duration) error {
	b.calls.Add(1)
	return errDown
}
func (b *broken) Delete(string) error {
	b.calls.Add(1)
	return errDown
}

// ADR-010: con la caché caída, el foro funciona igual, sin errores, leyendo de la base.
func TestConCacheCaidaElForoFuncionaIgual(t *testing.T) {
	c := &broken{}
	e := newEnv(t, c)
	id := e.approved(t, "Integrales dobles", "Análisis II")
	ctx := context.Background()

	for range 2 {
		if doc, err := e.svc.Get(ctx, anyone, id); err != nil || doc.ID != id {
			t.Fatalf("detalle = %v", err)
		}
		if p := e.list(t, ""); len(p.Items) != 1 {
			t.Fatalf("listado = %v", ids(p))
		}
	}
	if _, err := e.svc.Rate(ctx, rater, id, 5); err != nil {
		t.Fatalf("calificar = %v", err)
	}
	if err := e.svc.Delete(ctx, admin, id); err != nil {
		t.Fatalf("eliminar = %v", err)
	}
	if _, err := e.svc.Get(ctx, anyone, id); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("detalle de un eliminado = %v", err)
	}
	if c.calls.Load() == 0 {
		t.Error("se esperaba que intentara usar la caché")
	}
	if n := e.metrics.Count(cached.OpDetail, cached.ResultError); n == 0 {
		t.Error("los errores de la caché tienen que contarse")
	}
}

// Con el Memcached real apuntado a un puerto que no responde, cada operación
// espera como mucho el timeout de 100 ms y el foro responde igual.
func TestMemcachedQueNoRespondeNoFrenaMasDelTimeout(t *testing.T) {
	// Un puerto que acepta conexiones pero nunca contesta: el peor caso, un
	// Memcached colgado (si rechazara la conexión, fallaría enseguida).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	e := newEnv(t, cache.NewMemcached(ln.Addr().String()))
	id := e.approved(t, "Integrales dobles", "Análisis II")

	started := time.Now()
	doc, err := e.svc.Get(context.Background(), anyone, id)
	elapsed := time.Since(started)
	if err != nil || doc.ID != id {
		t.Fatalf("detalle = %v", err)
	}
	// Un solo timeout: si la lectura no respondió, no se intenta guardar.
	// Con margen para una máquina lenta.
	if elapsed > cache.Timeout+150*time.Millisecond {
		t.Errorf("el detalle tardó %v con Memcached colgado", elapsed)
	}
}

// Si borrar el detalle de un eliminado falla, se reintenta en segundo plano.
func TestEliminarReintentaLaInvalidacion(t *testing.T) {
	c := &flaky{Cache: cache.NewLocal()}
	e := newEnv(t, c)
	id := e.approved(t, "Integrales dobles", "Análisis II")
	ctx := context.Background()
	if _, err := e.svc.Get(ctx, anyone, id); err != nil { // queda en la caché
		t.Fatal(err)
	}

	c.failDeletes.Store(1) // falla el primer borrado, anda el reintento
	if err := e.svc.Delete(ctx, admin, id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for e.metrics.Invalidations(cached.WriteDelete, cached.ResultOK) < 2 { // listado + reintento del detalle
		if time.Now().After(deadline) {
			t.Fatal("no se reintentó la invalidación del detalle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := c.Get("forum:v1:summary:" + id); !errors.Is(err, cache.ErrCacheMiss) {
		t.Errorf("el detalle sigue en la caché: %v", err)
	}
}

type flaky struct {
	cache.Cache
	failDeletes atomic.Int64
}

func (f *flaky) Delete(key string) error {
	if f.failDeletes.Add(-1) >= 0 {
		return errDown
	}
	return f.Cache.Delete(key)
}
