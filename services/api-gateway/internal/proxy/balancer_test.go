package proxy

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// instance es una instancia de prueba que responde su nombre y cuenta los pedidos.
type instance struct {
	srv  *httptest.Server
	hits atomic.Int32
	sick atomic.Bool // /health responde 503
}

func newInstance(t *testing.T, name string) *instance {
	t.Helper()
	in := &instance{}
	in.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if in.sick.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}
		in.hits.Add(1)
		_, _ = w.Write([]byte(name))
	}))
	t.Cleanup(in.srv.Close)
	return in
}

func balancerFor(t *testing.T, ins ...*instance) *Balancer {
	var urls []*url.URL
	for _, in := range ins {
		urls = append(urls, mustURL(t, in.srv.URL))
	}
	return NewBalancer("forum-service", urls, testLogger)
}

func do(h http.Handler, method string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "/api/forum/summaries", nil))
	return rec
}

// ADR-012: los pedidos se reparten en round robin y la respuesta dice qué instancia atendió.
func TestBalanceoRoundRobin(t *testing.T) {
	a, b := newInstance(t, "a"), newInstance(t, "b")
	bal := balancerFor(t, a, b)
	h := Forum(bal, testLogger)
	served := map[string]bool{}
	for range 10 {
		rec := do(h, http.MethodGet)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		served[rec.Header().Get(InstanceHeader)] = true
	}
	if a.hits.Load() != 5 || b.hits.Load() != 5 {
		t.Errorf("reparto %d / %d, se esperaba 5 / 5", a.hits.Load(), b.hits.Load())
	}
	if len(served) != 2 {
		t.Errorf("%s tendría que nombrar las dos instancias: %v", InstanceHeader, served)
	}
}

// Una lectura a una instancia caída se reintenta en la otra, y la caída sale de la rotación.
func TestLecturaAUnaInstanciaCaidaSeReintenta(t *testing.T) {
	down, up := newInstance(t, "caida"), newInstance(t, "sana")
	bal := balancerFor(t, down, up)
	down.srv.Close()
	h := Forum(bal, testLogger)

	for range 4 {
		if rec := do(h, http.MethodGet); rec.Code != http.StatusOK || rec.Body.String() != "sana" {
			t.Fatalf("lectura: status %d, cuerpo %q", rec.Code, rec.Body.String())
		}
	}
	if bal.Instances[0].Healthy() {
		t.Error("la instancia caída tendría que haber salido de la rotación")
	}
	if up.hits.Load() != 4 {
		t.Errorf("la instancia sana recibió %d pedidos, se esperaban 4", up.hits.Load())
	}
}

// Una escritura no se reintenta: no se sabe si la instancia la llegó a aplicar.
func TestEscrituraAUnaInstanciaCaidaNoSeReintenta(t *testing.T) {
	down, up := newInstance(t, "caida"), newInstance(t, "sana")
	bal := balancerFor(t, down, up)
	down.srv.Close()
	bal.next.Store(1) // el próximo Pick elige la instancia 0, la caída
	rec := do(Forum(bal, testLogger), http.MethodPost)
	if rec.Code != http.StatusServiceUnavailable || up.hits.Load() != 0 {
		t.Errorf("escritura: status %d y %d pedidos a la sana; se esperaba 503 sin reintento", rec.Code, up.hits.Load())
	}
}

// El chequeo activo saca a una instancia después de dos fallas seguidas y la
// vuelve a poner con una respuesta correcta.
func TestChequeoDeSalud(t *testing.T) {
	a, b := newInstance(t, "a"), newInstance(t, "b")
	bal := balancerFor(t, a, b)
	ctx := context.Background()

	a.sick.Store(true)
	bal.CheckAll(ctx)
	if !bal.Instances[0].Healthy() {
		t.Fatal("con una sola falla todavía no tiene que salir")
	}
	bal.CheckAll(ctx)
	if bal.Instances[0].Healthy() {
		t.Fatal("con dos fallas seguidas tiene que salir de la rotación")
	}
	for range 4 {
		do(Forum(bal, testLogger), http.MethodGet)
	}
	if a.hits.Load() != 0 {
		t.Errorf("la instancia fuera de la rotación recibió %d pedidos", a.hits.Load())
	}

	a.sick.Store(false)
	bal.CheckAll(ctx)
	if !bal.Instances[0].Healthy() {
		t.Fatal("con una respuesta correcta tiene que volver a la rotación")
	}
}

// Sin ninguna instancia sana, el foro responde 503 enseguida.
func TestSinInstanciasSanas(t *testing.T) {
	a, b := newInstance(t, "a"), newInstance(t, "b")
	bal := balancerFor(t, a, b)
	a.srv.Close()
	b.srv.Close()
	if rec := do(Forum(bal, testLogger), http.MethodGet); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, se esperaba 503", rec.Code)
	}
	if rec := do(Forum(bal, testLogger), http.MethodGet); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("con las dos fuera de la rotación: status %d, se esperaba 503", rec.Code)
	}
}

// upInstances devuelve el valor de gateway_upstream_instance_up de cada
// instancia de upstream, como sale en /metrics.
func upInstances(t *testing.T, upstream string) map[string]float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, f := range families {
		if f.GetName() != "gateway_upstream_instance_up" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["upstream"] == upstream {
				out[labels["upstream_instance"]] = m.GetGauge().GetValue()
			}
		}
	}
	return out
}

func TestMetricaDeInstanciasEnLaRotacion(t *testing.T) {
	sana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(UpstreamInstanceHeader, "forum-metricas-1")
	}))
	defer sana.Close()
	caida := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer caida.Close()

	b := NewBalancer("forum-metricas", []*url.URL{mustURL(t, sana.URL), mustURL(t, caida.URL)}, testLogger)
	for range FailsToRemove {
		b.CheckAll(context.Background())
	}
	// La sana sale con el nombre de su X-Instance (y sin la serie con su host);
	// la caída, con su host y en 0.
	want := map[string]float64{"forum-metricas-1": 1, b.Instances[1].Name: 0}
	if got := upInstances(t, "forum-metricas"); !maps.Equal(got, want) {
		t.Errorf("gateway_upstream_instance_up = %v, se esperaba %v", got, want)
	}
}
