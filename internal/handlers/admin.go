package handlers

import (
	"net/http"
	"azure-openai-proxy/internal/instance"
	
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// AdminHandler handles administrative requests
type AdminHandler struct {
	instanceManager *instance.Manager
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(instanceManager *instance.Manager) *AdminHandler {
	return &AdminHandler{
		instanceManager: instanceManager,
	}
}

// GetInstances returns all instances with their current states
func (h *AdminHandler) GetInstances(c *gin.Context) {
	ctx := c.Request.Context()
	
	// Get all instance states
	states, err := h.instanceManager.GetStats(ctx)
	if err != nil {
		logrus.WithError(err).Error("Failed to get instance stats")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve instance information",
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"instances": states["instances"],
		"summary": gin.H{
			"total_instances":   states["total_instances"],
			"healthy_instances": states["healthy_instances"],
			"total_requests":    states["total_requests"],
			"total_tokens":      states["total_tokens"],
		},
	})
}

// GetInstance returns details for a specific instance
func (h *AdminHandler) GetInstance(c *gin.Context) {
	instanceName := c.Param("name")
	ctx := c.Request.Context()
	
	// Get instance configuration
	config, err := h.instanceManager.GetInstanceConfig(instanceName)
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
			"error": "Failed to retrieve instance state",
		})
		return
	}
	
	response := gin.H{
		"name":   instanceName,
		"config": config,
		"state":  state,
		"health": gin.H{
			"status":             state.Status,
			"health_status":      state.HealthStatus,
			"connection_status":  state.ConnectionStatus,
			"last_error":         state.LastError,
			"last_error_time":    state.LastErrorTime,
			"avg_latency_ms":     state.AvgLatencyMs,
		},
		"usage": gin.H{
			"current_tpm":          state.CurrentTPM,
			"current_rpm":          state.CurrentRPM,
			"total_requests":       state.TotalRequests,
			"successful_requests":  state.SuccessfulRequests,
			"total_tokens_served":  state.TotalTokensServed,
			"utilization_percent":  state.UtilizationPercentage,
			"last_used":            state.LastUsed,
		},
		"errors": gin.H{
			"total_errors":     state.ErrorCount,
			"errors_500":       state.TotalErrors500,
			"errors_503":       state.TotalErrors503,
			"other_errors":     state.TotalOtherErrors,
			"current_error_rate": state.CurrentErrorRate,
		},
	}
	
	c.JSON(http.StatusOK, response)
}

// ResetInstance resets an instance's state and rate limiting
func (h *AdminHandler) ResetInstance(c *gin.Context) {
	instanceName := c.Param("name")
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
	
	// Reset the instance
	err = h.instanceManager.ResetInstance(ctx, instanceName)
	if err != nil {
		logrus.WithError(err).WithField("instance", instanceName).Error("Failed to reset instance")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to reset instance",
			"details": err.Error(),
		})
		return
	}
	
	logrus.WithField("instance", instanceName).Info("Instance reset successfully")
	
	c.JSON(http.StatusOK, gin.H{
		"message": "Instance reset successfully",
		"instance": instanceName,
		"timestamp": c.Request.Header.Get("X-Request-Timestamp"),
	})
}

// GetConfig returns the current proxy configuration
func (h *AdminHandler) GetConfig(c *gin.Context) {
	configs := h.instanceManager.GetAllConfigs()
	
	// Remove sensitive information (API keys)
	sanitizedConfigs := make([]map[string]interface{}, len(configs))
	for i, cfg := range configs {
		sanitized := map[string]interface{}{
			"name":              cfg.Name,
			"provider_type":     cfg.ProviderType,
			"api_base":          cfg.APIBase,
			"api_version":       cfg.APIVersion,
			"priority":          cfg.Priority,
			"weight":            cfg.Weight,
			"max_tpm":           cfg.MaxTPM,
			"max_input_tokens":  cfg.MaxInputTokens,
			"supported_models":  cfg.SupportedModels,
			"model_deployments": cfg.ModelDeployments,
			"enabled":           cfg.Enabled,
			"timeout_seconds":   cfg.TimeoutSeconds,
			"retry_count":       cfg.RetryCount,
			"rate_limit_enabled": cfg.RateLimitEnabled,
			"api_key_configured": cfg.APIKey != "",
			"proxy_url":         cfg.ProxyURL,
		}
		sanitizedConfigs[i] = sanitized
	}
	
	c.JSON(http.StatusOK, gin.H{
		"instances": sanitizedConfigs,
		"total_instances": len(configs),
	})
}

