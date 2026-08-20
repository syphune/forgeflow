package httpapi

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type Metrics struct {
	requests      atomic.Uint64
	errors        atomic.Uint64
	inFlight      atomic.Int64
	durationNanos atomic.Uint64
}

func NewMetrics() *Metrics { return &Metrics{} }

func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		m.inFlight.Add(1)
		response := &statusWriter{ResponseWriter: w}
		defer func() {
			m.inFlight.Add(-1)
			m.requests.Add(1)
			m.durationNanos.Add(uint64(time.Since(started)))
			if response.statusCode() >= http.StatusBadRequest {
				m.errors.Add(1)
			}
		}()
		next.ServeHTTP(response, r)
	})
}

func (m *Metrics) Endpoint(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(token)
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprintf(w, "# TYPE forgeflow_http_requests_total counter\nforgeflow_http_requests_total %d\n# TYPE forgeflow_http_errors_total counter\nforgeflow_http_errors_total %d\n# TYPE forgeflow_http_requests_in_flight gauge\nforgeflow_http_requests_in_flight %d\n# TYPE forgeflow_http_request_duration_seconds_sum counter\nforgeflow_http_request_duration_seconds_sum %f\n", m.requests.Load(), m.errors.Load(), m.inFlight.Load(), float64(m.durationNanos.Load())/float64(time.Second))
	})
}
