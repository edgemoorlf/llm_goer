package handlers

import (
	"net/http"
	"strconv"
	"time"
	
	"azure-openai-proxy/internal/config"
	"azure-openai-proxy/internal/instance"
	
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// StatsHandler handles statistics requests
type StatsHandler struct {
	instanceManager *instance.Manager
}

// NewStatsHandler creates a new stats handler
func NewStatsHandler(instanceManager *instance.Manager) *StatsHandler {
	return &StatsHandler{
		instanceManager: instanceManager,
	}
}

// GetOverallStats returns overall proxy statistics
func (h *StatsHandler) GetOverallStats(c *gin.Context) {
	ctx := c.Request.Context()
	
	stats, err := h.instanceManager.GetStats(ctx)
	if err != nil {
		logrus.WithError(err).Error("Failed to get overall stats")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve statistics",
		})
		return
	}
	
	// Calculate additional metrics
	totalRequests := stats["total_requests"].(int)
	totalTokens := stats["total_tokens"].(int64)
	healthyInstances := stats["healthy_instances"].(int)
	totalInstances := stats["total_instances"].(int)
	
	healthPercentage := 0.0
	if totalInstances > 0 {
		healthPercentage = float64(healthyInstances) / float64(totalInstances) * 100
	}
	
	avgTokensPerRequest := 0.0
	if totalRequests > 0 {
		avgTokensPerRequest = float64(totalTokens) / float64(totalRequests)
	}
	
	response := gin.H{
		"summary": gin.H{
			"total_instances":       totalInstances,
			"healthy_instances":     healthyInstances,
			"unhealthy_instances":   totalInstances - healthyInstances,
			"health_percentage":     healthPercentage,
			"total_requests":        totalRequests,
			"total_tokens_served":   totalTokens,
			"avg_tokens_per_request": avgTokensPerRequest,
		},
		"instances": stats["instances"],
		"timestamp": time.Now().Unix(),
	}
	
	c.JSON(http.StatusOK, response)
}

// GetInstanceStats returns per-instance statistics
func (h *StatsHandler) GetInstanceStats(c *gin.Context) {
	// Get query parameters
	instanceName := c.Query("instance")
	windowParam := c.DefaultQuery("window", "60") // Default 1 hour
	
	windowMinutes, err := strconv.Atoi(windowParam)
	if err != nil || windowMinutes <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid window parameter, must be positive integer (minutes)",
		})
		return
	}
	
	if instanceName != "" {
		// Get stats for specific instance
		h.getSingleInstanceStats(c, instanceName, windowMinutes)
	} else {
		// Get stats for all instances
		h.getAllInstanceStats(c, windowMinutes)
	}
}

// getSingleInstanceStats returns statistics for a single instance
func (h *StatsHandler) getSingleInstanceStats(c *gin.Context, instanceName string, windowMinutes int) {
	ctx := c.Request.Context()
	
	// Check if instance exists
	_, err := h.instanceManager.GetInstanceConfig(instanceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Instance not found",
			"instance": instanceName,
		})
		return
	}
	
	// Get instance state
	state, err := h.instanceManager.GetInstanceState(ctx, instanceName)
	if err != nil {
		logrus.WithError(err).WithField("instance", instanceName).Error("Failed to get instance state")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve instance statistics",
		})
		return
	}
	
	// Calculate time-windowed statistics
	now := time.Now()
	windowStart := now.Add(-time.Duration(windowMinutes) * time.Minute)
	
	response := gin.H{
		"instance": instanceName,
		"window_minutes": windowMinutes,
		"window_start": windowStart.Unix(),
		"window_end": now.Unix(),
		"current_status": gin.H{
			"status":               state.Status,
			"health_status":        state.HealthStatus,
			"connection_status":    state.ConnectionStatus,
			"last_used":            state.LastUsed,
			"avg_latency_ms":       state.AvgLatencyMs,
			"utilization_percent":  state.UtilizationPercentage,
		},
		"usage": gin.H{
			"current_tpm":         state.CurrentTPM,
			"current_rpm":         state.CurrentRPM,
			"total_requests":      state.TotalRequests,
			"successful_requests": state.SuccessfulRequests,
			"total_tokens_served": state.TotalTokensServed,
		},
		"errors": gin.H{
			"total_errors":         state.ErrorCount,
			"error_rate_percent":   state.CurrentErrorRate,
			"errors_500":           state.TotalErrors500,
			"errors_503":           state.TotalErrors503,
			"other_errors":         state.TotalOtherErrors,
			"client_errors_500":    state.TotalClientErrors500,
			"client_errors_503":    state.TotalClientErrors503,
			"upstream_errors_429":  state.TotalUpstream429Errors,
			"upstream_errors_400":  state.TotalUpstream400Errors,
			"upstream_errors_500":  state.TotalUpstream500Errors,
		},
		"rate_limiting": gin.H{
			"rate_limited_until": state.RateLimitedUntil,
		},
	}
	
	c.JSON(http.StatusOK, response)
}

