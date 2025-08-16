package instance

import (
	"context"
	"fmt"
	"sync"
	"time"
	
	"azure-openai-proxy/internal/config"
	"azure-openai-proxy/internal/storage"
	"azure-openai-proxy/internal/utils"
)

// Manager manages API instances and their states
type Manager struct {
	configs         []config.InstanceConfig
	routingStrategy string
	stateStore      storage.StateStore
	configStore     storage.ConfigStore
	rateLimiters    map[string]*utils.RateLimiter
	mutex           sync.RWMutex
	selector        *InstanceSelector
	redisURL        string
	redisPassword   string
}

// NewManager creates a new instance manager
func NewManager(instances []config.InstanceConfig, strategy string, stateStore storage.StateStore, configStore storage.ConfigStore) (*Manager, error) {
	manager := &Manager{
		configs:         instances,
		routingStrategy: strategy,
		stateStore:      stateStore,
		configStore:     configStore,
		rateLimiters:    make(map[string]*utils.RateLimiter),
		redisURL:        "redis://localhost:6379", // TODO: Get from config
		redisPassword:   "",                       // TODO: Get from config
	}
	
	// Initialize rate limiters for enabled instances
	for _, instance := range instances {
		if instance.Enabled && instance.RateLimitEnabled {
			rateLimiter, err := utils.NewRateLimiter(
				instance.Name,
				instance.MaxTPM,
				instance.MaxInputTokens,
				manager.redisURL,
				manager.redisPassword,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create rate limiter for instance %s: %w", instance.Name, err)
			}
			manager.rateLimiters[instance.Name] = rateLimiter
		}
	}
	
	// Initialize selector
	manager.selector = NewInstanceSelector(manager)
	
	return manager, nil
}

// StartHealthMonitoring starts the health monitoring goroutine
func (m *Manager) StartHealthMonitoring() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		
		for range ticker.C {
			ctx := context.Background()
			m.performHealthChecks(ctx)
		}
	}()
}

// performHealthChecks checks the health of all instances
func (m *Manager) performHealthChecks(ctx context.Context) {
	m.mutex.RLock()
	configs := make([]config.InstanceConfig, len(m.configs))
	copy(configs, m.configs)
	m.mutex.RUnlock()
	
	// Use worker pool for concurrent health checks
	jobs := make(chan config.InstanceConfig, len(configs))
	results := make(chan healthCheckResult, len(configs))
	
	// Start workers
	const numWorkers = 10
	for w := 0; w < numWorkers; w++ {
		go m.healthCheckWorker(ctx, jobs, results)
	}
	
	// Send jobs
	for _, cfg := range configs {
		jobs <- cfg
	}
	close(jobs)
	
	// Collect results
	for i := 0; i < len(configs); i++ {
		result := <-results
		m.updateInstanceHealth(ctx, result)
	}
}

type healthCheckResult struct {
	instanceName string
	isHealthy    bool
	latency      time.Duration
	error        error
}

// healthCheckWorker performs health checks for instances
func (m *Manager) healthCheckWorker(ctx context.Context, jobs <-chan config.InstanceConfig, results chan<- healthCheckResult) {
	for cfg := range jobs {
		result := m.performSingleHealthCheck(ctx, cfg)
		results <- result
	}
}

// performSingleHealthCheck checks the health of a single instance
func (m *Manager) performSingleHealthCheck(ctx context.Context, cfg config.InstanceConfig) healthCheckResult {
	// TODO: Implement actual health check by making a simple API call
	// For now, just return healthy
	return healthCheckResult{
		instanceName: cfg.Name,
		isHealthy:    true,
		latency:      50 * time.Millisecond,
		error:        nil,
	}
}

// updateInstanceHealth updates the health status of an instance
func (m *Manager) updateInstanceHealth(ctx context.Context, result healthCheckResult) {
	state, err := m.GetInstanceState(ctx, result.instanceName)
	if err != nil {
		return
	}
	
	if result.isHealthy {
		state.Status = config.StatusHealthy
		state.HealthStatus = "healthy"
		state.ConnectionStatus = "connected"
		state.AvgLatencyMs = &[]float64{float64(result.latency.Milliseconds())}[0]
	} else {
		state.Status = config.StatusError
		state.HealthStatus = "unhealthy"
		state.ConnectionStatus = "disconnected"
		if result.error != nil {
			errorMsg := result.error.Error()
			state.LastError = &errorMsg
			now := time.Now().Unix()
			state.LastErrorTime = &now
		}
	}
	
	// Save updated state
	m.stateStore.Set(ctx, result.instanceName, state)
}

// GetAllConfigs returns all instance configurations
func (m *Manager) GetAllConfigs() []config.InstanceConfig {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	configs := make([]config.InstanceConfig, len(m.configs))
	copy(configs, m.configs)
	return configs
}

