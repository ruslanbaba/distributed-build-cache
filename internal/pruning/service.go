package pruning

import (
	"context"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/ruslanbaba/distributed-build-cache/internal/cache"
	"github.com/ruslanbaba/distributed-build-cache/internal/metrics"
)

// Service handles intelligent cache pruning to optimize storage costs
type Service struct {
	cache   *cache.Service
	logger  *zap.Logger
	metrics *metrics.Collector
	config  Config
}

// Config contains pruning configuration
type Config struct {
	MaxCacheSize    int64         // Maximum cache size in bytes
	PruningInterval time.Duration // How often to run pruning
	RetentionDays   int           // Minimum retention period in days
}

// NewService creates a new pruning service
func NewService(cache *cache.Service, logger *zap.Logger, metrics *metrics.Collector, config Config) *Service {
	return &Service{
		cache:   cache,
		logger:  logger,
		metrics: metrics,
		config:  config,
	}
}

// Start begins the pruning service
func (s *Service) Start(ctx context.Context) {
	ticker := time.NewTicker(s.config.PruningInterval)
	defer ticker.Stop()

	s.logger.Info("Starting cache pruning service",
		zap.Duration("interval", s.config.PruningInterval),
		zap.Int64("max_size_gb", s.config.MaxCacheSize/(1024*1024*1024)),
		zap.Int("retention_days", s.config.RetentionDays),
	)

	// Run initial pruning
	if err := s.RunPruning(ctx); err != nil {
		s.logger.Error("Initial pruning failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Stopping pruning service")
			return
		case <-ticker.C:
			if err := s.RunPruning(ctx); err != nil {
				s.logger.Error("Pruning failed", zap.Error(err))
				s.metrics.PruningErrors.Inc()
			}
		}
	}
}

// RunPruning executes the cache pruning algorithm
func (s *Service) RunPruning(ctx context.Context) error {
	start := time.Now()
	defer func() {
		s.metrics.PruningDuration.Observe(time.Since(start).Seconds())
	}()

	s.logger.Info("Starting cache pruning cycle")

	// Get current total size
	totalSize, err := s.cache.GetTotalSize(ctx)
	if err != nil {
		return err
	}

	s.logger.Info("Current cache state",
		zap.Int64("total_size_mb", totalSize/(1024*1024)),
		zap.Int64("max_size_mb", s.config.MaxCacheSize/(1024*1024)),
	)

	// Check if pruning is needed
	if totalSize <= s.config.MaxCacheSize {
		s.logger.Info("Cache size within limits, no pruning needed")
		return nil
	}

	// Calculate target size (80% of max to provide buffer)
	targetSize := int64(float64(s.config.MaxCacheSize) * 0.8)
	bytesToRemove := totalSize - targetSize

	s.logger.Info("Pruning required",
		zap.Int64("bytes_to_remove_mb", bytesToRemove/(1024*1024)),
		zap.Int64("target_size_mb", targetSize/(1024*1024)),
	)

	// Get all cache entries
	entries, err := s.cache.List(ctx, "")
	if err != nil {
		return err
	}

	// Apply pruning strategies
	toDelete := s.selectEntriesForDeletion(entries, bytesToRemove)

	// Delete selected entries
	var deletedCount int
	var deletedSize int64
	for _, entry := range toDelete {
		if err := s.cache.Delete(ctx, entry.Key); err != nil {
			s.logger.Error("Failed to delete cache entry", 
				zap.String("key", entry.Key),
				zap.Error(err),
			)
			continue
		}
		deletedCount++
		deletedSize += entry.Size
	}

	s.metrics.PrunedEntries.Add(float64(deletedCount))
	s.metrics.PrunedBytes.Add(float64(deletedSize))

	s.logger.Info("Pruning completed",
		zap.Int("deleted_count", deletedCount),
		zap.Int64("deleted_size_mb", deletedSize/(1024*1024)),
		zap.Duration("duration", time.Since(start)),
	)

	return nil
}

// selectEntriesForDeletion implements intelligent pruning strategies
func (s *Service) selectEntriesForDeletion(entries []*cache.CacheEntry, bytesToRemove int64) []*cache.CacheEntry {
	now := time.Now()
	retentionCutoff := now.AddDate(0, 0, -s.config.RetentionDays)

	var candidates []*cache.CacheEntry
	var toDelete []*cache.CacheEntry

	// First pass: Remove entries older than retention period
	for _, entry := range entries {
		if entry.LastAccessed.Before(retentionCutoff) {
			toDelete = append(toDelete, entry)
			bytesToRemove -= entry.Size
		} else {
			candidates = append(candidates, entry)
		}
	}

	// If we still need to remove more, apply LRU strategy
	if bytesToRemove > 0 && len(candidates) > 0 {
		// Sort by last accessed time (oldest first)
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].LastAccessed.Before(candidates[j].LastAccessed)
		})

		// Add LRU entries until we reach target
		for i, entry := range candidates {
			if bytesToRemove <= 0 {
				break
			}

			// Apply additional heuristics
			if s.shouldDelete(entry, now) {
				toDelete = append(toDelete, entry)
				bytesToRemove -= entry.Size
				
				// Remove from candidates to avoid double processing
				candidates = append(candidates[:i], candidates[i+1:]...)
			}
		}

		// If still need more space, delete more LRU entries
		for _, entry := range candidates {
			if bytesToRemove <= 0 {
				break
			}
			toDelete = append(toDelete, entry)
			bytesToRemove -= entry.Size
		}
	}

	s.logger.Info("Pruning strategy results",
		zap.Int("total_entries", len(entries)),
		zap.Int("candidates_for_deletion", len(toDelete)),
		zap.Int64("estimated_space_freed_mb", func() int64 {
			var total int64
			for _, entry := range toDelete {
				total += entry.Size
			}
			return total / (1024 * 1024)
		}()),
	)

	return toDelete
}

// shouldDelete applies additional heuristics for intelligent pruning
func (s *Service) shouldDelete(entry *cache.CacheEntry, now time.Time) bool {
	// Age-based scoring
	age := now.Sub(entry.LastAccessed)
	
	// Delete if not accessed in last week
	if age > 7*24*time.Hour {
		return true
	}
	
	// Delete large files that haven't been accessed recently
	if entry.Size > 100*1024*1024 && age > 3*24*time.Hour { // 100MB, 3 days
		return true
	}
	
	// Keep recently accessed files
	if age < 24*time.Hour {
		return false
	}
	
	// Default to LRU behavior
	return true
}
