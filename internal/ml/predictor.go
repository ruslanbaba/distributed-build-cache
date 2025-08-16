package ml

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/stat"
)

// CachePredictor uses machine learning to predict cache access patterns
type CachePredictor struct {
	model          *PredictionModel
	redisClient    *redis.Client
	logger         *zap.Logger
	
	// Pattern analysis
	accessPatterns map[string]*AccessPattern
	patternMutex   sync.RWMutex
	
	// ML model parameters
	weights        *mat.Dense
	features       []string
	threshold      float64
	
	// Performance tracking
	predictions    map[string]float64
	predictionHits int64
	totalPredictions int64
}

// AccessPattern represents cache access patterns for ML analysis
type AccessPattern struct {
	Key             string            `json:"key"`
	AccessCount     int64             `json:"access_count"`
	LastAccessed    time.Time         `json:"last_accessed"`
	AccessTimes     []time.Time       `json:"access_times"`
	FileSize        int64             `json:"file_size"`
	ContentType     string            `json:"content_type"`
	BuildContext    map[string]string `json:"build_context"`
	DeveloperID     string            `json:"developer_id"`
	ProjectID       string            `json:"project_id"`
	
	// Computed features
	AccessFrequency  float64 `json:"access_frequency"`
	TimeOfDay        float64 `json:"time_of_day"`
	DayOfWeek        float64 `json:"day_of_week"`
	RecentActivity   float64 `json:"recent_activity"`
	RelatedFiles     []string `json:"related_files"`
}

// PredictionModel represents the ML model for cache prediction
type PredictionModel struct {
	ModelType    string                 `json:"model_type"`
	Version      string                 `json:"version"`
	Parameters   map[string]interface{} `json:"parameters"`
	Accuracy     float64                `json:"accuracy"`
	LastTrained  time.Time              `json:"last_trained"`
	TrainingData int64                  `json:"training_data_count"`
}

// NewCachePredictor creates a new ML-based cache predictor
func NewCachePredictor(redisClient *redis.Client, logger *zap.Logger) *CachePredictor {
	predictor := &CachePredictor{
		redisClient:     redisClient,
		logger:          logger,
		accessPatterns:  make(map[string]*AccessPattern),
		predictions:     make(map[string]float64),
		threshold:       0.7,
		features: []string{
			"access_frequency",
			"time_of_day",
			"day_of_week",
			"recent_activity",
			"file_size_normalized",
			"developer_activity",
			"project_activity",
		},
	}

	// Load existing model
	predictor.loadModel()
	
	// Start background tasks
	go predictor.startPatternAnalysis()
	go predictor.startModelTraining()
	
	return predictor
}

// RecordAccess records cache access for pattern analysis
func (cp *CachePredictor) RecordAccess(ctx context.Context, key string, metadata AccessMetadata) {
	cp.patternMutex.Lock()
	defer cp.patternMutex.Unlock()

	pattern, exists := cp.accessPatterns[key]
	if !exists {
		pattern = &AccessPattern{
			Key:         key,
			AccessTimes: make([]time.Time, 0),
			BuildContext: make(map[string]string),
		}
		cp.accessPatterns[key] = pattern
	}

	// Update pattern
	pattern.AccessCount++
	pattern.LastAccessed = time.Now()
	pattern.AccessTimes = append(pattern.AccessTimes, time.Now())
	pattern.FileSize = metadata.FileSize
	pattern.ContentType = metadata.ContentType
	pattern.DeveloperID = metadata.DeveloperID
	pattern.ProjectID = metadata.ProjectID

	// Merge build context
	for k, v := range metadata.BuildContext {
		pattern.BuildContext[k] = v
	}

	// Keep only recent access times (last 100)
	if len(pattern.AccessTimes) > 100 {
		pattern.AccessTimes = pattern.AccessTimes[len(pattern.AccessTimes)-100:]
	}

	// Update computed features
	cp.updatePatternFeatures(pattern)

	// Store in Redis for persistence
	go cp.persistPattern(ctx, pattern)
}

