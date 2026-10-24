package gsmetrics

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Identity struct {
	Pod        string
	DC         string
	Deployment string
	Namespace  string
	Cluster    string
}

func IdentityFromEnv() Identity {
	env := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	return Identity{
		Pod:        env("POD_NAME", "pod-local"),
		DC:         env("DC", "dc-local"),
		Deployment: env("DEPLOYMENT", "crud-api"),
		Namespace:  env("NS", "hw1"),
		Cluster:    env("CLUSTER", "dev"),
	}
}

// Верхние бакеты около 1с+ нужны, чтобы p99 не упирался в +Inf
var LatencyBuckets = []float64{
	0.0005, 0.001, 0.0025, 0.005, 0.0075, 0.01, 0.025, 0.05, 0.075,
	0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 10,
}

type Metrics struct {
	id      Identity
	latency *prometheus.HistogramVec // *_request_latency_seconds
	reqs    *prometheus.CounterVec   // *_requests_total
	errs    *prometheus.CounterVec   // *_errors_total
	colls   prometheus.Counter       // *_scrape_total
	dur     prometheus.Summary       // *_scrape_latency_seconds
}

func New(reg prometheus.Registerer) *Metrics {
	constLabels := prometheus.Labels{
		"pod":        os.Getenv("POD_NAME"),
		"dc":         os.Getenv("DC"),
		"deployment": os.Getenv("DEPLOYMENT"),
	}
	labels := []string{"route"}

	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "http_request_latency_seconds",
		Help:        "golden signal: latency",
		Buckets:     LatencyBuckets,
		ConstLabels: constLabels,
	}, labels)

	reqs := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "http_requests_total",
		Help:        "golden signal: traffic",
		ConstLabels: constLabels,
	}, labels)

	errs := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "http_errors_total",
		Help:        "golden signal: errors",
		ConstLabels: constLabels,
	}, []string{"route", "status_code"})

	colls := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "metrics_scrape_total",
		Help:        "Number of /metrics scrapes",
		ConstLabels: constLabels,
	})

	dur := prometheus.NewSummary(prometheus.SummaryOpts{
		Name:        "metrics_scrape_latency_seconds",
		Help:        "/metrics generation duration",
		ConstLabels: constLabels,
	})

	reg.MustRegister(latency, reqs, errs, colls, dur)
	return &Metrics{id: IdentityFromEnv(), latency: latency, reqs: reqs, errs: errs, colls: colls, dur: dur}
}

// Точка входа библиотеки
func (m *Metrics) Middleware(route string) func(http.Handler) http.Handler {
	// начальная инициация. Без этого иногда писало "NO DATA" там, где не надо.
	m.reqs.WithLabelValues(route)
	m.latency.WithLabelValues(route)
	for _, class := range []string{"4xx", "5xx"} {
		m.errs.WithLabelValues(route, class)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			m.latency.WithLabelValues(route).Observe(time.Since(start).Seconds())
			m.reqs.WithLabelValues(route).Inc()
			if class := errorClass(rw.status); class != "" {
				m.errs.WithLabelValues(route, class).Inc()
			}
		})
	}
}

func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func errorClass(code int) string {
	if code < 400 {
		return ""
	}
	if code >= 500 {
		return "5xx"
	}
	return "4xx"
}

type responseWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *responseWriter) WriteHeader(code int) {
	w.status = code
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}
func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.(http.Hijacker).Hijack()
}

// HTTP-обёртка для /metrics с моими метриками.
func (m *Metrics) WrapMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		m.colls.Inc()
		next.ServeHTTP(w, r)
		m.dur.Observe(time.Since(start).Seconds())
	})
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (m *Metrics) String() string { return fmt.Sprintf("gsmetrics{pod=%s,dc=%s}", m.id.Pod, m.id.DC) }
