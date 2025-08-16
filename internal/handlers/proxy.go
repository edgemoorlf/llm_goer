package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	
	"azure-openai-proxy/internal/config"
	"azure-openai-proxy/internal/errors"
	"azure-openai-proxy/internal/instance"
	"azure-openai-proxy/internal/services"
	
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ProxyHandler handles OpenAI API proxy requests
type ProxyHandler struct {
	instanceManager *instance.Manager
	transformer     *services.RequestTransformer
	azureServices   map[string]*services.AzureService
}

// NewProxyHandler creates a new proxy handler
func NewProxyHandler(instanceManager *instance.Manager) *ProxyHandler {
	handler := &ProxyHandler{
		instanceManager: instanceManager,
		transformer:     services.NewRequestTransformer(),
		azureServices:   make(map[string]*services.AzureService),
	}
	
	// Initialize Azure services for each instance
	configs := instanceManager.GetAllConfigs()
	for _, cfg := range configs {
		if cfg.ProviderType == "azure" {
			handler.azureServices[cfg.Name] = services.NewAzureService(cfg)
		}
	}
	
	return handler
}

// ChatCompletions handles /v1/chat/completions requests
func (h *ProxyHandler) ChatCompletions(c *gin.Context) {
	h.handleProxyRequest(c, "/v1/chat/completions")
}

// Completions handles /v1/completions requests
func (h *ProxyHandler) Completions(c *gin.Context) {
	h.handleProxyRequest(c, "/v1/completions")
}

// Embeddings handles /v1/embeddings requests
func (h *ProxyHandler) Embeddings(c *gin.Context) {
	h.handleProxyRequest(c, "/v1/embeddings")
}

// handleProxyRequest is the main proxy logic
func (h *ProxyHandler) handleProxyRequest(c *gin.Context, endpoint string) {
	startTime := time.Now()
	
	// Parse request payload
	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		proxyErr := errors.NewClientError("invalid JSON payload", 400, map[string]interface{}{
			"error": err.Error(),
		})
		h.sendErrorResponse(c, proxyErr)
		return
	}
	
	// Validate request
	if err := h.transformer.ValidateRequest(endpoint, payload); err != nil {
		proxyErr := errors.NewClientError(err.Error(), 400, map[string]interface{}{
			"endpoint": endpoint,
		})
		h.sendErrorResponse(c, proxyErr)
		return
	}
	
	// Extract model and determine tokens needed
	modelName, _ := payload["model"].(string)
	
	// Get instance configuration to determine deployment mapping
	selectedInstance, err := h.instanceManager.SelectInstance(c.Request.Context(), modelName, 0, "azure")
	if err != nil {
		proxyErr := errors.NewInstanceError("no suitable instance available", map[string]interface{}{
			"model":    modelName,
			"endpoint": endpoint,
			"error":    err.Error(),
		})
		h.sendErrorResponse(c, proxyErr)
		return
	}
	
	instanceConfig, err := h.instanceManager.GetInstanceConfig(selectedInstance)
	if err != nil {
		proxyErr := errors.NewInternalError("failed to get instance config", map[string]interface{}{
			"instance": selectedInstance,
			"error":    err.Error(),
		})
		h.sendErrorResponse(c, proxyErr)
		return
	}
	
	// Get deployment name for the model
	deploymentName := h.transformer.GetDeploymentName(modelName, instanceConfig.ModelDeployments)
	
	// Transform request
	transformResult, err := h.transformer.TransformOpenAIToAzure(c.Request.Context(), endpoint, payload, deploymentName)
	if err != nil {
		proxyErr := errors.NewInternalError("request transformation failed", map[string]interface{}{
			"error": err.Error(),
			"model": modelName,
		})
		h.sendErrorResponse(c, proxyErr)
		return
	}
	
	// Check rate limit
	hasCapacity, err := h.instanceManager.CheckRateLimit(c.Request.Context(), selectedInstance, transformResult.RequiredTokens)
	if err != nil {
		logrus.WithError(err).Warn("Rate limit check failed")
	}
	if !hasCapacity {
		proxyErr := errors.NewUpstreamError("rate limit exceeded", 429, map[string]interface{}{
			"instance": selectedInstance,
			"tokens":   transformResult.RequiredTokens,
		})
		h.sendErrorResponse(c, proxyErr)
		return
	}
	
	// Check if streaming is requested
	isStreaming := false
	if stream, ok := payload["stream"].(bool); ok && stream {
		isStreaming = true
	}
	
	// Get Azure service for the instance
	azureService, exists := h.azureServices[selectedInstance]
	if !exists {
		proxyErr := errors.NewInternalError("Azure service not found for instance", map[string]interface{}{
			"instance": selectedInstance,
		})
		h.sendErrorResponse(c, proxyErr)
		return
	}
	
	// Clean payload before sending
	cleanPayload := h.transformer.CleanRequestMetadata(transformResult.Payload)
	
	// Send request to Azure
	var resp *http.Response
	if isStreaming {
		resp, err = azureService.StreamRequest(c.Request.Context(), endpoint, cleanPayload, deploymentName)
	} else {
		resp, err = azureService.ProxyRequest(c.Request.Context(), endpoint, cleanPayload, deploymentName)
	}
	
	if err != nil {
		if proxyErr, ok := err.(*errors.ProxyError); ok {
			h.sendErrorResponse(c, proxyErr)
		} else {
			proxyErr := errors.NewUpstreamError("request failed", 500, map[string]interface{}{
				"error":    err.Error(),
				"instance": selectedInstance,
			})
			h.sendErrorResponse(c, proxyErr)
		}
		return
	}
	defer resp.Body.Close()
	
	// Handle error responses
	if resp.StatusCode >= 400 {
		proxyErr := azureService.ParseErrorResponse(resp)
		h.sendErrorResponse(c, proxyErr)
		h.recordError(selectedInstance, resp.StatusCode)
		return
	}
	
	// Record successful usage
	h.recordUsage(selectedInstance, transformResult.RequiredTokens, startTime)
	
	// Stream or return response
	if isStreaming {
		h.streamResponse(c, resp, transformResult.OriginalModel)
	} else {
		h.forwardResponse(c, resp, transformResult.OriginalModel)
	}
}