// getAllInstanceStats returns statistics for all instances
func (h *StatsHandler) getAllInstanceStats(c *gin.Context, windowMinutes int) {
	ctx := c.Request.Context()
	
	stats, err := h.instanceManager.GetStats(ctx)
	if err != nil {
		logrus.WithError(err).Error("Failed to get all instance stats")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve instance statistics",
		})
		return
	}
	
	now := time.Now()
	windowStart := now.Add(-time.Duration(windowMinutes) * time.Minute)
	
	response := gin.H{
		"window_minutes": windowMinutes,
		"window_start": windowStart.Unix(),
		"window_end": now.Unix(),
		"summary": gin.H{
			"total_instances":   stats["total_instances"],
			"healthy_instances": stats["healthy_instances"],
			"total_requests":    stats["total_requests"],
			"total_tokens":      stats["total_tokens"],
		},
		"instances": stats["instances"],
	}
	
	c.JSON(http.StatusOK, response)
}

// GetUsageStats returns usage statistics with time-series data
func (h *StatsHandler) GetUsageStats(c *gin.Context) {
	// Get query parameters
	instanceName := c.Query("instance")
	metricType := c.DefaultQuery("metric", "tokens") // tokens, requests, errors
	windowParam := c.DefaultQuery("window", "60")    // minutes
	granularityParam := c.DefaultQuery("granularity", "5") // minutes
	
	windowMinutes, err := strconv.Atoi(windowParam)
	if err != nil || windowMinutes <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid window parameter",
		})
		return
	}
	
	granularityMinutes, err := strconv.Atoi(granularityParam)
	if err != nil || granularityMinutes <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid granularity parameter",
		})
		return
	}
	
	validMetrics := map[string]bool{
		"tokens":   true,
		"requests": true,
		"errors":   true,
		"latency":  true,
	}
	
	if !validMetrics[metricType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid metric type",
			"valid_metrics": []string{"tokens", "requests", "errors", "latency"},
		})
		return
	}
	
	if instanceName != "" {
		// Get usage stats for specific instance
		h.getSingleInstanceUsageStats(c, instanceName, metricType, windowMinutes, granularityMinutes)
	} else {
		// Get aggregated usage stats for all instances
		h.getAggregatedUsageStats(c, metricType, windowMinutes, granularityMinutes)
	}
}

// getSingleInstanceUsageStats returns usage statistics for a single instance
func (h *StatsHandler) getSingleInstanceUsageStats(c *gin.Context, instanceName string, metricType string, windowMinutes, granularityMinutes int) {
	ctx := c.Request.Context()
	
	// Check if instance exists
	_, err := h.instanceManager.GetInstanceConfig(instanceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Instance not found",
			"instance": instanceName,
		})
		return
	}
	
	// Get instance state for current metrics
	state, err := h.instanceManager.GetInstanceState(ctx, instanceName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve usage statistics",
		})
		return
	}
	
	now := time.Now()
	windowStart := now.Add(-time.Duration(windowMinutes) * time.Minute)
	
	// Generate time series data (mock implementation)
	timeSeries := h.generateTimeSeriesData(metricType, windowStart, now, granularityMinutes, state)
	
	response := gin.H{
		"instance": instanceName,
		"metric": metricType,
		"window_minutes": windowMinutes,
		"granularity_minutes": granularityMinutes,
		"window_start": windowStart.Unix(),
		"window_end": now.Unix(),
		"current_value": h.getCurrentMetricValue(metricType, state),
		"time_series": timeSeries,
	}
	
	c.JSON(http.StatusOK, response)
}

