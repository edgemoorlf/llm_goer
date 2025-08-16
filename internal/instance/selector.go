package instance

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
	
	"azure-openai-proxy/internal/config"
)

// InstanceSelector implements different instance selection algorithms
type InstanceSelector struct {
	manager *Manager
}

// NewInstanceSelector creates a new instance selector
func NewInstanceSelector(manager *Manager) *InstanceSelector {
	return &InstanceSelector{
		manager: manager,
	}
}

// instanceWithState combines configuration and runtime state
type instanceWithState struct {
	Config config.InstanceConfig
	State  config.InstanceState
}

// SelectInstanceForRequest selects the best instance for a given request
func (is *InstanceSelector) SelectInstanceForRequest(ctx context.Context, model string, tokens int, providerType string) (string, error) {
	// Get all instances (configs only first)
	configs := is.manager.GetAllConfigs()
	
	// Pre-filter by provider type, model support, and enabled status
	filteredConfigs := make([]config.InstanceConfig, 0)
	for _, cfg := range configs {
		// Skip disabled instances
		if !cfg.Enabled {
			continue
		}
		
		// Filter by provider type if specified
		if providerType != "" && cfg.ProviderType != providerType {
			continue
		}
		
		// Check model support (exact matching)
		modelSupported := false
		modelLower := strings.ToLower(model)
		for _, supportedModel := range cfg.SupportedModels {
			if strings.ToLower(supportedModel) == modelLower {
				modelSupported = true
				break
			}
		}
		if !modelSupported {
			continue
		}
		
		// Check token capacity (basic check against max_tpm)
		if tokens > cfg.MaxTPM {
			continue
		}
		
		filteredConfigs = append(filteredConfigs, cfg)
	}
	
	if len(filteredConfigs) == 0 {
		return "", fmt.Errorf("no suitable instances found for model %s", model)
	}
	
	// Get states for filtered instances
	eligibleInstances := make([]instanceWithState, 0)
	for _, cfg := range filteredConfigs {
		state, err := is.manager.GetInstanceState(ctx, cfg.Name)
		if err != nil {
			continue // Skip instances with state errors
		}
		
		// Skip unhealthy instances
		if !state.IsHealthy() {
			continue
		}
		
		// Check rate limit capacity
		hasCapacity, err := is.manager.CheckRateLimit(ctx, cfg.Name, tokens)
		if err != nil || !hasCapacity {
			continue
		}
		
		eligibleInstances = append(eligibleInstances, instanceWithState{
			Config: cfg,
			State:  *state,
		})
	}
	
	if len(eligibleInstances) == 0 {
		return "", fmt.Errorf("no healthy instances with capacity found for model %s", model)
	}
	
	// Apply routing strategy
	strategy := is.manager.routingStrategy
	switch strategy {
	case "failover":
		return is.selectByFailover(eligibleInstances), nil
	case "weighted":
		return is.selectByWeight(eligibleInstances), nil
	case "round_robin":
		return is.selectByRoundRobin(eligibleInstances), nil
	default:
		return is.selectByFailover(eligibleInstances), nil
	}
}

// selectByFailover selects the instance with the highest priority (lowest number)
func (is *InstanceSelector) selectByFailover(instances []instanceWithState) string {
	// Sort by priority (lower number = higher priority)
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].Config.Priority < instances[j].Config.Priority
	})
	
	return instances[0].Config.Name
}

// selectByWeight selects an instance based on weighted random selection
func (is *InstanceSelector) selectByWeight(instances []instanceWithState) string {
	// Calculate total weight
	totalWeight := 0
	for _, instance := range instances {
		totalWeight += instance.Config.Weight
	}
	
	// Generate random number
	rand.Seed(time.Now().UnixNano())
	target := rand.Intn(totalWeight)
	
	// Select instance based on weight
	current := 0
	for _, instance := range instances {
		current += instance.Config.Weight
		if current > target {
			return instance.Config.Name
		}
	}
	
	// Fallback to first instance
	return instances[0].Config.Name
}

// selectByRoundRobin selects the least recently used instance
func (is *InstanceSelector) selectByRoundRobin(instances []instanceWithState) string {
	// Sort by last_used timestamp (least recently used first)
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].State.LastUsed.Before(instances[j].State.LastUsed)
	})
	
	return instances[0].Config.Name
}

