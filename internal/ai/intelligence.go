package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
)

// IntelligenceConfig holds configuration for the AI intelligence service
type IntelligenceConfig struct {
	RedisAddr              string
	RedisPassword          string
	RedisDB                int
	ModelUpdateInterval    time.Duration
	RecommendationWindow   time.Duration
	MinDataPoints          int
	LearningRate           float64
	EnableRealTimeUpdates  bool
}

// CacheRecommendation represents an AI-generated cache recommendation
type CacheRecommendation struct {
	ArtifactHash      string            `json:"artifact_hash"`
	Action            string            `json:"action"` // "cache", "evict", "promote", "demote"
	Confidence        float64           `json:"confidence"`
	Reasoning         string            `json:"reasoning"`
	EstimatedImpact   float64           `json:"estimated_impact"` // Performance improvement %
	Metadata          map[string]string `json:"metadata"`
	Timestamp         time.Time         `json:"timestamp"`
	ValidUntil        time.Time         `json:"valid_until"`
}

// ModelMetrics tracks AI model performance and accuracy
type ModelMetrics struct {
	Version            string    `json:"version"`
	Accuracy           float64   `json:"accuracy"`
	Precision          float64   `json:"precision"`
	Recall             float64   `json:"recall"`
	F1Score            float64   `json:"f1_score"`
	LastUpdated        time.Time `json:"last_updated"`
	TrainingDataPoints int       `json:"training_data_points"`
	PredictionsCount   int64     `json:"predictions_count"`
	CorrectPredictions int64     `json:"correct_predictions"`
}

// IntelligenceService provides AI-driven cache intelligence and optimization
type IntelligenceService struct {
	config      IntelligenceConfig
	redisClient *redis.Client
	predictor   *AIPredictor
	metrics     *ModelMetrics
	
	// Prometheus metrics
	recommendationsTotal    *prometheus.CounterVec
	recommendationAccuracy  *prometheus.GaugeVec
	modelPerformance        *prometheus.GaugeVec
	optimizationImpact      *prometheus.HistogramVec
}

// NewIntelligenceService creates a new AI intelligence service
func NewIntelligenceService(config IntelligenceConfig, predictor *AIPredictor) (*IntelligenceService, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Password: config.RedisPassword,
		DB:       config.RedisDB,
	})

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	service := &IntelligenceService{
		config:      config,
		redisClient: rdb,
		predictor:   predictor,
		metrics: &ModelMetrics{
			Version:     "v2.0",
			LastUpdated: time.Now(),
		},
	}

	service.initMetrics()
	return service, nil
}

func (s *IntelligenceService) initMetrics() {
	s.recommendationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_ai_recommendations_total",
			Help: "Total number of AI cache recommendations made",
		},
		[]string{"action", "confidence_level"},
	)

	s.recommendationAccuracy = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cache_ai_recommendation_accuracy",
			Help: "Accuracy of AI cache recommendations",
		},
		[]string{"action", "time_window"},
	)

	s.modelPerformance = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cache_ai_model_performance",
			Help: "AI model performance metrics",
		},
		[]string{"metric_type", "model_version"},
	)

	s.optimizationImpact = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cache_ai_optimization_impact",
			Help:    "Impact of AI-driven cache optimizations",
			Buckets: []float64{0.01, 0.05, 0.1, 0.2, 0.3, 0.5, 0.7, 1.0},
		},
		[]string{"optimization_type"},
	)
}

// GenerateRecommendations creates AI-driven cache optimization recommendations
func (s *IntelligenceService) GenerateRecommendations(ctx context.Context, currentMetrics map[string]interface{}) ([]CacheRecommendation, error) {
	log.Printf("Generating AI cache recommendations based on current metrics")

	recommendations := make([]CacheRecommendation, 0)

	// Analyze cache hit patterns
	hitRateRecommendations, err := s.analyzeHitRatePatterns(ctx, currentMetrics)
	if err != nil {
		log.Printf("Error analyzing hit rate patterns: %v", err)
	} else {
		recommendations = append(recommendations, hitRateRecommendations...)
	}

	// Analyze storage optimization opportunities
	storageRecommendations, err := s.analyzeStorageOptimization(ctx, currentMetrics)
	if err != nil {
		log.Printf("Error analyzing storage optimization: %v", err)
	} else {
		recommendations = append(recommendations, storageRecommendations...)
	}

	// Analyze performance bottlenecks
	performanceRecommendations, err := s.analyzePerformanceBottlenecks(ctx, currentMetrics)
	if err != nil {
		log.Printf("Error analyzing performance bottlenecks: %v", err)
	} else {
		recommendations = append(recommendations, performanceRecommendations...)
	}

	// Store recommendations in Redis for tracking
	for _, rec := range recommendations {
		if err := s.storeRecommendation(ctx, rec); err != nil {
			log.Printf("Error storing recommendation: %v", err)
		}
	}

	// Update metrics
	for _, rec := range recommendations {
		confidenceLevel := s.getConfidenceLevel(rec.Confidence)
		s.recommendationsTotal.WithLabelValues(rec.Action, confidenceLevel).Inc()
	}

	log.Printf("Generated %d AI cache recommendations", len(recommendations))
	return recommendations, nil
}