// forwardResponse forwards a non-streaming response
func (h *ProxyHandler) forwardResponse(c *gin.Context, resp *http.Response, originalModel string) {
	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		proxyErr := errors.NewInternalError("failed to read response", map[string]interface{}{
			"error": err.Error(),
		})
		h.sendErrorResponse(c, proxyErr)
		return
	}
	
	// Parse and transform response
	var responseData map[string]interface{}
	if err := json.Unmarshal(body, &responseData); err != nil {
		// If can't parse as JSON, return as-is
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
		return
	}
	
	// Transform back to OpenAI format
	transformedResponse, err := h.transformer.TransformAzureToOpenAI(c.Request.Context(), responseData, originalModel)
	if err != nil {
		logrus.WithError(err).Warn("Failed to transform response, returning as-is")
		transformedResponse = responseData
	}
	
	// Copy headers
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}
	
	c.JSON(resp.StatusCode, transformedResponse)
}

// streamResponse streams a response back to the client
func (h *ProxyHandler) streamResponse(c *gin.Context, resp *http.Response, originalModel string) {
	// Set streaming headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	
	// Copy other headers
	for key, values := range resp.Header {
		if !strings.HasPrefix(strings.ToLower(key), "content-") && 
		   strings.ToLower(key) != "cache-control" &&
		   strings.ToLower(key) != "connection" {
			for _, value := range values {
				c.Header(key, value)
			}
		}
	}
	
	c.Status(resp.StatusCode)
	
	// Stream the response
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		
		// SSE format: "data: {json}\n\n"
		if strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")
			
			// Handle special cases
			if dataStr == "[DONE]" {
				c.Writer.WriteString("data: [DONE]\n\n")
				c.Writer.Flush()
				break
			}
			
			// Parse and transform the JSON chunk
			var chunkData map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &chunkData); err == nil {
				// Transform model name back to original
				transformedChunk, err := h.transformer.TransformAzureToOpenAI(c.Request.Context(), chunkData, originalModel)
				if err == nil {
					chunkData = transformedChunk
				}
				
				// Write transformed chunk
				if jsonBytes, err := json.Marshal(chunkData); err == nil {
					c.Writer.WriteString(fmt.Sprintf("data: %s\n\n", string(jsonBytes)))
				} else {
					c.Writer.WriteString(line + "\n\n")
				}
			} else {
				// If can't parse, pass through as-is
				c.Writer.WriteString(line + "\n\n")
			}
		} else {
			// Pass through non-data lines
			c.Writer.WriteString(line + "\n")
		}
		
		c.Writer.Flush()
	}
	
	if err := scanner.Err(); err != nil {
		logrus.WithError(err).Error("Error reading stream")
	}
}

