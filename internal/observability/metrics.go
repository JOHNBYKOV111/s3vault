package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registry = prometheus.NewRegistry()
	factory  = promauto.With(registry)
)

func init() {
	// USE: явная регистрация в нашем изолированном реестре.
	// Никакого DefaultRegisterer — никаких дубликатов.
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

var (
	// RED: HTTP
	httpRequestsTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	// Бизнес: S3-домен
	s3DedupRatio = factory.NewGauge(
		prometheus.GaugeOpts{
			Name: "s3_dedup_ratio",
			Help: "Deduplication ratio (unique blobs / total objects).",
		},
	)

	s3JobsQueueDepth = factory.NewGauge(
		prometheus.GaugeOpts{
			Name: "s3_jobs_queue_depth",
			Help: "Number of pending jobs in the queue.",
		},
	)

	s3BlobsTotal = factory.NewCounter(
		prometheus.CounterOpts{
			Name: "s3_blobs_total",
			Help: "Total number of unique blobs stored.",
		},
	)
)

type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapper := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapper, r)

		duration := time.Since(start).Seconds()
		statusStr := strconv.Itoa(wrapper.statusCode)

		httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, statusStr).Inc()
		httpRequestDuration.WithLabelValues(r.Method, r.URL.Path, statusStr).Observe(duration)
	})
}

// MetricsHandler возвращает HTTP-хендлер для Prometheus, привязанный к нашему реестру.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}