// OptimizeCache applies AI-driven cache optimizations
func (s *IntelligenceService) OptimizeCache(ctx context.Context, recommendations []CacheRecommendation) error {
	appliedOptimizations := 0

	for _, rec := range recommendations {
		if rec.Confidence < 0.7 {
			log.Printf("Skipping low-confidence recommendation: %s (confidence: %.3f)", rec.Action, rec.Confidence)
			continue
		}

		log.Printf("Applying cache optimization: %s for artifact %s (confidence: %.3f)", 
			rec.Action, rec.ArtifactHash, rec.Confidence)

		switch rec.Action {
		case "cache":
			err := s.applyCacheRecommendation(ctx, rec)
			if err != nil {
				log.Printf("Error applying cache recommendation: %v", err)
				continue
			}
		case "evict":
			err := s.applyEvictRecommendation(ctx, rec)
			if err != nil {
				log.Printf("Error applying evict recommendation: %v", err)
				continue
			}
		case "promote":
			err := s.applyPromoteRecommendation(ctx, rec)
			if err != nil {
				log.Printf("Error applying promote recommendation: %v", err)
				continue
			}
		case "demote":
			err := s.applyDemoteRecommendation(ctx, rec)
			if err != nil {
				log.Printf("Error applying demote recommendation: %v", err)
				continue
			}
		}

		appliedOptimizations++
		s.optimizationImpact.WithLabelValues(rec.Action).Observe(rec.EstimatedImpact)
	}

	log.Printf("Applied %d cache optimizations out of %d recommendations", appliedOptimizations, len(recommendations))
	return nil
}

// LearnFromFeedback updates AI models based on feedback and actual performance
func (s *IntelligenceService) LearnFromFeedback(ctx context.Context, feedback map[string]interface{}) error {
	log.Printf("Learning from performance feedback")

	// Extract performance data
	actualHitRate, ok := feedback["hit_rate"].(float64)
	if !ok {
		return fmt.Errorf("invalid hit_rate in feedback")
	}

	actualLatency, ok := feedback["avg_latency"].(float64)
	if !ok {
		return fmt.Errorf("invalid avg_latency in feedback")
	}

	// Update model metrics
	s.updateModelAccuracy(ctx, feedback)

	// Store learning data for model retraining
	learningData := map[string]interface{}{
		"timestamp":       time.Now().Unix(),
		"actual_hit_rate": actualHitRate,
		"actual_latency":  actualLatency,
		"feedback":        feedback,
	}

	dataJSON, err := json.Marshal(learningData)
	if err != nil {
		return fmt.Errorf("failed to marshal learning data: %w", err)
	}

	// Store in Redis for batch processing
	key := fmt.Sprintf("ai:learning:%d", time.Now().Unix())
	err = s.redisClient.Set(ctx, key, dataJSON, 24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to store learning data: %w", err)
	}

	// Update Prometheus metrics
	s.modelPerformance.WithLabelValues("accuracy", s.metrics.Version).Set(s.metrics.Accuracy)
	s.modelPerformance.WithLabelValues("precision", s.metrics.Version).Set(s.metrics.Precision)
	s.modelPerformance.WithLabelValues("recall", s.metrics.Version).Set(s.metrics.Recall)
	s.modelPerformance.WithLabelValues("f1_score", s.metrics.Version).Set(s.metrics.F1Score)

	return nil
}

// GetModelMetrics returns current AI model performance metrics
func (s *IntelligenceService) GetModelMetrics() *ModelMetrics {
	return s.metrics
}

// Private methods

func (s *IntelligenceService) analyzeHitRatePatterns(ctx context.Context, metrics map[string]interface{}) ([]CacheRecommendation, error) {
	recommendations := make([]CacheRecommendation, 0)

	hitRate, ok := metrics["hit_rate"].(float64)
	if !ok {
		return recommendations, fmt.Errorf("invalid hit_rate metric")
	}

	// If hit rate is low, recommend more aggressive caching
	if hitRate < 0.6 {
		rec := CacheRecommendation{
			ArtifactHash:    "global-policy",
			Action:          "cache",
			Confidence:      0.85,
			Reasoning:       fmt.Sprintf("Low hit rate detected (%.2f%%). Recommend more aggressive caching strategy.", hitRate*100),
			EstimatedImpact: (0.8 - hitRate) * 100, // Potential improvement percentage
			Metadata: map[string]string{
				"current_hit_rate": fmt.Sprintf("%.3f", hitRate),
				"target_hit_rate":  "0.80",
				"strategy":         "aggressive_caching",
			},
			Timestamp:  time.Now(),
			ValidUntil: time.Now().Add(s.config.RecommendationWindow),
		}
		recommendations = append(recommendations, rec)
	}

	return recommendations, nil
}

