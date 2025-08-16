package server

import (
	"context"
	"fmt"
	"io"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ruslanbaba/distributed-build-cache/internal/cache"
	"github.com/ruslanbaba/distributed-build-cache/internal/metrics"
)

// CacheServer implements the BuildCacheService gRPC interface
type CacheServer struct {
	UnimplementedBuildCacheServiceServer
	cache   *cache.Service
	logger  *zap.Logger
	metrics *metrics.Collector
}

// NewCacheServer creates a new cache server
func NewCacheServer(cache *cache.Service, logger *zap.Logger) *CacheServer {
	return &CacheServer{
		cache:  cache,
		logger: logger,
	}
}

// Get retrieves a cached artifact
func (s *CacheServer) Get(req *GetRequest, stream BuildCacheService_GetServer) error {
	start := time.Now()
	defer func() {
		s.metrics.GRPCRequestDuration.WithLabelValues("Get").Observe(time.Since(start).Seconds())
	}()

	if req.Digest == nil {
		s.metrics.GRPCRequestsTotal.WithLabelValues("Get", "invalid_request").Inc()
		return status.Error(codes.InvalidArgument, "digest is required")
	}

	s.logger.Debug("Get request", 
		zap.String("hash", req.Digest.Hash),
		zap.Int64("size", req.Digest.SizeBytes),
		zap.String("instance", req.InstanceName),
	)

	// Generate cache key from digest
	key := fmt.Sprintf("%s/%s", req.InstanceName, req.Digest.Hash)

	// Retrieve from cache
	reader, entry, err := s.cache.Get(stream.Context(), key)
	if err != nil {
		s.logger.Error("Failed to get cache entry",
			zap.String("key", key),
			zap.Error(err),
		)
		s.metrics.GRPCRequestsTotal.WithLabelValues("Get", "error").Inc()
		return status.Error(codes.NotFound, "cache miss")
	}
	defer reader.Close()

	// Stream the data back to client
	buffer := make([]byte, 64*1024) // 64KB chunks
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			response := &GetResponse{
				Data: buffer[:n],
				Digest: &Digest{
					Hash:      req.Digest.Hash,
					SizeBytes: entry.Size,
				},
			}

			if err := stream.Send(response); err != nil {
				s.logger.Error("Failed to send response chunk",
					zap.String("key", key),
					zap.Error(err),
				)
				s.metrics.GRPCRequestsTotal.WithLabelValues("Get", "stream_error").Inc()
				return status.Error(codes.Internal, "failed to stream data")
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			s.logger.Error("Failed to read cache data",
				zap.String("key", key),
				zap.Error(err),
			)
			s.metrics.GRPCRequestsTotal.WithLabelValues("Get", "read_error").Inc()
			return status.Error(codes.Internal, "failed to read cache data")
		}
	}

	s.metrics.GRPCRequestsTotal.WithLabelValues("Get", "success").Inc()
	s.logger.Debug("Get completed successfully",
		zap.String("key", key),
		zap.Int64("size", entry.Size),
	)

	return nil
}

// Put stores an artifact in the cache
func (s *CacheServer) Put(stream BuildCacheService_PutServer) error {
	start := time.Now()
	defer func() {
		s.metrics.GRPCRequestDuration.WithLabelValues("Put").Observe(time.Since(start).Seconds())
	}()

	// Receive first message with metadata
	req, err := stream.Recv()
	if err != nil {
		s.metrics.GRPCRequestsTotal.WithLabelValues("Put", "recv_error").Inc()
		return status.Error(codes.InvalidArgument, "failed to receive metadata")
	}

	if req.Metadata == nil {
		s.metrics.GRPCRequestsTotal.WithLabelValues("Put", "invalid_request").Inc()
		return status.Error(codes.InvalidArgument, "metadata is required")
	}

	metadata := req.Metadata
	if metadata.Digest == nil {
		s.metrics.GRPCRequestsTotal.WithLabelValues("Put", "invalid_request").Inc()
		return status.Error(codes.InvalidArgument, "digest is required")
	}

	s.logger.Debug("Put request", 
		zap.String("hash", metadata.Digest.Hash),
		zap.Int64("size", metadata.Digest.SizeBytes),
		zap.String("instance", metadata.InstanceName),
		zap.String("content_type", metadata.ContentType),
	)

	// Generate cache key
	key := fmt.Sprintf("%s/%s", metadata.InstanceName, metadata.Digest.Hash)

	// Create a pipe to stream data to cache service
	pr, pw := io.Pipe()
	
	// Start goroutine to write to pipe
	errChan := make(chan error, 1)
	go func() {
		defer pw.Close()
		
		// Write first chunk if present
		if len(req.Data) > 0 {
			if _, err := pw.Write(req.Data); err != nil {
				errChan <- err
				return
			}
		}

		// Read remaining chunks
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				errChan <- err
				return
			}

			if len(req.Data) > 0 {
				if _, err := pw.Write(req.Data); err != nil {
					errChan <- err
					return
				}
			}
		}
		errChan <- nil
	}()

	// Store in cache
	if err := s.cache.Put(stream.Context(), key, pr, metadata.ContentType); err != nil {
		s.logger.Error("Failed to store cache entry",
			zap.String("key", key),
			zap.Error(err),
		)
		s.metrics.GRPCRequestsTotal.WithLabelValues("Put", "storage_error").Inc()
		return status.Error(codes.Internal, "failed to store cache entry")
	}

	// Wait for streaming to complete
	if err := <-errChan; err != nil {
		s.logger.Error("Failed to stream data",
			zap.String("key", key),
			zap.Error(err),
		)
		s.metrics.GRPCRequestsTotal.WithLabelValues("Put", "stream_error").Inc()
		return status.Error(codes.Internal, "failed to stream data")
	}

	// Send response
	response := &PutResponse{
		Digest: metadata.Digest,
		Size:   metadata.Digest.SizeBytes,
	}

	if err := stream.SendAndClose(response); err != nil {
		s.logger.Error("Failed to send response",
			zap.String("key", key),
			zap.Error(err),
		)
		s.metrics.GRPCRequestsTotal.WithLabelValues("Put", "response_error").Inc()
		return status.Error(codes.Internal, "failed to send response")
	}

	s.metrics.GRPCRequestsTotal.WithLabelValues("Put", "success").Inc()
	s.logger.Debug("Put completed successfully",
		zap.String("key", key),
		zap.Int64("size", metadata.Digest.SizeBytes),
	)

	return nil
}

