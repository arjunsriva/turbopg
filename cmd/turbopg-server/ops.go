package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/arjunsriva/turbopg"
)

type metrics struct {
	requests       atomic.Int64
	inflight       atomic.Int64
	status4        atomic.Int64
	status5        atomic.Int64
	latencyN       atomic.Int64
	latencySum     atomic.Int64
	latencyBuckets [len(latencyBucketBounds) + 1]atomic.Int64
}

// latencyBucketBounds are Prometheus histogram `le` values in milliseconds.
var latencyBucketBounds = [...]int64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

func (m *metrics) observe(ms int64, code int) {
	if m == nil {
		return
	}
	m.requests.Add(1)
	m.latencyN.Add(1)
	m.latencySum.Add(ms)
	for i, le := range latencyBucketBounds {
		if ms <= le {
			m.latencyBuckets[i].Add(1)
		}
	}
	m.latencyBuckets[len(latencyBucketBounds)].Add(1)
	if code >= 500 {
		m.status5.Add(1)
	} else if code >= 400 {
		m.status4.Add(1)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *Server) wrap(next http.Handler) http.Handler {
	maxBody := s.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBody
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		if s.metrics != nil {
			s.metrics.inflight.Add(1)
			defer s.metrics.inflight.Add(-1)
		}
		started := time.Now()
		next.ServeHTTP(rec, r)
		dur := time.Since(started)
		if s.metrics != nil {
			s.metrics.observe(dur.Milliseconds(), rec.code)
		}
		if s.Logger != nil {
			s.Logger.Info("http",
				turbopg.Field{Key: "request_id", Value: id},
				turbopg.Field{Key: "method", Value: r.Method},
				turbopg.Field{Key: "path", Value: sanitizePath(r.URL.Path)},
				turbopg.Field{Key: "status", Value: rec.code},
				turbopg.Field{Key: "duration_ms", Value: dur.Milliseconds()},
			)
		}
	})
}

func sanitizePath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 4 && (parts[1] == "v1" || parts[1] == "v2") && parts[2] == "namespaces" {
		parts[3] = "{ns}"
		return strings.Join(parts, "/")
	}
	return path
}

func newRequestID() string {
	var b [8]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil {
		http.Error(w, "store not initialized", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]string{
		"version": Version,
		"commit":  Commit,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	m := s.metrics
	if m == nil {
		m = &metrics{}
	}
	var poolOpen, poolIdle, poolWait int64
	if s.Store != nil {
		st := s.Store.DBStats()
		poolOpen = int64(st.OpenConnections)
		poolIdle = int64(st.Idle)
		poolWait = int64(st.WaitCount)
	}
	fmt.Fprintf(w, "# HELP turbopg_http_requests_total Total HTTP requests.\n")
	fmt.Fprintf(w, "# TYPE turbopg_http_requests_total counter\n")
	fmt.Fprintf(w, "turbopg_http_requests_total %d\n", m.requests.Load())
	fmt.Fprintf(w, "# HELP turbopg_http_in_flight Current in-flight requests.\n")
	fmt.Fprintf(w, "# TYPE turbopg_http_in_flight gauge\n")
	fmt.Fprintf(w, "turbopg_http_in_flight %d\n", m.inflight.Load())
	fmt.Fprintf(w, "# HELP turbopg_http_requests_4xx_total 4xx responses.\n")
	fmt.Fprintf(w, "# TYPE turbopg_http_requests_4xx_total counter\n")
	fmt.Fprintf(w, "turbopg_http_requests_4xx_total %d\n", m.status4.Load())
	fmt.Fprintf(w, "# HELP turbopg_http_requests_5xx_total 5xx responses.\n")
	fmt.Fprintf(w, "# TYPE turbopg_http_requests_5xx_total counter\n")
	fmt.Fprintf(w, "turbopg_http_requests_5xx_total %d\n", m.status5.Load())
	fmt.Fprintf(w, "# HELP turbopg_http_request_duration_milliseconds Request duration.\n")
	fmt.Fprintf(w, "# TYPE turbopg_http_request_duration_milliseconds histogram\n")
	for i, le := range latencyBucketBounds {
		fmt.Fprintf(w, "turbopg_http_request_duration_milliseconds_bucket{le=\"%d\"} %d\n", le, m.latencyBuckets[i].Load())
	}
	fmt.Fprintf(w, "turbopg_http_request_duration_milliseconds_bucket{le=\"+Inf\"} %d\n", m.latencyBuckets[len(latencyBucketBounds)].Load())
	fmt.Fprintf(w, "turbopg_http_request_duration_milliseconds_sum %d\n", m.latencySum.Load())
	fmt.Fprintf(w, "turbopg_http_request_duration_milliseconds_count %d\n", m.latencyN.Load())
	fmt.Fprintf(w, "# HELP turbopg_db_pool_open Open database connections.\n")
	fmt.Fprintf(w, "# TYPE turbopg_db_pool_open gauge\n")
	fmt.Fprintf(w, "turbopg_db_pool_open %d\n", poolOpen)
	fmt.Fprintf(w, "# HELP turbopg_db_pool_idle Idle database connections.\n")
	fmt.Fprintf(w, "# TYPE turbopg_db_pool_idle gauge\n")
	fmt.Fprintf(w, "turbopg_db_pool_idle %d\n", poolIdle)
	fmt.Fprintf(w, "# HELP turbopg_db_pool_wait_total Connection wait events.\n")
	fmt.Fprintf(w, "# TYPE turbopg_db_pool_wait_total counter\n")
	fmt.Fprintf(w, "turbopg_db_pool_wait_total %d\n", poolWait)
}
