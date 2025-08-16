package chaos

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ChaosEngineer implements chaos engineering practices for resilience testing
type ChaosEngineer struct {
	k8sClient    kubernetes.Interface
	logger       *zap.Logger
	enabled      bool
	experiments  []ChaosExperiment
	scheduler    *ExperimentScheduler
}

// ChaosExperiment defines a chaos engineering experiment
type ChaosExperiment struct {
	Name        string        `yaml:"name"`
	Type        string        `yaml:"type"` // pod-kill, network-delay, cpu-stress, memory-stress
	Target      Target        `yaml:"target"`
	Duration    time.Duration `yaml:"duration"`
	Probability float64       `yaml:"probability"` // 0.0 to 1.0
	Schedule    string        `yaml:"schedule"`    // cron expression
	Enabled     bool          `yaml:"enabled"`
	Rollback    RollbackConfig `yaml:"rollback"`
}

// Target defines the target for chaos experiments
type Target struct {
	Namespace     string            `yaml:"namespace"`
	LabelSelector map[string]string `yaml:"labelSelector"`
	PodName       string            `yaml:"podName,omitempty"`
	Percentage    int               `yaml:"percentage"` // percentage of pods to affect
}

// RollbackConfig defines automatic rollback conditions
type RollbackConfig struct {
	Enabled           bool          `yaml:"enabled"`
	HealthCheckURL    string        `yaml:"healthCheckURL"`
	MaxErrorRate      float64       `yaml:"maxErrorRate"`
	CheckInterval     time.Duration `yaml:"checkInterval"`
	RollbackTimeout   time.Duration `yaml:"rollbackTimeout"`
}

// NewChaosEngineer creates a new chaos engineer
func NewChaosEngineer(k8sClient kubernetes.Interface, logger *zap.Logger) *ChaosEngineer {
	return &ChaosEngineer{
		k8sClient: k8sClient,
		logger:    logger,
		enabled:   true,
		scheduler: NewExperimentScheduler(logger),
	}
}

// LoadExperiments loads chaos experiments from configuration
func (ce *ChaosEngineer) LoadExperiments(configPath string) error {
	// Load experiments from YAML configuration
	experiments := []ChaosExperiment{
		{
			Name:        "pod-kill-random",
			Type:        "pod-kill",
			Target:      Target{
				Namespace:     "build-cache",
				LabelSelector: map[string]string{"app": "build-cache-server"},
				Percentage:    20,
			},
			Duration:    5 * time.Minute,
			Probability: 0.1,
			Schedule:    "0 */6 * * *", // Every 6 hours
			Enabled:     true,
			Rollback: RollbackConfig{
				Enabled:         true,
				HealthCheckURL:  "http://build-cache-server:9090/health",
				MaxErrorRate:    0.05,
				CheckInterval:   30 * time.Second,
				RollbackTimeout: 2 * time.Minute,
			},
		},
		{
			Name:        "network-latency-injection",
			Type:        "network-delay",
			Target:      Target{
				Namespace:     "build-cache",
				LabelSelector: map[string]string{"app": "build-cache-server"},
				Percentage:    10,
			},
			Duration:    10 * time.Minute,
			Probability: 0.05,
			Schedule:    "0 */8 * * *", // Every 8 hours
			Enabled:     true,
		},
		{
			Name:        "cpu-stress-test",
			Type:        "cpu-stress",
			Target:      Target{
				Namespace:     "build-cache",
				LabelSelector: map[string]string{"app": "build-cache-server"},
				Percentage:    30,
			},
			Duration:    5 * time.Minute,
			Probability: 0.08,
			Schedule:    "0 */12 * * *", // Every 12 hours
			Enabled:     true,
		},
		{
			Name:        "memory-pressure-test",
			Type:        "memory-stress",
			Target:      Target{
				Namespace:     "build-cache",
				LabelSelector: map[string]string{"app": "build-cache-server"},
				Percentage:    20,
			},
			Duration:    3 * time.Minute,
			Probability: 0.06,
			Schedule:    "0 */10 * * *", // Every 10 hours
			Enabled:     true,
		},
	}

	ce.experiments = experiments
	return nil
}

// Start begins chaos engineering experiments
func (ce *ChaosEngineer) Start(ctx context.Context) error {
	if !ce.enabled {
		ce.logger.Info("Chaos engineering is disabled")
		return nil
	}

	ce.logger.Info("Starting chaos engineering experiments")

	// Schedule experiments
	for _, experiment := range ce.experiments {
		if experiment.Enabled {
			ce.scheduler.Schedule(experiment, ce.executeExperiment)
		}
	}

	return ce.scheduler.Start(ctx)
}

