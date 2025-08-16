package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ObservabilityStack provides comprehensive monitoring and tracing
type ObservabilityStack struct {
	tracer           trace.Tracer
	meter            metric.Meter
	logger           *zap.Logger
	
	// Custom metrics
	cacheLatency     metric.Float64Histogram
	cacheHitRate     metric.Float64Gauge
	storageUsage     metric.Int64Gauge
	costMetrics      metric.Float64Gauge
	slaMetrics       metric.Float64Gauge
	
	// Business metrics
	buildAcceleration metric.Float64Gauge
	developerSatisfaction metric.Float64Gauge
	costSavings      metric.Float64Gauge
}

// NewObservabilityStack initializes comprehensive monitoring
func NewObservabilityStack(serviceName string, version string) (*ObservabilityStack, error) {
	// Initialize Jaeger tracer
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint("http://jaeger:14268/api/traces")))
	if err != nil {
		return nil, err
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(
			resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String(serviceName),
				semconv.ServiceVersionKey.String(version),
			),
		),
	)
	otel.SetTracerProvider(tp)

	// Initialize Prometheus metrics
	promExporter, err := prometheus.New()
	if err != nil {
		return nil, err
	}

	mp := metric.NewMeterProvider(metric.WithReader(promExporter))
	otel.SetMeterProvider(mp)

	tracer := otel.Tracer(serviceName)
	meter := otel.Meter(serviceName)

	// Initialize custom metrics
	cacheLatency, _ := meter.Float64Histogram(
		"cache_operation_latency_seconds",
		metric.WithDescription("Cache operation latency in seconds"),
		metric.WithUnit("s"),
	)

	cacheHitRate, _ := meter.Float64Gauge(
		"cache_hit_rate_percentage",
		metric.WithDescription("Cache hit rate percentage"),
	)

	storageUsage, _ := meter.Int64Gauge(
		"storage_usage_bytes",
		metric.WithDescription("Current storage usage in bytes"),
	)

	costMetrics, _ := meter.Float64Gauge(
		"monthly_cost_usd",
		metric.WithDescription("Monthly cost in USD"),
	)

	slaMetrics, _ := meter.Float64Gauge(
		"sla_compliance_percentage",
		metric.WithDescription("SLA compliance percentage"),
	)

	buildAcceleration, _ := meter.Float64Gauge(
		"build_acceleration_percentage",
		metric.WithDescription("Build time reduction percentage"),
	)

	developerSatisfaction, _ := meter.Float64Gauge(
		"developer_satisfaction_score",
		metric.WithDescription("Developer satisfaction score (1-10)"),
	)

	costSavings, _ := meter.Float64Gauge(
		"monthly_cost_savings_usd",
		metric.WithDescription("Monthly cost savings in USD"),
	)

	return &ObservabilityStack{
		tracer:               tracer,
		meter:                meter,
		cacheLatency:         cacheLatency,
		cacheHitRate:         cacheHitRate,
		storageUsage:         storageUsage,
		costMetrics:          costMetrics,
		slaMetrics:           slaMetrics,
		buildAcceleration:    buildAcceleration,
		developerSatisfaction: developerSatisfaction,
		costSavings:          costSavings,
	}, nil
}

// TraceOperation wraps operations with distributed tracing
func (o *ObservabilityStack) TraceOperation(ctx context.Context, operationName string, fn func(context.Context) error) error {
	ctx, span := o.tracer.Start(ctx, operationName)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	// Record metrics
	o.cacheLatency.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			attribute.String("operation", operationName),
			attribute.Bool("success", err == nil),
		),
	)

	// Add span attributes
	span.SetAttributes(
		attribute.String("operation.name", operationName),
		attribute.Float64("operation.duration_ms", float64(duration.Nanoseconds())/1e6),
		attribute.Bool("operation.success", err == nil),
	)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	return err
}

// RecordBusinessMetrics records high-level business metrics
func (o *ObservabilityStack) RecordBusinessMetrics(ctx context.Context, metrics BusinessMetrics) {
	o.buildAcceleration.Record(ctx, metrics.BuildAccelerationPercent)
	o.developerSatisfaction.Record(ctx, metrics.DeveloperSatisfactionScore)
	o.costSavings.Record(ctx, metrics.MonthlyCostSavings)
	o.cacheHitRate.Record(ctx, metrics.CacheHitRatePercent)
	o.storageUsage.Record(ctx, metrics.StorageUsageBytes)
	o.slaMetrics.Record(ctx, metrics.SLACompliancePercent)
}

// BusinessMetrics represents high-level business metrics
type BusinessMetrics struct {
	BuildAccelerationPercent    float64
	DeveloperSatisfactionScore  float64
	MonthlyCostSavings         float64
	CacheHitRatePercent        float64
	StorageUsageBytes          int64
	SLACompliancePercent       float64
}
