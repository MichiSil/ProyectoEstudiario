package cached

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Resultados de una lectura o una invalidación.
const (
	ResultHit   = "hit"
	ResultMiss  = "miss"
	ResultError = "error" // Memcached no respondió (error o timeout): se leyó de MongoDB
	ResultOK    = "ok"
)

// Metrics cuenta los aciertos y fallos por operación y las invalidaciones por
// escritura (ADR-007, 6). Es un prometheus.Collector: registrado en el
// registro de Prometheus, sale en /metrics como
//
//	forum_cache_requests_total{operation="detalle|listado", result="hit|miss|error"}
//	forum_cache_invalidations_total{write="aprobar|eliminar|calificar|indexar", result="ok|error"}
//
// y el porcentaje de aciertos es hit / (hit + miss + error). Un *Metrics nil
// no cuenta nada.
type Metrics struct {
	requests      [2][3]atomic.Int64 // [operación][hit, miss, error]
	invalidations [4][2]atomic.Int64 // [escritura][ok, error]
}

var (
	ops     = []string{OpDetail, OpList}
	reads   = []string{ResultHit, ResultMiss, ResultError}
	writes  = []string{WriteApprove, WriteDelete, WriteRate, WriteIndex}
	results = []string{ResultOK, ResultError}

	requestsDesc = prometheus.NewDesc("forum_cache_requests_total",
		"Lecturas de la caché del foro por operación y resultado (ADR-007).", []string{"operation", "result"}, nil)
	invalidationsDesc = prometheus.NewDesc("forum_cache_invalidations_total",
		"Invalidaciones de la caché del foro por escritura y resultado (ADR-007).", []string{"write", "result"}, nil)
)

func index(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return -1
}

func (m *Metrics) add(op, result string) {
	if m == nil {
		return
	}
	if i, j := index(ops, op), index(reads, result); i >= 0 && j >= 0 {
		m.requests[i][j].Add(1)
	}
}

func (m *Metrics) invalidation(write, result string) {
	if m == nil {
		return
	}
	if i, j := index(writes, write), index(results, result); i >= 0 && j >= 0 {
		m.invalidations[i][j].Add(1)
	}
}

// Count devuelve cuántas lecturas de op terminaron en result.
func (m *Metrics) Count(op, result string) int64 {
	i, j := index(ops, op), index(reads, result)
	if m == nil || i < 0 || j < 0 {
		return 0
	}
	return m.requests[i][j].Load()
}

// Invalidations devuelve cuántas invalidaciones de write terminaron en result.
func (m *Metrics) Invalidations(write, result string) int64 {
	i, j := index(writes, write), index(results, result)
	if m == nil || i < 0 || j < 0 {
		return 0
	}
	return m.invalidations[i][j].Load()
}

// Describe y Collect implementan prometheus.Collector.
func (m *Metrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- requestsDesc
	ch <- invalidationsDesc
}

func (m *Metrics) Collect(ch chan<- prometheus.Metric) {
	for i, op := range ops {
		for j, res := range reads {
			ch <- prometheus.MustNewConstMetric(requestsDesc, prometheus.CounterValue, float64(m.requests[i][j].Load()), op, res)
		}
	}
	for i, w := range writes {
		for j, res := range results {
			ch <- prometheus.MustNewConstMetric(invalidationsDesc, prometheus.CounterValue, float64(m.invalidations[i][j].Load()), w, res)
		}
	}
}

// Log escribe cada every un resumen de la caché (aciertos, fallos y
// porcentaje de aciertos por operación desde el resumen anterior), si hubo
// lecturas. Así el impacto se ve también en los logs (ADR-011).
func (m *Metrics) Log(ctx context.Context, logger *slog.Logger, provider string, every time.Duration) {
	var last [2][3]int64
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for i, op := range ops {
			var now [3]int64
			var total int64
			for j := range reads {
				now[j] = m.requests[i][j].Load()
				total += now[j] - last[i][j]
			}
			if total == 0 {
				continue
			}
			hits := now[0] - last[i][0]
			logger.InfoContext(ctx, "caché del foro", "provider", provider, "operation", op,
				"hits", hits, "misses", now[1]-last[i][1], "errors", now[2]-last[i][2],
				"hitRatio", float64(hits)/float64(total))
			last[i] = now
		}
	}
}