// GetInstanceState retrieves the current state of an instance
func (m *Manager) GetInstanceState(ctx context.Context, instanceName string) (*config.InstanceState, error) {
	return m.stateStore.Get(ctx, instanceName)
}

// UpdateInstanceState updates the state of an instance
func (m *Manager) UpdateInstanceState(ctx context.Context, instanceName string, state *config.InstanceState) error {
	return m.stateStore.Set(ctx, instanceName, state)
}

// CheckRateLimit checks if an instance has capacity for the given tokens
func (m *Manager) CheckRateLimit(ctx context.Context, instanceName string, tokens int) (bool, error) {
	m.mutex.RLock()
	rateLimiter, exists := m.rateLimiters[instanceName]
	m.mutex.RUnlock()
	
	if !exists {
		// No rate limiting configured
		return true, nil
	}
	
	hasCapacity, _, err := rateLimiter.CheckCapacity(ctx, tokens)
	return hasCapacity, err
}

// UpdateUsage records token usage for an instance
func (m *Manager) UpdateUsage(ctx context.Context, instanceName string, tokens int) error {
	m.mutex.RLock()
	rateLimiter, exists := m.rateLimiters[instanceName]
	m.mutex.RUnlock()
	
	if !exists {
		// No rate limiting configured
		return nil
	}
	
	return rateLimiter.UpdateUsage(ctx, tokens)
}

// SelectInstance selects the best instance for a request
func (m *Manager) SelectInstance(ctx context.Context, model string, tokens int, providerType string) (string, error) {
	return m.selector.SelectInstanceForRequest(ctx, model, tokens, providerType)
}

// GetInstanceConfig returns the configuration for a specific instance
func (m *Manager) GetInstanceConfig(instanceName string) (*config.InstanceConfig, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	for _, cfg := range m.configs {
		if cfg.Name == instanceName {
			return &cfg, nil
		}
	}
	
	return nil, fmt.Errorf("instance not found: %s", instanceName)
}

// ResetInstance resets the state and rate limiting for an instance
func (m *Manager) ResetInstance(ctx context.Context, instanceName string) error {
	// Reset state
	if err := m.stateStore.Delete(ctx, instanceName); err != nil {
		return fmt.Errorf("failed to reset instance state: %w", err)
	}
	
	// Reset rate limiter
	m.mutex.RLock()
	rateLimiter, exists := m.rateLimiters[instanceName]
	m.mutex.RUnlock()
	
	if exists {
		if err := rateLimiter.Reset(ctx); err != nil {
			return fmt.Errorf("failed to reset rate limiter: %w", err)
		}
	}
	
	return nil
}

// GetStats returns comprehensive statistics for all instances
func (m *Manager) GetStats(ctx context.Context) (map[string]interface{}, error) {
	states, err := m.stateStore.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance states: %w", err)
	}
	
	stats := map[string]interface{}{
		"total_instances":   len(m.configs),
		"healthy_instances": 0,
		"total_requests":    0,
		"total_tokens":      int64(0),
		"instances":         make(map[string]interface{}),
	}
	
	for _, state := range states {
		if state.IsHealthy() {
			stats["healthy_instances"] = stats["healthy_instances"].(int) + 1
		}
		stats["total_requests"] = stats["total_requests"].(int) + state.TotalRequests
		stats["total_tokens"] = stats["total_tokens"].(int64) + state.TotalTokensServed
		
		instanceStats := map[string]interface{}{
			"status":               state.Status,
			"health_status":        state.HealthStatus,
			"total_requests":       state.TotalRequests,
			"successful_requests":  state.SuccessfulRequests,
			"total_tokens_served":  state.TotalTokensServed,
			"current_tpm":          state.CurrentTPM,
			"current_rpm":          state.CurrentRPM,
			"error_count":          state.ErrorCount,
			"utilization_percent":  state.UtilizationPercentage,
			"last_used":            state.LastUsed,
		}
		
		stats["instances"].(map[string]interface{})[state.Name] = instanceStats
	}
	
	return stats, nil
}

// Close closes all connections and cleanup resources
func (m *Manager) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	var errors []error
	
	// Close rate limiters
	for name, rateLimiter := range m.rateLimiters {
		if err := rateLimiter.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close rate limiter for %s: %w", name, err))
		}
	}
	
	// Close storage connections
	if err := m.stateStore.Close(); err != nil {
		errors = append(errors, fmt.Errorf("failed to close state store: %w", err))
	}
	
	if err := m.configStore.Close(); err != nil {
		errors = append(errors, fmt.Errorf("failed to close config store: %w", err))
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("multiple errors during close: %v", errors)
	}
	
	return nil
}