// Contains checks if artifacts exist in the cache
func (s *CacheServer) Contains(ctx context.Context, req *ContainsRequest) (*ContainsResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.GRPCRequestDuration.WithLabelValues("Contains").Observe(time.Since(start).Seconds())
	}()

	if len(req.Digests) == 0 {
		s.metrics.GRPCRequestsTotal.WithLabelValues("Contains", "invalid_request").Inc()
		return nil, status.Error(codes.InvalidArgument, "at least one digest is required")
	}

	s.logger.Debug("Contains request", 
		zap.Int("digest_count", len(req.Digests)),
		zap.String("instance", req.InstanceName),
	)

	var results []*ContentAddressableStorageStatus

	for _, digest := range req.Digests {
		key := fmt.Sprintf("%s/%s", req.InstanceName, digest.Hash)
		
		// Check if entry exists (lightweight operation)
		_, _, err := s.cache.Get(ctx, key)
		exists := err == nil

		status := &ContentAddressableStorageStatus{
			Digest: digest,
			Exists: exists,
		}
		results = append(results, status)

		s.logger.Debug("Contains check",
			zap.String("hash", digest.Hash),
			zap.Bool("exists", exists),
		)
	}

	response := &ContainsResponse{
		Results: results,
	}

	s.metrics.GRPCRequestsTotal.WithLabelValues("Contains", "success").Inc()
	return response, nil
}

// GetActionResult retrieves action execution results
func (s *CacheServer) GetActionResult(ctx context.Context, req *GetActionResultRequest) (*ActionResult, error) {
	start := time.Now()
	defer func() {
		s.metrics.GRPCRequestDuration.WithLabelValues("GetActionResult").Observe(time.Since(start).Seconds())
	}()

	if req.ActionDigest == nil {
		s.metrics.GRPCRequestsTotal.WithLabelValues("GetActionResult", "invalid_request").Inc()
		return nil, status.Error(codes.InvalidArgument, "action digest is required")
	}

	key := fmt.Sprintf("%s/action_result/%s", req.InstanceName, req.ActionDigest.Hash)
	
	s.logger.Debug("GetActionResult request", 
		zap.String("hash", req.ActionDigest.Hash),
		zap.String("instance", req.InstanceName),
	)

	// Implementation would deserialize ActionResult from cache
	// For now, return not found
	s.metrics.GRPCRequestsTotal.WithLabelValues("GetActionResult", "not_found").Inc()
	return nil, status.Error(codes.NotFound, "action result not found")
}

// UpdateActionResult stores action execution results
func (s *CacheServer) UpdateActionResult(ctx context.Context, req *UpdateActionResultRequest) (*UpdateActionResultResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.GRPCRequestDuration.WithLabelValues("UpdateActionResult").Observe(time.Since(start).Seconds())
	}()

	if req.ActionDigest == nil {
		s.metrics.GRPCRequestsTotal.WithLabelValues("UpdateActionResult", "invalid_request").Inc()
		return nil, status.Error(codes.InvalidArgument, "action digest is required")
	}

	key := fmt.Sprintf("%s/action_result/%s", req.InstanceName, req.ActionDigest.Hash)
	
	s.logger.Debug("UpdateActionResult request", 
		zap.String("hash", req.ActionDigest.Hash),
		zap.String("instance", req.InstanceName),
	)

	// Implementation would serialize and store ActionResult
	// For now, return success
	response := &UpdateActionResultResponse{
		Success: true,
	}

	s.metrics.GRPCRequestsTotal.WithLabelValues("UpdateActionResult", "success").Inc()
	return response, nil
}