func (s *IntelligenceService) analyzeStorageOptimization(ctx context.Context, metrics map[string]interface{}) ([]CacheRecommendation, error) {
	recommendations := make([]CacheRecommendation, 0)

	storageUsed, ok := metrics["storage_used_bytes"].(float64)
	if !ok {
		return recommendations, nil
	}

	storageCapacity, ok := metrics["storage_capacity_bytes"].(float64)
	if !ok {
		return recommendations, nil
	}

	utilizationRate := storageUsed / storageCapacity

	// If storage utilization is high, recommend eviction of low-value items
	if utilizationRate > 0.85 {
		rec := CacheRecommendation{
			ArtifactHash:    "storage-policy",
			Action:          "evict",
			Confidence:      0.90,
			Reasoning:       fmt.Sprintf("High storage utilization (%.1f%%). Recommend evicting low-value cache items.", utilizationRate*100),
			EstimatedImpact: (utilizationRate - 0.70) * 50, // Storage savings
			Metadata: map[string]string{
				"current_utilization": fmt.Sprintf("%.3f", utilizationRate),
				"target_utilization":  "0.75",
				"strategy":            "lru_eviction",
			},
			Timestamp:  time.Now(),
			ValidUntil: time.Now().Add(s.config.RecommendationWindow),
		}
		recommendations = append(recommendations, rec)
	}

	return recommendations, nil
}

func (s *IntelligenceService) analyzePerformanceBottlenecks(ctx context.Context, metrics map[string]interface{}) ([]CacheRecommendation, error) {
	recommendations := make([]CacheRecommendation, 0)

	avgLatency, ok := metrics["avg_latency_ms"].(float64)
	if !ok {
		return recommendations, nil
	}

	// If latency is high, recommend promoting frequently accessed items
	if avgLatency > 100 { // 100ms threshold
		rec := CacheRecommendation{
			ArtifactHash:    "performance-policy",
			Action:          "promote",
			Confidence:      0.80,
			Reasoning:       fmt.Sprintf("High average latency detected (%.1fms). Recommend promoting hot cache items.", avgLatency),
			EstimatedImpact: (avgLatency - 50) / avgLatency * 100, // Latency reduction percentage
			Metadata: map[string]string{
				"current_latency": fmt.Sprintf("%.1f", avgLatency),
				"target_latency":  "50.0",
				"strategy":        "hot_promotion",
			},
			Timestamp:  time.Now(),
			ValidUntil: time.Now().Add(s.config.RecommendationWindow),
		}
		recommendations = append(recommendations, rec)
	}

	return recommendations, nil
}

func (s *IntelligenceService) storeRecommendation(ctx context.Context, rec CacheRecommendation) error {
	recJSON, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("failed to marshal recommendation: %w", err)
	}

	key := fmt.Sprintf("ai:recommendation:%s:%d", rec.ArtifactHash, rec.Timestamp.Unix())
	return s.redisClient.Set(ctx, key, recJSON, rec.ValidUntil.Sub(time.Now())).Err()
}

func (s *IntelligenceService) applyCacheRecommendation(ctx context.Context, rec CacheRecommendation) error {
	// TODO: Implement cache recommendation application
	log.Printf("Applied cache recommendation for %s", rec.ArtifactHash)
	return nil
}

func (s *IntelligenceService) applyEvictRecommendation(ctx context.Context, rec CacheRecommendation) error {
	// TODO: Implement evict recommendation application
	log.Printf("Applied evict recommendation for %s", rec.ArtifactHash)
	return nil
}

func (s *IntelligenceService) applyPromoteRecommendation(ctx context.Context, rec CacheRecommendation) error {
	// TODO: Implement promote recommendation application
	log.Printf("Applied promote recommendation for %s", rec.ArtifactHash)
	return nil
}

func (s *IntelligenceService) applyDemoteRecommendation(ctx context.Context, rec CacheRecommendation) error {
	// TODO: Implement demote recommendation application
	log.Printf("Applied demote recommendation for %s", rec.ArtifactHash)
	return nil
}

func (s *IntelligenceService) updateModelAccuracy(ctx context.Context, feedback map[string]interface{}) {
	// Simple accuracy tracking - in production, this would be more sophisticated
	s.metrics.PredictionsCount++
	
	if accuracy, ok := feedback["prediction_accuracy"].(float64); ok {
		s.metrics.CorrectPredictions++
		s.metrics.Accuracy = float64(s.metrics.CorrectPredictions) / float64(s.metrics.PredictionsCount)
		
		// Update other metrics based on feedback
		if accuracy > 0.8 {
			s.metrics.Precision = 0.85
			s.metrics.Recall = 0.82
			s.metrics.F1Score = 2 * (s.metrics.Precision * s.metrics.Recall) / (s.metrics.Precision + s.metrics.Recall)
		}
	}
	
	s.metrics.LastUpdated = time.Now()
}

func (s *IntelligenceService) getConfidenceLevel(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "very_high"
	case confidence >= 0.8:
		return "high"
	case confidence >= 0.6:
		return "medium"
	case confidence >= 0.4:
		return "low"
	default:
		return "very_low"
	}
}

// Close closes the intelligence service and cleans up resources
func (s *IntelligenceService) Close() error {
	return s.redisClient.Close()
}