// getAggregatedUsageStats returns aggregated usage statistics for all instances
func (h *StatsHandler) getAggregatedUsageStats(c *gin.Context, metricType string, windowMinutes, granularityMinutes int) {
	ctx := c.Request.Context()
	
	stats, err := h.instanceManager.GetStats(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve usage statistics",
		})
		return
	}
	
	now := time.Now()
	windowStart := now.Add(-time.Duration(windowMinutes) * time.Minute)
	
	// Calculate aggregated current value
	aggregatedValue := h.getAggregatedCurrentValue(metricType, stats)
	
	// Generate aggregated time series (simplified)
	timeSeries := h.generateAggregatedTimeSeriesData(metricType, windowStart, now, granularityMinutes, stats)
	
	response := gin.H{
		"metric": metricType,
		"window_minutes": windowMinutes,
		"granularity_minutes": granularityMinutes,
		"window_start": windowStart.Unix(),
		"window_end": now.Unix(),
		"instances_count": stats["total_instances"],
		"current_value": aggregatedValue,
		"time_series": timeSeries,
	}
	
	c.JSON(http.StatusOK, response)
}

// Helper functions for metrics calculation

func (h *StatsHandler) getCurrentMetricValue(metricType string, state *config.InstanceState) interface{} {
	switch metricType {
	case "tokens":
		return state.CurrentTPM
	case "requests":
		return state.CurrentRPM
	case "errors":
		return state.CurrentErrorRate
	case "latency":
		if state.AvgLatencyMs != nil {
			return *state.AvgLatencyMs
		}
		return 0.0
	default:
		return 0
	}
}

func (h *StatsHandler) getAggregatedCurrentValue(metricType string, stats map[string]interface{}) interface{} {
	switch metricType {
	case "tokens":
		return stats["total_tokens"]
	case "requests":
		return stats["total_requests"]
	case "errors":
		// Calculate average error rate across instances
		return 0.0 // Simplified
	case "latency":
		// Calculate average latency across instances
		return 0.0 // Simplified
	default:
		return 0
	}
}

func (h *StatsHandler) generateTimeSeriesData(metricType string, start, end time.Time, granularityMinutes int, state *config.InstanceState) []map[string]interface{} {
	var timeSeries []map[string]interface{}
	
	// Generate mock time series data
	current := start
	for current.Before(end) {
		value := h.getCurrentMetricValue(metricType, state)
		
		// Add some variation for demonstration
		if floatVal, ok := value.(float64); ok {
			value = floatVal * (0.8 + 0.4*float64(current.Unix()%100)/100.0)
		} else if intVal, ok := value.(int); ok {
			variation := int(float64(intVal) * (0.8 + 0.4*float64(current.Unix()%100)/100.0))
			value = variation
		}
		
		timeSeries = append(timeSeries, map[string]interface{}{
			"timestamp": current.Unix(),
			"value": value,
		})
		
		current = current.Add(time.Duration(granularityMinutes) * time.Minute)
	}
	
	return timeSeries
}

func (h *StatsHandler) generateAggregatedTimeSeriesData(metricType string, start, end time.Time, granularityMinutes int, stats map[string]interface{}) []map[string]interface{} {
	var timeSeries []map[string]interface{}
	
	baseValue := h.getAggregatedCurrentValue(metricType, stats)
	
	current := start
	for current.Before(end) {
		value := baseValue
		
		// Add variation for demonstration
		if floatVal, ok := value.(float64); ok {
			value = floatVal * (0.8 + 0.4*float64(current.Unix()%100)/100.0)
		} else if intVal, ok := value.(int); ok {
			variation := int(float64(intVal) * (0.8 + 0.4*float64(current.Unix()%100)/100.0))
			value = variation
		}
		
		timeSeries = append(timeSeries, map[string]interface{}{
			"timestamp": current.Unix(),
			"value": value,
		})
		
		current = current.Add(time.Duration(granularityMinutes) * time.Minute)
	}
	
	return timeSeries
}