package server

import (
	"context"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryLoggingInterceptor logs unary RPC calls
func UnaryLoggingInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		
		logger.Debug("Unary RPC started",
			zap.String("method", info.FullMethod),
		)

		resp, err := handler(ctx, req)
		
		duration := time.Since(start)
		
		if err != nil {
			logger.Error("Unary RPC failed",
				zap.String("method", info.FullMethod),
				zap.Duration("duration", duration),
				zap.Error(err),
				zap.String("code", status.Code(err).String()),
			)
		} else {
			logger.Debug("Unary RPC completed",
				zap.String("method", info.FullMethod),
				zap.Duration("duration", duration),
			)
		}

		return resp, err
	}
}

// StreamLoggingInterceptor logs streaming RPC calls
func StreamLoggingInterceptor(logger *zap.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		
		logger.Debug("Stream RPC started",
			zap.String("method", info.FullMethod),
			zap.Bool("client_stream", info.IsClientStream),
			zap.Bool("server_stream", info.IsServerStream),
		)

		err := handler(srv, stream)
		
		duration := time.Since(start)
		
		if err != nil {
			logger.Error("Stream RPC failed",
				zap.String("method", info.FullMethod),
				zap.Duration("duration", duration),
				zap.Error(err),
				zap.String("code", status.Code(err).String()),
			)
		} else {
			logger.Debug("Stream RPC completed",
				zap.String("method", info.FullMethod),
				zap.Duration("duration", duration),
			)
		}

		return err
	}
}

// AuthInterceptor validates authentication
func AuthInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Extract metadata for authentication
		// This would integrate with your auth system
		
		// For now, log the request
		logger.Debug("Auth check",
			zap.String("method", info.FullMethod),
		)

		return handler(ctx, req)
	}
}

// RateLimitInterceptor implements rate limiting
func RateLimitInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Implement rate limiting logic here
		// This could integrate with Redis or in-memory rate limiter
		
		return handler(ctx, req)
	}
}
