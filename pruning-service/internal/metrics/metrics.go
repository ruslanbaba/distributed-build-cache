package metrics

import (
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Total bytes in bucket
	TotalBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gcs_cache_total_bytes",
		Help: "Total bytes in the GCS cache bucket",
	})

	// Objects scanned during pruning
	ObjectsScanned = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gcs_cache_objects_scanned_total",
		Help: "Total number of objects scanned during pruning",
	})

	// Objects deleted counter
	ObjectsDeleted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gcs_cache_objects_deleted_total",
		Help: "Total number of objects deleted by pruner",
	})

	// Bytes freed counter
	BytesFreed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gcs_cache_bytes_freed_total",
		Help: "Total bytes freed by pruner",
	})

	// Deletion errors
	DeletionErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gcs_cache_deletion_errors_total",
		Help: "Total number of deletion errors",
	})

	// Pruning duration histogram
	PruningDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "gcs_cache_pruning_duration_seconds",
		Help:    "Time spent on pruning operations",
		Buckets: prometheus.DefBuckets,
	})

	// Pruning efficiency gauge (percentage of cache freed)
	PruningEfficiency = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gcs_cache_pruning_efficiency_percent",
		Help: "Percentage of cache freed during last pruning",
	})
)

func init() {
	prometheus.MustRegister(
		TotalBytes,
		ObjectsScanned,
		ObjectsDeleted,
		BytesFreed,
		DeletionErrors,
		PruningDuration,
		PruningEfficiency,
	)
}

func Serve(addr string) error {
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	log.Printf("Starting metrics server on %s", addr)
	return http.ListenAndServe(addr, nil)
}
