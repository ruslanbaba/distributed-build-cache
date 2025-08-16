package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	aiplatform "cloud.google.com/go/aiplatform/apiv1"
	"cloud.google.com/go/aiplatform/apiv1/aiplatformpb"
	"google.golang.org/protobuf/types/known/structpb"
	"github.com/prometheus/client_golang/prometheus"
)

// PredictorConfig holds configuration for the AI predictor service
type PredictorConfig struct {
	ProjectID           string
	Region              string
	ModelEndpoint       string
	PredictionWindow    time.Duration
	ConfidenceThreshold float64
	EnableGPU          bool
}

// CachePrediction represents a cache prediction with confidence score
type CachePrediction struct {
	ArtifactHash     string    `json:"artifact_hash"`
	PredictedHit     bool      `json:"predicted_hit"`
	Confidence       float64   `json:"confidence"`
	Timestamp        time.Time `json:"timestamp"`
	RecommendedTTL   int64     `json:"recommended_ttl_seconds"`
	Priority         int       `json:"priority"` // 1-10, 10 being highest
	EstimatedSize    int64     `json:"estimated_size_bytes"`
}

// BuildPattern represents historical build patterns for ML training
type BuildPattern struct {
	DeveloperID      string            `json:"developer_id"`
	ProjectID        string            `json:"project_id"`
	BuildTargets     []string          `json:"build_targets"`
	Timestamp        time.Time         `json:"timestamp"`
	Duration         time.Duration     `json:"duration"`
	CacheHitRate     float64           `json:"cache_hit_rate"`
	ArtifactsUsed    []string          `json:"artifacts_used"`
	TimeOfDay        int               `json:"hour_of_day"`
	DayOfWeek        int               `json:"day_of_week"`
	BuildType        string            `json:"build_type"` // debug, release, test
	GitCommitSHA     string            `json:"git_commit_sha"`
	ChangedFiles     []string          `json:"changed_files"`
	Dependencies     map[string]string `json:"dependencies"`
}

// AIPredictor implements intelligent cache prediction using Vertex AI
type AIPredictor struct {
	config           PredictorConfig
	client           *aiplatform.PredictionClient
	metricsCollector *prometheus.Registry
	
	// Metrics
	predictionsTotal    *prometheus.CounterVec
	predictionLatency   *prometheus.HistogramVec
	predictionConfidence *prometheus.HistogramVec
	modelAccuracy       *prometheus.GaugeVec
}

// NewAIPredictor creates a new AI-powered cache predictor
func NewAIPredictor(config PredictorConfig) (*AIPredictor, error) {
	ctx := context.Background()
	
	client, err := aiplatform.NewPredictionClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create prediction client: %w", err)
	}

	predictor := &AIPredictor{
		config:           config,
		client:           client,
		metricsCollector: prometheus.NewRegistry(),
	}

	predictor.initMetrics()
	return predictor, nil
}

// validateBuildPattern validates input build patterns for security
func (p *AIPredictor) validateBuildPattern(pattern BuildPattern) error {
	// Validate required fields
	if pattern.DeveloperID == "" {
		return fmt.Errorf("developer ID cannot be empty")
	}
	if pattern.ProjectID == "" {
		return fmt.Errorf("project ID cannot be empty")
	}

	// Validate string lengths to prevent buffer overflow
	if len(pattern.DeveloperID) > 100 {
		return fmt.Errorf("developer ID too long")
	}
	if len(pattern.ProjectID) > 100 {
		return fmt.Errorf("project ID too long")
	}
	if len(pattern.GitCommitSHA) > 0 && len(pattern.GitCommitSHA) != 40 {
		return fmt.Errorf("invalid git commit SHA format")
	}

	// Validate build targets
	for _, target := range pattern.BuildTargets {
		if len(target) > 500 {
			return fmt.Errorf("build target too long: %s", target)
		}
		// Basic Bazel target format validation
		if !strings.HasPrefix(target, "//") {
			return fmt.Errorf("invalid build target format: %s", target)
		}
	}

	// Validate file paths
	for _, file := range pattern.ChangedFiles {
		if len(file) > 1000 {
			return fmt.Errorf("file path too long: %s", file)
		}
		if strings.Contains(file, "..") {
			return fmt.Errorf("file path contains directory traversal: %s", file)
		}
	}

	// Validate build type
	validBuildTypes := []string{"debug", "release", "test", ""}
	isValidBuildType := false
	for _, validType := range validBuildTypes {
		if pattern.BuildType == validType {
			isValidBuildType = true
			break
		}
	}
	if !isValidBuildType {
		return fmt.Errorf("invalid build type: %s", pattern.BuildType)
	}

	return nil
}