// UpdateInstanceConfig updates configuration for a specific instance
func (h *AdminHandler) UpdateInstanceConfig(c *gin.Context) {
	instanceName := c.Param("name")
	
	// Parse the update request
	var updateData map[string]interface{}
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid JSON payload",
			"details": err.Error(),
		})
		return
	}
	
	// Get current configuration
	currentConfig, err := h.instanceManager.GetInstanceConfig(instanceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Instance not found",
			"instance": instanceName,
		})
		return
	}
	
	// Apply allowed updates (only certain fields can be updated at runtime)
	allowedUpdates := map[string]bool{
		"enabled":           true,
		"weight":            true,
		"priority":          true,
		"max_tpm":           true,
		"max_input_tokens":  true,
		"timeout_seconds":   true,
		"retry_count":       true,
		"rate_limit_enabled": true,
	}
	
	updated := false
	for key, value := range updateData {
		if !allowedUpdates[key] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Field cannot be updated at runtime",
				"field": key,
				"allowed_fields": []string{"enabled", "weight", "priority", "max_tpm", "max_input_tokens", "timeout_seconds", "retry_count", "rate_limit_enabled"},
			})
			return
		}
		
		// Apply the update based on field type
		switch key {
		case "enabled":
			if val, ok := value.(bool); ok {
				currentConfig.Enabled = val
				updated = true
			}
		case "weight":
			if val, ok := value.(float64); ok {
				currentConfig.Weight = int(val)
				updated = true
			}
		case "priority":
			if val, ok := value.(float64); ok {
				currentConfig.Priority = int(val)
				updated = true
			}
		case "max_tpm":
			if val, ok := value.(float64); ok {
				currentConfig.MaxTPM = int(val)
				updated = true
			}
		case "max_input_tokens":
			if val, ok := value.(float64); ok {
				currentConfig.MaxInputTokens = int(val)
				updated = true
			}
		case "timeout_seconds":
			if val, ok := value.(float64); ok {
				currentConfig.TimeoutSeconds = val
				updated = true
			}
		case "retry_count":
			if val, ok := value.(float64); ok {
				currentConfig.RetryCount = int(val)
				updated = true
			}
		case "rate_limit_enabled":
			if val, ok := value.(bool); ok {
				currentConfig.RateLimitEnabled = val
				updated = true
			}
		}
	}
	
	if !updated {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "No valid updates provided",
		})
		return
	}
	
	logrus.WithFields(logrus.Fields{
		"instance": instanceName,
		"updates":  updateData,
	}).Info("Instance configuration updated")
	
	c.JSON(http.StatusOK, gin.H{
		"message": "Instance configuration updated successfully",
		"instance": instanceName,
		"updated_fields": updateData,
	})
}

// GetHealth returns overall proxy health status
func (h *AdminHandler) GetHealth(c *gin.Context) {
	ctx := c.Request.Context()
	
	stats, err := h.instanceManager.GetStats(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error": "Failed to retrieve health information",
		})
		return
	}
	
	totalInstances := stats["total_instances"].(int)
	healthyInstances := stats["healthy_instances"].(int)
	
	status := "healthy"
	if healthyInstances == 0 {
		status = "unhealthy"
	} else if healthyInstances < totalInstances/2 {
		status = "degraded"
	}
	
	response := gin.H{
		"status": status,
		"instances": gin.H{
			"total":   totalInstances,
			"healthy": healthyInstances,
			"unhealthy": totalInstances - healthyInstances,
		},
		"uptime": gin.H{
			"status": "running",
		},
	}
	
	statusCode := http.StatusOK
	if status == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}
	
	c.JSON(statusCode, response)
}