// executeExperiment executes a specific chaos experiment
func (ce *ChaosEngineer) executeExperiment(ctx context.Context, experiment ChaosExperiment) error {
	// Check probability
	if rand.Float64() > experiment.Probability {
		ce.logger.Debug("Skipping experiment due to probability",
			zap.String("experiment", experiment.Name),
			zap.Float64("probability", experiment.Probability),
		)
		return nil
	}

	ce.logger.Info("Executing chaos experiment",
		zap.String("experiment", experiment.Name),
		zap.String("type", experiment.Type),
		zap.Duration("duration", experiment.Duration),
	)

	// Start health monitoring
	var healthMonitor *HealthMonitor
	if experiment.Rollback.Enabled {
		healthMonitor = NewHealthMonitor(experiment.Rollback, ce.logger)
		go healthMonitor.Start(ctx, func() {
			ce.logger.Warn("Health check failed, rolling back experiment",
				zap.String("experiment", experiment.Name),
			)
			ce.rollbackExperiment(ctx, experiment)
		})
	}

	// Execute experiment based on type
	var err error
	switch experiment.Type {
	case "pod-kill":
		err = ce.executePodKillExperiment(ctx, experiment)
	case "network-delay":
		err = ce.executeNetworkDelayExperiment(ctx, experiment)
	case "cpu-stress":
		err = ce.executeCPUStressExperiment(ctx, experiment)
	case "memory-stress":
		err = ce.executeMemoryStressExperiment(ctx, experiment)
	default:
		return fmt.Errorf("unknown experiment type: %s", experiment.Type)
	}

	if err != nil {
		ce.logger.Error("Experiment execution failed",
			zap.String("experiment", experiment.Name),
			zap.Error(err),
		)
		return err
	}

	// Wait for experiment duration
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(experiment.Duration):
		// Experiment completed
	}

	// Stop health monitoring
	if healthMonitor != nil {
		healthMonitor.Stop()
	}

	// Clean up experiment
	ce.cleanupExperiment(ctx, experiment)

	ce.logger.Info("Chaos experiment completed",
		zap.String("experiment", experiment.Name),
	)

	return nil
}

// executePodKillExperiment kills random pods
func (ce *ChaosEngineer) executePodKillExperiment(ctx context.Context, experiment ChaosExperiment) error {
	// Get target pods
	pods, err := ce.getTargetPods(ctx, experiment.Target)
	if err != nil {
		return err
	}

	if len(pods) == 0 {
		return fmt.Errorf("no target pods found")
	}

	// Calculate number of pods to kill
	numToKill := (len(pods) * experiment.Target.Percentage) / 100
	if numToKill == 0 {
		numToKill = 1
	}

	// Randomly select pods to kill
	rand.Shuffle(len(pods), func(i, j int) {
		pods[i], pods[j] = pods[j], pods[i]
	})

	for i := 0; i < numToKill && i < len(pods); i++ {
		pod := pods[i]
		ce.logger.Info("Killing pod",
			zap.String("pod", pod.Name),
			zap.String("namespace", pod.Namespace),
		)

		err := ce.k8sClient.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if err != nil {
			ce.logger.Error("Failed to kill pod",
				zap.String("pod", pod.Name),
				zap.Error(err),
			)
		}
	}

	return nil
}

// executeNetworkDelayExperiment injects network latency
func (ce *ChaosEngineer) executeNetworkDelayExperiment(ctx context.Context, experiment ChaosExperiment) error {
	// This would typically use tools like tc (traffic control) or chaos mesh
	ce.logger.Info("Network delay experiment - would inject latency using network chaos tools")
	return nil
}

// executeCPUStressExperiment creates CPU stress
func (ce *ChaosEngineer) executeCPUStressExperiment(ctx context.Context, experiment ChaosExperiment) error {
	// This would create CPU stress using stress-ng or similar tools
	ce.logger.Info("CPU stress experiment - would create CPU load using stress tools")
	return nil
}

// executeMemoryStressExperiment creates memory pressure
func (ce *ChaosEngineer) executeMemoryStressExperiment(ctx context.Context, experiment ChaosExperiment) error {
	// This would create memory pressure using stress-ng or similar tools
	ce.logger.Info("Memory stress experiment - would create memory pressure using stress tools")
	return nil
}

// HealthMonitor monitors system health during experiments
type HealthMonitor struct {
	config      RollbackConfig
	logger      *zap.Logger
	stopChan    chan struct{}
	rollbackFn  func()
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(config RollbackConfig, logger *zap.Logger) *HealthMonitor {
	return &HealthMonitor{
		config:   config,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

// Start begins health monitoring
func (hm *HealthMonitor) Start(ctx context.Context, rollbackFn func()) {
	hm.rollbackFn = rollbackFn
	ticker := time.NewTicker(hm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-hm.stopChan:
			return
		case <-ticker.C:
			if !hm.checkHealth(ctx) {
				hm.rollbackFn()
				return
			}
		}
	}
}

// Stop stops health monitoring
func (hm *HealthMonitor) Stop() {
	close(hm.stopChan)
}

// checkHealth performs health checks
func (hm *HealthMonitor) checkHealth(ctx context.Context) bool {
	// Implement actual health checks
	// This would check metrics, endpoints, error rates, etc.
	return true
}
