package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/ruslanbaba/distributed-build-cache/internal/cache"
	"github.com/ruslanbaba/distributed-build-cache/internal/config"
	"github.com/ruslanbaba/distributed-build-cache/internal/metrics"
	"github.com/ruslanbaba/distributed-build-cache/internal/pruning"
	"github.com/ruslanbaba/distributed-build-cache/pkg/grpc/server"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Initialize structured logging
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Initialize metrics
	metricsRegistry := prometheus.NewRegistry()
	metricsCollector := metrics.NewCollector()
	metricsRegistry.MustRegister(metricsCollector)

	// Initialize Cloud Storage client
	ctx := context.Background()
	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		logger.Fatal("Failed to create storage client", zap.Error(err))
	}
	defer storageClient.Close()

	// Initialize cache service
	cacheService := cache.NewService(
		storageClient,
		cfg.Storage.BucketName,
		logger.Named("cache"),
		metricsCollector,
	)

	// Initialize pruning service
	pruningService := pruning.NewService(
		cacheService,
		logger.Named("pruning"),
		metricsCollector,
		pruning.Config{
			MaxCacheSize:    cfg.Pruning.MaxCacheSizeGB * 1024 * 1024 * 1024, // Convert GB to bytes
			PruningInterval: cfg.Pruning.IntervalHours * time.Hour,
			RetentionDays:   cfg.Pruning.RetentionDays,
		},
	)

	// Start pruning service
	go pruningService.Start(ctx)

	// Initialize gRPC server
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(server.UnaryLoggingInterceptor(logger)),
		grpc.StreamInterceptor(server.StreamLoggingInterceptor(logger)),
	)

	// Register services
	cacheGRPCServer := server.NewCacheServer(cacheService, logger.Named("grpc"))
	server.RegisterBuildCacheServiceServer(grpcServer, cacheGRPCServer)

	// Register health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Enable reflection for development
	if cfg.Server.EnableReflection {
		reflection.Register(grpcServer)
	}

	// Start gRPC server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.Port))
	if err != nil {
		logger.Fatal("Failed to listen", zap.Error(err), zap.Int("port", cfg.Server.Port))
	}

	go func() {
		logger.Info("Starting gRPC server",
			zap.Int("port", cfg.Server.Port),
			zap.String("version", version),
			zap.String("commit", commit),
			zap.String("date", date),
		)
		if err := grpcServer.Serve(listener); err != nil {
			logger.Fatal("Failed to serve gRPC", zap.Error(err))
		}
	}()

	// Start metrics server
	go func() {
		http.Handle("/metrics", promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{}))
		logger.Info("Starting metrics server", zap.Int("port", cfg.Metrics.Port))
		if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.Metrics.Port), nil); err != nil {
			logger.Error("Metrics server failed", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down gracefully")

	// Graceful shutdown
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("Server stopped gracefully")
	case <-shutdownCtx.Done():
		logger.Warn("Forcing server shutdown")
		grpcServer.Stop()
	}
}