// sendErrorResponse sends a standardized error response
func (h *ProxyHandler) sendErrorResponse(c *gin.Context, proxyErr *errors.ProxyError) {
	// Log the error
	logrus.WithFields(logrus.Fields{
		"type":        proxyErr.Type,
		"status_code": proxyErr.StatusCode,
		"details":     proxyErr.Details,
	}).Error(proxyErr.Message)
	
	// Format error response in OpenAI format
	errorResponse := map[string]interface{}{
		"error": map[string]interface{}{
			"message": proxyErr.Message,
			"type":    string(proxyErr.Type),
			"code":    proxyErr.StatusCode,
		},
	}
	
	// Add retry-after header if applicable
	if retryAfter := proxyErr.GetRetryAfter(); retryAfter > 0 {
		c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
	}
	
	c.JSON(proxyErr.StatusCode, errorResponse)
}

// recordUsage records successful token usage
func (h *ProxyHandler) recordUsage(instanceName string, tokens int, startTime time.Time) {
	ctx := context.Background()
	
	// Update usage in rate limiter
	if err := h.instanceManager.UpdateUsage(ctx, instanceName, tokens); err != nil {
		logrus.WithError(err).WithField("instance", instanceName).Warn("Failed to update usage")
	}
	
	// Update instance state
	state, err := h.instanceManager.GetInstanceState(ctx, instanceName)
	if err != nil {
		logrus.WithError(err).WithField("instance", instanceName).Warn("Failed to get instance state")
		return
	}
	
	// Update metrics
	state.TotalRequests++
	state.SuccessfulRequests++
	state.TotalTokensServed += int64(tokens)
	state.LastUsed = time.Now()
	
	// Calculate latency
	latency := float64(time.Since(startTime).Milliseconds())
	if state.AvgLatencyMs == nil {
		state.AvgLatencyMs = &latency
	} else {
		// Exponential moving average
		*state.AvgLatencyMs = 0.9*(*state.AvgLatencyMs) + 0.1*latency
	}
	
	// Save updated state
	if err := h.instanceManager.UpdateInstanceState(ctx, instanceName, state); err != nil {
		logrus.WithError(err).WithField("instance", instanceName).Warn("Failed to update instance state")
	}
}

// recordError records an error occurrence
func (h *ProxyHandler) recordError(instanceName string, statusCode int) {
	ctx := context.Background()
	
	state, err := h.instanceManager.GetInstanceState(ctx, instanceName)
	if err != nil {
		logrus.WithError(err).WithField("instance", instanceName).Warn("Failed to get instance state for error recording")
		return
	}
	
	// Update error counts
	state.ErrorCount++
	state.TotalRequests++
	
	// Update specific error type counts
	switch {
	case statusCode == 500:
		state.TotalErrors500++
	case statusCode == 503:
		state.TotalErrors503++
	default:
		state.TotalOtherErrors++
	}
	
	// Update instance status if too many errors
	errorRate := float64(state.ErrorCount) / float64(state.TotalRequests) * 100
	if errorRate > 50 && state.TotalRequests > 10 {
		state.Status = config.StatusError
		state.HealthStatus = "unhealthy"
	}
	
	// Save updated state
	if err := h.instanceManager.UpdateInstanceState(ctx, instanceName, state); err != nil {
		logrus.WithError(err).WithField("instance", instanceName).Warn("Failed to update instance state after error")
	}
}