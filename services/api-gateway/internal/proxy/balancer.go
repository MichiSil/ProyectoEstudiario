package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/observability"
)

// Valores del chequeo de salud (ADR-012).
const (
	HealthInterval = 5 * time.Second
	HealthTimeout  = 2 * time.Second
	// FailsToRemove son los chequeos fallidos seguidos para sacar una
	// instancia de la rotación. Con un chequeo correcto vuelve.
	FailsToRemove = 2
)

// InstanceHeader indica en la respuesta qué instancia atendió el pedido
// (ADR-012). Se toma del X-Instance que manda la instancia (su nombre en los
// logs, por ejemplo forum-service-2) o, si no viene, de su dirección.
const (
	InstanceHeader         = "X-Served-By"
	UpstreamInstanceHeader = "X-Instance"
)

// Instance es una instancia de un servicio, por ejemplo forum-service-2:8083.
type Instance struct {
	Name string
	URL  *url.URL

	healthy atomic.Bool
	mu      sync.Mutex
	fails   int
}

// Healthy dice si la instancia está en la rotación.
func (i *Instance) Healthy() bool { return i.healthy.Load() }

// Balancer reparte los pedidos en round robin entre las instancias sanas y
// saca de la rotación a las que no responden (ADR-012):
//
//   - chequeo activo: GET /health cada HealthInterval; FailsToRemove fallas
//     seguidas la sacan y una respuesta correcta la vuelve a poner;
//   - chequeo pasivo: si un pedido real no puede conectarse, la instancia sale
//     en ese momento, sin esperar al próximo chequeo.
type Balancer struct {
	Service   string
	Instances []*Instance
	logger    *slog.Logger
	client    *http.Client
	next      atomic.Uint64
}

// NewBalancer arma el balanceador. Todas las instancias empiezan en la
// rotación: el primer chequeo saca a las que no respondan.
func NewBalancer(service string, targets []*url.URL, logger *slog.Logger) *Balancer {
	b := &Balancer{Service: service, logger: logger, client: &http.Client{Timeout: HealthTimeout}}
	for _, t := range targets {
		inst := &Instance{Name: t.Host, URL: t}
		inst.healthy.Store(true)
		b.Instances = append(b.Instances, inst)
	}
	return b
}

// Pick devuelve la próxima instancia sana en round robin, salteando skip (la
// que acaba de fallar), o nil si no hay ninguna.
func (b *Balancer) Pick(skip *Instance) *Instance {
	n := len(b.Instances)
	start := b.next.Add(1)
	for k := 0; k < n; k++ {
		inst := b.Instances[(int(start)+k)%n]
		if inst != skip && inst.Healthy() {
			return inst
		}
	}
	return nil
}

// MarkDown saca una instancia de la rotación por un pedido que falló (chequeo pasivo).
func (b *Balancer) MarkDown(inst *Instance, reason error) {
	inst.mu.Lock()
	inst.fails = FailsToRemove
	inst.mu.Unlock()
	if inst.healthy.Swap(false) {
		b.logger.Warn("instancia fuera de la rotación", "upstream", b.Service, "upstreamInstance", inst.Name,
			"reason", "falló un pedido", "error", reason.Error())
	}
}

// Run hace el chequeo activo hasta que se cancele ctx.
func (b *Balancer) Run(ctx context.Context) {
	ticker := time.NewTicker(HealthInterval)
	defer ticker.Stop()
	for {
		b.CheckAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// CheckAll consulta el GET /health de cada instancia una vez.
func (b *Balancer) CheckAll(ctx context.Context) {
	for _, inst := range b.Instances {
		err := b.check(ctx, inst)
		inst.mu.Lock()
		if err == nil {
			inst.fails = 0
		} else {
			inst.fails++
		}
		fails := inst.fails
		inst.mu.Unlock()

		switch {
		case err == nil && !inst.healthy.Swap(true):
			b.logger.Info("instancia de vuelta en la rotación", "upstream", b.Service, "upstreamInstance", inst.Name)
		case err != nil && fails >= FailsToRemove && inst.healthy.Swap(false):
			b.logger.Warn("instancia fuera de la rotación", "upstream", b.Service, "upstreamInstance", inst.Name,
				"reason", "no responde el chequeo de salud", "error", err.Error())
		}
	}
}

func (b *Balancer) check(ctx context.Context, inst *Instance) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.URL.JoinPath("/health").String(), nil)
	if err != nil {
		return err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}

type instanceKey struct{}
type retriedKey struct{}

// Balanced devuelve un reverse proxy que reparte los pedidos de prefix entre
// las instancias del balanceador, con los mismos timeouts, errores y
// propagación de la correlación y la traza que newProxy.
//
// Si una instancia no responde, una lectura (GET o HEAD) se reintenta una vez
// en otra instancia (ADR-005: sólo operaciones idempotentes). Una escritura no
// se reintenta, porque no se sabe si la instancia la llegó a aplicar: responde
// 503 UPSTREAM_UNAVAILABLE.
func Balanced(b *Balancer, prefix string, timeout func(*http.Request) time.Duration, logger *slog.Logger) http.Handler {
	var rp *httputil.ReverseProxy
	rp = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			inst := pr.In.Context().Value(instanceKey{}).(*Instance)
			pr.SetURL(inst.URL)
			pr.Out.URL.Path = trimPrefix(pr.In.URL.Path, prefix)
			pr.Out.URL.RawPath = ""
			pr.SetXForwarded()
		},
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del(observability.CorrelationHeader)
			name := resp.Header.Get(UpstreamInstanceHeader)
			if name == "" {
				name = resp.Request.Context().Value(instanceKey{}).(*Instance).Name
			}
			resp.Header.Del(UpstreamInstanceHeader)
			resp.Header.Set(InstanceHeader, name)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			inst := r.Context().Value(instanceKey{}).(*Instance)
			if errors.Is(err, context.DeadlineExceeded) {
				logger.ErrorContext(r.Context(), "falla al llamar a "+b.Service, "upstreamInstance", inst.Name, "error", err.Error())
				writeProblem(w, r, http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT", b.Service+" no respondió a tiempo")
				return
			}
			if errors.Is(err, context.Canceled) {
				return // el cliente cortó el pedido
			}
			b.MarkDown(inst, err)
			idempotent := r.Method == http.MethodGet || r.Method == http.MethodHead
			if idempotent && r.Context().Value(retriedKey{}) == nil {
				if other := b.Pick(inst); other != nil {
					logger.WarnContext(r.Context(), "se reintenta la lectura en otra instancia",
						"upstream", b.Service, "from", inst.Name, "to", other.Name)
					ctx := context.WithValue(context.WithValue(r.Context(), retriedKey{}, true), instanceKey{}, other)
					serve(rp, w, r.WithContext(ctx), other)
					return
				}
			}
			logger.ErrorContext(r.Context(), "falla al llamar a "+b.Service, "upstreamInstance", inst.Name, "error", err.Error())
			writeProblem(w, r, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", b.Service+" no está disponible")
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inst := b.Pick(nil)
		if inst == nil {
			logger.ErrorContext(r.Context(), "ninguna instancia disponible", "upstream", b.Service)
			writeProblem(w, r, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", b.Service+" no está disponible")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout(r))
		defer cancel()
		serve(rp, w, r.WithContext(context.WithValue(ctx, instanceKey{}, inst)), inst)
	})
}

// serve marca la traza con la instancia elegida y reenvía el pedido.
func serve(rp *httputil.ReverseProxy, w http.ResponseWriter, r *http.Request, inst *Instance) {
	trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("upstream.instance", inst.Name))
	rp.ServeHTTP(w, r)
}
