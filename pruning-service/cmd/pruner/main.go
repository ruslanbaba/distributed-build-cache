package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ruslanbaba/distributed-build-cache/pruning-service/internal/gcs"
	"github.com/ruslanbaba/distributed-build-cache/pruning-service/internal/metrics"
)

func main() {
	cfg := gcs.Config{
		ProjectID:       env("GCP_PROJECT_ID", ""),
		Bucket:          env("GCS_BUCKET", ""),
		MaxTotalBytes:   envInt64("MAX_TOTAL_BYTES", 5*1024*1024*1024*1024), // 5 TB
		MinAgeToDelete:  envDuration("MIN_AGE", 14*24*time.Hour),
		DeleteBatchSize: envInt("DELETE_BATCH_SIZE", 1000),
	}

	if cfg.Bucket == "" {
		log.Fatal("GCS_BUCKET is required")
	}

	log.Printf("Starting pruner with config: ProjectID=%s, Bucket=%s, MaxTotalBytes=%d, MinAge=%s", 
		cfg.ProjectID, cfg.Bucket, cfg.MaxTotalBytes, cfg.MinAgeToDelete)

	ctx := context.Background()
	cl, err := gcs.NewClient(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()

	// Start metrics server
	go func() {
		log.Println("Starting metrics server on :9090")
		if err := metrics.Serve(":9090"); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	// Run pruning
	log.Println("Starting cache pruning...")
	stats, err := cl.Prune(ctx)
	if err != nil {
		log.Fatal(err)
	}
	
	log.Printf("Pruning completed: scanned=%d deleted=%d bytes_freed=%d total=%d efficiency=%.2f%%", 
		stats.Scanned, stats.Deleted, stats.BytesFreed, stats.Total, 
		float64(stats.BytesFreed)/float64(stats.Total)*100)

	// Update final metrics
	metrics.TotalBytes.Set(float64(stats.Total))
	metrics.ObjectsDeleted.Add(float64(stats.Deleted))
	metrics.BytesFreed.Add(float64(stats.BytesFreed))
	metrics.PruningDuration.Observe(time.Since(time.Now()).Seconds())
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if x, err := strconv.Atoi(v); err == nil {
			return x
		}
	}
	return d
}

func envInt64(k string, d int64) int64 {
	if v := os.Getenv(k); v != "" {
		if x, err := strconv.ParseInt(v, 10, 64); err == nil {
			return x
		}
	}
	return d
}

func envDuration(k string, d time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if dur, err := time.ParseDuration(v); err == nil {
			return dur
		}
	}
	return d
}