func (p *AIPredictor) initMetrics() {
	p.predictionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_ai_predictions_total",
			Help: "Total number of AI cache predictions made",
		},
		[]string{"prediction_type", "confidence_level"},
	)

	p.predictionLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cache_ai_prediction_duration_seconds",
			Help:    "Time spent making AI predictions",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"model_version"},
	)

	p.predictionConfidence = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cache_ai_prediction_confidence",
			Help:    "Confidence scores of AI predictions",
			Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99},
		},
		[]string{"prediction_type"},
	)

	p.modelAccuracy = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cache_ai_model_accuracy",
			Help: "Current accuracy of the AI prediction model",
		},
		[]string{"model_version", "time_window"},
	)

	p.metricsCollector.MustRegister(
		p.predictionsTotal,
		p.predictionLatency,
		p.predictionConfidence,
		p.modelAccuracy,
	)
}

// PredictCacheHit predicts whether a specific artifact will be a cache hit
func (p *AIPredictor) PredictCacheHit(ctx context.Context, pattern BuildPattern) (*CachePrediction, error) {
	start := time.Now()
	defer func() {
		p.predictionLatency.WithLabelValues("v2.0").Observe(time.Since(start).Seconds())
	}()

	// SECURITY: Validate input pattern
	if err := p.validateBuildPattern(pattern); err != nil {
		return nil, fmt.Errorf("invalid build pattern: %w", err)
	}

	// Prepare feature vector for ML model
	features := p.buildFeatureVector(pattern)
	
	// Convert to Vertex AI format
	instance, err := structpb.NewStruct(features)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance struct: %w", err)
	}

	// Make prediction request
	req := &aiplatformpb.PredictRequest{
		Endpoint:  p.config.ModelEndpoint,
		Instances: []*structpb.Value{structpb.NewStructValue(instance)},
	}

	resp, err := p.client.Predict(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("prediction request failed: %w", err)
	}

	// Parse prediction response
	prediction, err := p.parsePrediction(resp, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to parse prediction: %w", err)
	}

	// Update metrics
	confidenceLevel := p.getConfidenceLevel(prediction.Confidence)
	predictionType := "hit"
	if !prediction.PredictedHit {
		predictionType = "miss"
	}

	p.predictionsTotal.WithLabelValues(predictionType, confidenceLevel).Inc()
	p.predictionConfidence.WithLabelValues(predictionType).Observe(prediction.Confidence)

	log.Printf("AI Prediction: hash=%s, hit=%v, confidence=%.3f, ttl=%ds", 
		prediction.ArtifactHash, prediction.PredictedHit, prediction.Confidence, prediction.RecommendedTTL)

	return prediction, nil
}

// PredictBuildPatterns predicts future build patterns for proactive caching
func (p *AIPredictor) PredictBuildPatterns(ctx context.Context, historicalPatterns []BuildPattern) ([]BuildPattern, error) {
	features := make(map[string]interface{})
	
	// Aggregate historical patterns into time series features
	features["historical_patterns"] = p.aggregatePatterns(historicalPatterns)
	features["time_features"] = p.extractTimeFeatures(time.Now())
	features["sequence_length"] = len(historicalPatterns)
	
	instance, err := structpb.NewStruct(features)
	if err != nil {
		return nil, fmt.Errorf("failed to create patterns instance: %w", err)
	}

	req := &aiplatformpb.PredictRequest{
		Endpoint:  p.config.ModelEndpoint,
		Instances: []*structpb.Value{structpb.NewStructValue(instance)},
	}

	resp, err := p.client.Predict(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("pattern prediction failed: %w", err)
	}

	return p.parsePatternPredictions(resp)
}