// selectByLowestUtilization selects the instance with the lowest utilization
func (is *InstanceSelector) selectByLowestUtilization(instances []instanceWithState) string {
	// Sort by utilization percentage (lowest first)
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].State.UtilizationPercentage < instances[j].State.UtilizationPercentage
	})
	
	return instances[0].Config.Name
}

// selectByLowestLatency selects the instance with the lowest average latency
func (is *InstanceSelector) selectByLowestLatency(instances []instanceWithState) string {
	// Sort by average latency (lowest first)
	sort.Slice(instances, func(i, j int) bool {
		latencyI := float64(100) // Default latency if not available
		if instances[i].State.AvgLatencyMs != nil {
			latencyI = *instances[i].State.AvgLatencyMs
		}
		
		latencyJ := float64(100)
		if instances[j].State.AvgLatencyMs != nil {
			latencyJ = *instances[j].State.AvgLatencyMs
		}
		
		return latencyI < latencyJ
	})
	
	return instances[0].Config.Name
}

// selectByComposite selects an instance using a composite scoring algorithm
func (is *InstanceSelector) selectByComposite(instances []instanceWithState) string {
	// Calculate composite scores for each instance
	type instanceScore struct {
		instance instanceWithState
		score    float64
	}
	
	scores := make([]instanceScore, len(instances))
	
	for i, instance := range instances {
		score := is.calculateCompositeScore(instance)
		scores[i] = instanceScore{
			instance: instance,
			score:    score,
		}
	}
	
	// Sort by score (higher is better)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})
	
	return scores[0].instance.Config.Name
}

// calculateCompositeScore calculates a composite score for an instance
func (is *InstanceSelector) calculateCompositeScore(instance instanceWithState) float64 {
	score := 0.0
	
	// Weight factor (higher weight = better)
	weightScore := float64(instance.Config.Weight) / 20.0 // Normalize to 0-1 range
	score += weightScore * 0.3
	
	// Utilization factor (lower utilization = better)
	utilizationScore := (100.0 - instance.State.UtilizationPercentage) / 100.0
	score += utilizationScore * 0.4
	
	// Error rate factor (lower error rate = better)
	errorRate := instance.State.CurrentErrorRate
	errorScore := (100.0 - errorRate) / 100.0
	score += errorScore * 0.2
	
	// Latency factor (lower latency = better)
	latencyScore := 1.0
	if instance.State.AvgLatencyMs != nil {
		// Normalize latency: 0ms = 1.0, 1000ms = 0.0
		latencyScore = 1.0 - (*instance.State.AvgLatencyMs / 1000.0)
		if latencyScore < 0 {
			latencyScore = 0
		}
	}
	score += latencyScore * 0.1
	
	return score
}

// GetEligibleInstances returns all instances eligible for a request
func (is *InstanceSelector) GetEligibleInstances(ctx context.Context, model string, tokens int, providerType string) ([]instanceWithState, error) {
	configs := is.manager.GetAllConfigs()
	
	// Pre-filter instances
	filteredConfigs := make([]config.InstanceConfig, 0)
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		
		if providerType != "" && cfg.ProviderType != providerType {
			continue
		}
		
		// Check model support
		modelSupported := false
		modelLower := strings.ToLower(model)
		for _, supportedModel := range cfg.SupportedModels {
			if strings.ToLower(supportedModel) == modelLower {
				modelSupported = true
				break
			}
		}
		if !modelSupported {
			continue
		}
		
		if tokens > cfg.MaxTPM {
			continue
		}
		
		filteredConfigs = append(filteredConfigs, cfg)
	}
	
	// Get states and check health/capacity
	eligibleInstances := make([]instanceWithState, 0)
	for _, cfg := range filteredConfigs {
		state, err := is.manager.GetInstanceState(ctx, cfg.Name)
		if err != nil {
			continue
		}
		
		if !state.IsHealthy() {
			continue
		}
		
		hasCapacity, err := is.manager.CheckRateLimit(ctx, cfg.Name, tokens)
		if err != nil || !hasCapacity {
			continue
		}
		
		eligibleInstances = append(eligibleInstances, instanceWithState{
			Config: cfg,
			State:  *state,
		})
	}
	
	return eligibleInstances, nil
}