// GetPredictions returns predicted cache keys with confidence scores
func (cp *CachePredictor) GetPredictions(currentKey string) map[string]float64 {
	if cp.model == nil {
		return make(map[string]float64)
	}

	cp.patternMutex.RLock()
	defer cp.patternMutex.RUnlock()

	predictions := make(map[string]float64)
	currentPattern := cp.accessPatterns[currentKey]
	
	if currentPattern == nil {
		return predictions
	}

	// Generate predictions for related keys
	for key, pattern := range cp.accessPatterns {
		if key == currentKey {
			continue
		}

		confidence := cp.calculatePredictionConfidence(currentPattern, pattern)
		if confidence > cp.threshold {
			predictions[key] = confidence
		}
	}

	// Limit to top 10 predictions
	return cp.limitPredictions(predictions, 10)
}

// calculatePredictionConfidence uses ML model to calculate prediction confidence
func (cp *CachePredictor) calculatePredictionConfidence(current, target *AccessPattern) float64 {
	features := cp.extractFeatures(current, target)
	
	if cp.weights == nil {
		// Fallback to heuristic-based prediction
		return cp.heuristicPrediction(current, target)
	}

	// Apply ML model
	featureVector := mat.NewDense(1, len(features), features)
	result := mat.NewDense(1, 1, nil)
	result.Mul(featureVector, cp.weights)
	
	// Apply sigmoid activation
	confidence := 1.0 / (1.0 + math.Exp(-result.At(0, 0)))
	
	return confidence
}

// extractFeatures extracts feature vector for ML prediction
func (cp *CachePredictor) extractFeatures(current, target *AccessPattern) []float64 {
	features := make([]float64, len(cp.features))
	
	// Temporal correlation
	features[0] = cp.calculateTemporalCorrelation(current, target)
	
	// Project correlation
	features[1] = cp.calculateProjectCorrelation(current, target)
	
	// Developer correlation
	features[2] = cp.calculateDeveloperCorrelation(current, target)
	
	// File type correlation
	features[3] = cp.calculateContentTypeCorrelation(current, target)
	
	// Size correlation
	features[4] = cp.calculateSizeCorrelation(current, target)
	
	// Build context correlation
	features[5] = cp.calculateBuildContextCorrelation(current, target)
	
	// Historical co-access pattern
	features[6] = cp.calculateCoAccessPattern(current, target)
	
	return features
}

// heuristicPrediction provides fallback prediction using heuristics
func (cp *CachePredictor) heuristicPrediction(current, target *AccessPattern) float64 {
	score := 0.0
	
	// Same project
	if current.ProjectID == target.ProjectID {
		score += 0.3
	}
	
	// Same developer
	if current.DeveloperID == target.DeveloperID {
		score += 0.2
	}
	
	// Similar content type
	if current.ContentType == target.ContentType {
		score += 0.2
	}
	
	// Recent activity
	if time.Since(target.LastAccessed) < time.Hour {
		score += 0.2
	}
	
	// Similar access patterns
	if cp.calculateTemporalCorrelation(current, target) > 0.5 {
		score += 0.1
	}
	
	return math.Min(score, 1.0)
}

// startPatternAnalysis runs background pattern analysis
func (cp *CachePredictor) startPatternAnalysis() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cp.analyzePatterns()
	}
}

// startModelTraining runs periodic model training
func (cp *CachePredictor) startModelTraining() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		cp.trainModel()
	}
}

// trainModel trains the ML model with collected data
func (cp *CachePredictor) trainModel() {
	cp.logger.Info("Starting ML model training")
	
	trainingData := cp.prepareTrainingData()
	if len(trainingData) < 100 {
		cp.logger.Debug("Insufficient training data", zap.Int("samples", len(trainingData)))
		return
	}

	// Simple linear regression for now - can be upgraded to more sophisticated models
	features, labels := cp.prepareFeatureMatrix(trainingData)
	
	// Calculate weights using least squares
	weights := cp.calculateWeights(features, labels)
	cp.weights = weights

	// Update model metadata
	cp.model = &PredictionModel{
		ModelType:    "linear_regression",
		Version:      "1.0",
		Accuracy:     cp.calculateModelAccuracy(features, labels, weights),
		LastTrained:  time.Now(),
		TrainingData: int64(len(trainingData)),
	}

	cp.logger.Info("ML model training completed",
		zap.Float64("accuracy", cp.model.Accuracy),
		zap.Int64("training_samples", cp.model.TrainingData),
	)
}

// AccessMetadata contains metadata for access recording
type AccessMetadata struct {
	FileSize     int64
	ContentType  string
	DeveloperID  string
	ProjectID    string
	BuildContext map[string]string
}