// WarmCache proactively warms the cache based on AI predictions
func (p *AIPredictor) WarmCache(ctx context.Context, predictions []CachePrediction) error {
	highConfidencePredictions := make([]CachePrediction, 0)
	
	// Filter high-confidence predictions
	for _, pred := range predictions {
		if pred.Confidence >= p.config.ConfidenceThreshold && pred.Priority >= 7 {
			highConfidencePredictions = append(highConfidencePredictions, pred)
		}
	}

	log.Printf("Starting cache warming for %d high-confidence predictions", len(highConfidencePredictions))

	// TODO: Implement cache warming logic
	// This would interface with the cache service to pre-load artifacts
	
	return nil
}

// buildFeatureVector creates ML features from build pattern
func (p *AIPredictor) buildFeatureVector(pattern BuildPattern) map[string]interface{} {
	features := map[string]interface{}{
		// Temporal features
		"hour_of_day":           pattern.TimeOfDay,
		"day_of_week":          pattern.DayOfWeek,
		"days_since_epoch":     time.Since(time.Unix(0, 0)).Hours() / 24,
		
		// Build characteristics
		"build_type":           pattern.BuildType,
		"target_count":         len(pattern.BuildTargets),
		"changed_files_count":  len(pattern.ChangedFiles),
		"dependency_count":     len(pattern.Dependencies),
		
		// Historical performance
		"historical_hit_rate":  pattern.CacheHitRate,
		"last_build_duration":  pattern.Duration.Seconds(),
		
		// Developer patterns
		"developer_id_hash":    hashString(pattern.DeveloperID),
		"project_id_hash":      hashString(pattern.ProjectID),
		
		// File change patterns
		"file_extensions":      p.extractFileExtensions(pattern.ChangedFiles),
		"change_magnitude":     float64(len(pattern.ChangedFiles)),
		
		// Git information
		"commit_recency":       time.Since(pattern.Timestamp).Hours(),
	}

	// Add target similarity features
	features["target_similarity"] = p.calculateTargetSimilarity(pattern.BuildTargets)
	
	return features
}

func (p *AIPredictor) parsePrediction(resp *aiplatformpb.PredictResponse, pattern BuildPattern) (*CachePrediction, error) {
	if len(resp.Predictions) == 0 {
		return nil, fmt.Errorf("no predictions returned")
	}

	predictionData := resp.Predictions[0].GetStructValue().AsMap()
	
	predictedHit, ok := predictionData["predicted_hit"].(bool)
	if !ok {
		return nil, fmt.Errorf("invalid predicted_hit format")
	}

	confidence, ok := predictionData["confidence"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid confidence format")
	}

	ttl, ok := predictionData["recommended_ttl"].(float64)
	if !ok {
		ttl = 3600 // Default 1 hour
	}

	priority, ok := predictionData["priority"].(float64)
	if !ok {
		priority = 5 // Default medium priority
	}

	// Generate artifact hash from build pattern
	artifactHash := p.generateArtifactHash(pattern)

	return &CachePrediction{
		ArtifactHash:     artifactHash,
		PredictedHit:     predictedHit,
		Confidence:       confidence,
		Timestamp:        time.Now(),
		RecommendedTTL:   int64(ttl),
		Priority:         int(priority),
		EstimatedSize:    p.estimateArtifactSize(pattern),
	}, nil
}

