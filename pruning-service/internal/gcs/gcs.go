package gcs

import (
	"cloud.google.com/go/storage"
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/ruslanbaba/distributed-build-cache/pruning-service/internal/metrics"
)

type Config struct {
	ProjectID       string
	Bucket          string
	MaxTotalBytes   int64
	MinAgeToDelete  time.Duration
	DeleteBatchSize int
}

type Client struct {
	cfg    Config
	client *storage.Client
}

type Stats struct {
	Scanned    int64
	Deleted    int64
	BytesFreed int64
	Total      int64
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}
	return &Client{cfg: cfg, client: client}, nil
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) Prune(ctx context.Context) (Stats, error) {
	startTime := time.Now()
	bucket := c.client.Bucket(c.cfg.Bucket)
	
	// List all objects
	query := &storage.Query{Prefix: ""}
	it := bucket.Objects(ctx, query)
	
	var objects []storage.ObjectAttrs
	var totalBytes int64
	
	log.Println("Scanning bucket objects...")
	for {
		attrs, err := it.Next()
		if err == storage.IteratorDone {
			break
		}
		if err != nil {
			return Stats{}, fmt.Errorf("failed to list objects: %w", err)
		}
		objects = append(objects, *attrs)
		totalBytes += attrs.Size
	}
	
	log.Printf("Found %d objects, total size: %d bytes (%.2f GB)", 
		len(objects), totalBytes, float64(totalBytes)/1024/1024/1024)
	
	// Update metrics
	metrics.TotalBytes.Set(float64(totalBytes))
	metrics.ObjectsScanned.Set(float64(len(objects)))
	
	// Check if pruning is needed
	if totalBytes <= c.cfg.MaxTotalBytes {
		log.Printf("Total size (%d) is under limit (%d), no pruning needed", totalBytes, c.cfg.MaxTotalBytes)
		return Stats{
			Scanned: int64(len(objects)),
			Total:   totalBytes,
		}, nil
	}
	
	log.Printf("Pruning needed: current=%d target=%d excess=%d", 
		totalBytes, c.cfg.MaxTotalBytes, totalBytes-c.cfg.MaxTotalBytes)
	
	// Sort by updated time (oldest first) for LRU approximation
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].Updated.Before(objects[j].Updated)
	})
	
	// Delete objects respecting MinAgeToDelete
	threshold := time.Now().Add(-c.cfg.MinAgeToDelete)
	var deleted, bytesFreed int64
	
	log.Printf("Deleting objects older than %v", threshold)
	
	for _, obj := range objects {
		if totalBytes <= c.cfg.MaxTotalBytes {
			break
		}
		
		if obj.Updated.After(threshold) {
			log.Printf("Skipping recent object: %s (updated: %v)", obj.Name, obj.Updated)
			continue
		}
		
		log.Printf("Deleting object: %s (size: %d, updated: %v)", obj.Name, obj.Size, obj.Updated)
		
		if err := bucket.Object(obj.Name).Delete(ctx); err != nil {
			log.Printf("Failed to delete %s: %v", obj.Name, err)
			metrics.DeletionErrors.Inc()
			continue
		}
		
		deleted++
		bytesFreed += obj.Size
		totalBytes -= obj.Size
		
		// Update metrics
		metrics.ObjectsDeleted.Inc()
		metrics.BytesFreed.Add(float64(obj.Size))
		
		// Batch size check
		if int(deleted)%c.cfg.DeleteBatchSize == 0 {
			log.Printf("Deleted %d objects so far, freed %d bytes", deleted, bytesFreed)
		}
	}
	
	duration := time.Since(startTime)
	metrics.PruningDuration.Observe(duration.Seconds())
	
	log.Printf("Pruning completed in %v", duration)
	
	return Stats{
		Scanned:    int64(len(objects)),
		Deleted:    deleted,
		BytesFreed: bytesFreed,
		Total:      totalBytes,
	}, nil
}
