package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"go.uber.org/zap"
	"google.golang.org/api/iterator"

	"github.com/ruslanbaba/distributed-build-cache/internal/metrics"
)

// Service provides cache operations with Cloud Storage backend
type Service struct {
	client     *storage.Client
	bucketName string
	logger     *zap.Logger
	metrics    *metrics.Collector
}

// CacheEntry represents a cached build artifact
type CacheEntry struct {
	Key          string
	Size         int64
	LastAccessed time.Time
	ContentType  string
	Hash         string
}

// NewService creates a new cache service
func NewService(client *storage.Client, bucketName string, logger *zap.Logger, metrics *metrics.Collector) *Service {
	return &Service{
		client:     client,
		bucketName: bucketName,
		logger:     logger,
		metrics:    metrics,
	}
}

// Get retrieves a cache entry from Cloud Storage
func (s *Service) Get(ctx context.Context, key string) (io.ReadCloser, *CacheEntry, error) {
	start := time.Now()
	defer func() {
		s.metrics.CacheOperationDuration.WithLabelValues("get").Observe(time.Since(start).Seconds())
	}()

	// Sanitize key
	objectName := s.sanitizeKey(key)
	
	bucket := s.client.Bucket(s.bucketName)
	obj := bucket.Object(objectName)

	// Get object attributes
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			s.metrics.CacheHits.WithLabelValues("miss").Inc()
			return nil, nil, fmt.Errorf("cache miss for key %s: %w", key, err)
		}
		s.metrics.CacheErrors.WithLabelValues("get_attrs").Inc()
		return nil, nil, fmt.Errorf("failed to get object attributes: %w", err)
	}

	// Update last accessed time
	if _, err := obj.Update(ctx, storage.ObjectAttrsToUpdate{
		Metadata: map[string]string{
			"last_accessed": time.Now().Format(time.RFC3339),
		},
	}); err != nil {
		s.logger.Warn("Failed to update last accessed time", zap.Error(err))
	}

	// Open reader
	reader, err := obj.NewReader(ctx)
	if err != nil {
		s.metrics.CacheErrors.WithLabelValues("read").Inc()
		return nil, nil, fmt.Errorf("failed to create reader: %w", err)
	}

	entry := &CacheEntry{
		Key:          key,
		Size:         attrs.Size,
		LastAccessed: attrs.Updated,
		ContentType:  attrs.ContentType,
		Hash:         attrs.MD5,
	}

	s.metrics.CacheHits.WithLabelValues("hit").Inc()
	s.metrics.CacheSize.Add(float64(attrs.Size))
	
	s.logger.Debug("Cache hit", 
		zap.String("key", key),
		zap.Int64("size", attrs.Size),
	)

	return reader, entry, nil
}

// Put stores a cache entry in Cloud Storage
func (s *Service) Put(ctx context.Context, key string, data io.Reader, contentType string) error {
	start := time.Now()
	defer func() {
		s.metrics.CacheOperationDuration.WithLabelValues("put").Observe(time.Since(start).Seconds())
	}()

	// Sanitize key
	objectName := s.sanitizeKey(key)
	
	bucket := s.client.Bucket(s.bucketName)
	obj := bucket.Object(objectName)

	// Create writer with metadata
	writer := obj.NewWriter(ctx)
	writer.ContentType = contentType
	writer.Metadata = map[string]string{
		"cache_key":      key,
		"last_accessed":  time.Now().Format(time.RFC3339),
		"stored_at":      time.Now().Format(time.RFC3339),
	}

	// Copy data and calculate hash
	hash := sha256.New()
	tee := io.TeeReader(data, hash)
	
	size, err := io.Copy(writer, tee)
	if err != nil {
		writer.Close()
		s.metrics.CacheErrors.WithLabelValues("write").Inc()
		return fmt.Errorf("failed to write data: %w", err)
	}

	if err := writer.Close(); err != nil {
		s.metrics.CacheErrors.WithLabelValues("close").Inc()
		return fmt.Errorf("failed to close writer: %w", err)
	}

	s.metrics.CacheWrites.Inc()
	s.metrics.CacheSize.Add(float64(size))
	
	s.logger.Debug("Cache write", 
		zap.String("key", key),
		zap.Int64("size", size),
		zap.String("hash", fmt.Sprintf("%x", hash.Sum(nil))),
	)

	return nil
}

// Delete removes a cache entry from Cloud Storage
func (s *Service) Delete(ctx context.Context, key string) error {
	start := time.Now()
	defer func() {
		s.metrics.CacheOperationDuration.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	}()

	objectName := s.sanitizeKey(key)
	
	bucket := s.client.Bucket(s.bucketName)
	obj := bucket.Object(objectName)

	// Get size before deletion for metrics
	attrs, err := obj.Attrs(ctx)
	if err != nil && err != storage.ErrObjectNotExist {
		s.logger.Warn("Failed to get object attributes before deletion", zap.Error(err))
	}

	if err := obj.Delete(ctx); err != nil {
		if err == storage.ErrObjectNotExist {
			return nil // Already deleted
		}
		s.metrics.CacheErrors.WithLabelValues("delete").Inc()
		return fmt.Errorf("failed to delete object: %w", err)
	}

	if attrs != nil {
		s.metrics.CacheSize.Sub(float64(attrs.Size))
	}
	
	s.metrics.CacheDeletions.Inc()
	
	s.logger.Debug("Cache delete", zap.String("key", key))

	return nil
}

// List returns cache entries for pruning analysis
func (s *Service) List(ctx context.Context, prefix string) ([]*CacheEntry, error) {
	bucket := s.client.Bucket(s.bucketName)
	query := &storage.Query{Prefix: prefix}
	
	var entries []*CacheEntry
	it := bucket.Objects(ctx, query)
	
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		// Parse last accessed time
		lastAccessed := attrs.Updated
		if accessedStr, ok := attrs.Metadata["last_accessed"]; ok {
			if parsed, err := time.Parse(time.RFC3339, accessedStr); err == nil {
				lastAccessed = parsed
			}
		}

		entry := &CacheEntry{
			Key:          attrs.Metadata["cache_key"],
			Size:         attrs.Size,
			LastAccessed: lastAccessed,
			ContentType:  attrs.ContentType,
			Hash:         fmt.Sprintf("%x", attrs.MD5),
		}

		if entry.Key == "" {
			entry.Key = attrs.Name
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// GetTotalSize returns the total size of all cached objects
func (s *Service) GetTotalSize(ctx context.Context) (int64, error) {
	bucket := s.client.Bucket(s.bucketName)
	it := bucket.Objects(ctx, nil)
	
	var totalSize int64
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("failed to calculate total size: %w", err)
		}
		totalSize += attrs.Size
	}

	return totalSize, nil
}

// sanitizeKey ensures the key is valid for Cloud Storage object names
func (s *Service) sanitizeKey(key string) string {
	// Replace invalid characters and ensure it doesn't start with '.'
	sanitized := strings.ReplaceAll(key, "/", "_")
	sanitized = strings.ReplaceAll(sanitized, "\\", "_")
	sanitized = strings.ReplaceAll(sanitized, ":", "_")
	
	if strings.HasPrefix(sanitized, ".") {
		sanitized = "cache_" + sanitized
	}
	
	return fmt.Sprintf("cache/%s", sanitized)
}