func (p *AIPredictor) getConfidenceLevel(confidence float64) string {
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

// Helper functions
func hashString(s string) float64 {
	hash := uint64(0)
	for _, c := range s {
		hash = hash*31 + uint64(c)
	}
	return float64(hash % 1000000) // Normalize to reasonable range
}

func (p *AIPredictor) extractFileExtensions(files []string) []string {
	extensions := make(map[string]bool)
	for _, file := range files {
		if idx := len(file) - 1; idx >= 0 {
			for i := idx; i >= 0; i-- {
				if file[i] == '.' {
					ext := file[i:]
					extensions[ext] = true
					break
				}
			}
		}
	}
	
	result := make([]string, 0, len(extensions))
	for ext := range extensions {
		result = append(result, ext)
	}
	return result
}

func (p *AIPredictor) calculateTargetSimilarity(targets []string) float64 {
	// Simple similarity calculation based on common prefixes
	if len(targets) <= 1 {
		return 1.0
	}
	
	commonPrefixes := 0
	for i := 0; i < len(targets)-1; i++ {
		for j := i + 1; j < len(targets); j++ {
			if len(targets[i]) > 0 && len(targets[j]) > 0 && targets[i][0] == targets[j][0] {
				commonPrefixes++
			}
		}
	}
	
	maxPossible := len(targets) * (len(targets) - 1) / 2
	if maxPossible == 0 {
		return 1.0
	}
	
	return float64(commonPrefixes) / float64(maxPossible)
}

func (p *AIPredictor) generateArtifactHash(pattern BuildPattern) string {
	data, _ := json.Marshal(map[string]interface{}{
		"targets":     pattern.BuildTargets,
		"build_type":  pattern.BuildType,
		"commit_sha":  pattern.GitCommitSHA,
		"project_id":  pattern.ProjectID,
	})
	return fmt.Sprintf("%x", hashString(string(data)))
}

func (p *AIPredictor) estimateArtifactSize(pattern BuildPattern) int64 {
	// Simple heuristic based on target count and build type
	baseSize := int64(1024 * 1024) // 1MB base
	
	targetMultiplier := int64(len(pattern.BuildTargets))
	if targetMultiplier == 0 {
		targetMultiplier = 1
	}
	
	buildTypeMultiplier := int64(1)
	switch pattern.BuildType {
	case "release":
		buildTypeMultiplier = 3
	case "debug":
		buildTypeMultiplier = 2
	case "test":
		buildTypeMultiplier = 1
	}
	
	return baseSize * targetMultiplier * buildTypeMultiplier
}

func (p *AIPredictor) aggregatePatterns(patterns []BuildPattern) interface{} {
	// Aggregate patterns into time series features for sequence prediction
	aggregated := make(map[string]interface{})
	
	if len(patterns) == 0 {
		return aggregated
	}
	
	// Calculate averages and trends
	totalDuration := time.Duration(0)
	totalHitRate := 0.0
	buildTypes := make(map[string]int)
	
	for _, pattern := range patterns {
		totalDuration += pattern.Duration
		totalHitRate += pattern.CacheHitRate
		buildTypes[pattern.BuildType]++
	}
	
	aggregated["avg_duration"] = totalDuration.Seconds() / float64(len(patterns))
	aggregated["avg_hit_rate"] = totalHitRate / float64(len(patterns))
	aggregated["build_type_distribution"] = buildTypes
	aggregated["pattern_count"] = len(patterns)
	
	return aggregated
}

func (p *AIPredictor) extractTimeFeatures(t time.Time) map[string]interface{} {
	return map[string]interface{}{
		"hour":       t.Hour(),
		"day":        t.Day(),
		"month":      int(t.Month()),
		"weekday":    int(t.Weekday()),
		"quarter":    (int(t.Month()) - 1) / 3 + 1,
		"is_weekend": t.Weekday() == time.Saturday || t.Weekday() == time.Sunday,
	}
}

func (p *AIPredictor) parsePatternPredictions(resp *aiplatformpb.PredictResponse) ([]BuildPattern, error) {
	predictions := make([]BuildPattern, 0)
	
	for _, prediction := range resp.Predictions {
		data := prediction.GetStructValue().AsMap()
		
		// Parse predicted build pattern
		pattern := BuildPattern{
			Timestamp: time.Now().Add(time.Hour), // Predict 1 hour ahead
			// TODO: Parse other fields from prediction response
		}
		
		predictions = append(predictions, pattern)
	}
	
	return predictions, nil
}

// Close closes the AI predictor and cleans up resources
func (p *AIPredictor) Close() error {
	return p.client.Close()
}
