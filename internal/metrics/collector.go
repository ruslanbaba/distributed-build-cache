package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Collector holds all Prometheus metrics
type Collector struct {
	// Cache operation metrics
	CacheHits               *prometheus.CounterVec
	CacheWrites             prometheus.Counter
	CacheDeletions          prometheus.Counter
	CacheErrors             *prometheus.CounterVec
	CacheOperationDuration  *prometheus.HistogramVec
	CacheSize               prometheus.Gauge

	// Pruning metrics
	PrunedEntries    prometheus.Counter
	PrunedBytes      prometheus.Counter
	PruningDuration  prometheus.Histogram
	PruningErrors    prometheus.Counter

	// System metrics
	ActiveConnections   prometheus.Gauge
	RequestsTotal       *prometheus.CounterVec
	RequestDuration     *prometheus.HistogramVec
	GRPCRequestsTotal   *prometheus.CounterVec
	GRPCRequestDuration *prometheus.HistogramVec
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		// Cache metrics
		CacheHits: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_hits_total",
				Help: "Total number of cache hits and misses",
			},
			[]string{"result"}, // hit, miss
		),
		CacheWrites: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "cache_writes_total",
				Help: "Total number of cache writes",
			},
		),
		CacheDeletions: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "cache_deletions_total",
				Help: "Total number of cache deletions",
			},
		),
		CacheErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_errors_total",
				Help: "Total number of cache operation errors",
			},
			[]string{"operation"}, // get, put, delete, list
		),
		CacheOperationDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "cache_operation_duration_seconds",
				Help:    "Duration of cache operations",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~32s
			},
			[]string{"operation"},
		),
		CacheSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "cache_size_bytes",
				Help: "Current size of the cache in bytes",
			},
		),

		// Pruning metrics
		PrunedEntries: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "pruned_entries_total",
				Help: "Total number of cache entries pruned",
			},
		),
		PrunedBytes: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "pruned_bytes_total",
				Help: "Total bytes pruned from cache",
			},
		),
		PruningDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "pruning_duration_seconds",
				Help:    "Duration of pruning operations",
				Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17m
			},
		),
		PruningErrors: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "pruning_errors_total",
				Help: "Total number of pruning errors",
			},
		),

		// System metrics
		ActiveConnections: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "active_connections",
				Help: "Number of active gRPC connections",
			},
		),
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "status"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "request_duration_seconds",
				Help:    "Duration of HTTP requests",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method"},
		),
		GRPCRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "grpc_requests_total",
				Help: "Total number of gRPC requests",
			},
			[]string{"method", "status"},
		),
		GRPCRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "grpc_request_duration_seconds",
				Help:    "Duration of gRPC requests",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
			},
			[]string{"method"},
		),
	}
}

// Describe implements prometheus.Collector
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	c.CacheHits.Describe(ch)
	c.CacheWrites.Describe(ch)
	c.CacheDeletions.Describe(ch)
	c.CacheErrors.Describe(ch)
	c.CacheOperationDuration.Describe(ch)
	c.CacheSize.Describe(ch)
	c.PrunedEntries.Describe(ch)
	c.PrunedBytes.Describe(ch)
	c.PruningDuration.Describe(ch)
	c.PruningErrors.Describe(ch)
	c.ActiveConnections.Describe(ch)
	c.RequestsTotal.Describe(ch)
	c.RequestDuration.Describe(ch)
	c.GRPCRequestsTotal.Describe(ch)
	c.GRPCRequestDuration.Describe(ch)
}

// Collect implements prometheus.Collector
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.CacheHits.Collect(ch)
	c.CacheWrites.Collect(ch)
	c.CacheDeletions.Collect(ch)
	c.CacheErrors.Collect(ch)
	c.CacheOperationDuration.Collect(ch)
	c.CacheSize.Collect(ch)
	c.PrunedEntries.Collect(ch)
	c.PrunedBytes.Collect(ch)
	c.PruningDuration.Collect(ch)
	c.PruningErrors.Collect(ch)
	c.ActiveConnections.Collect(ch)
	c.RequestsTotal.Collect(ch)
	c.RequestDuration.Collect(ch)
	c.GRPCRequestsTotal.Collect(ch)
	c.GRPCRequestDuration.Collect(ch)
}
