package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
	
	"azure-openai-proxy/internal/config"
	"azure-openai-proxy/internal/errors"
)

// AzureService handles communication with Azure OpenAI API
type AzureService struct {
	client *http.Client
	config config.InstanceConfig
}

// NewAzureService creates a new Azure OpenAI service client
func NewAzureService(cfg config.InstanceConfig) *AzureService {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	
	// Configure proxy if specified
	if cfg.ProxyURL != nil && *cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(*cfg.ProxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
	}
	
	return &AzureService{
		client: client,
		config: cfg,
	}
}

// ProxyRequest sends a request to Azure OpenAI and returns the response
func (as *AzureService) ProxyRequest(ctx context.Context, endpoint string, payload map[string]interface{}, deploymentName string) (*http.Response, error) {
	// Build Azure URL
	azureURL := as.buildAzureURL(endpoint, deploymentName)
	
	// Serialize payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.NewInternalError("failed to marshal request payload", map[string]interface{}{
			"error": err.Error(),
		})
	}
	
	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", azureURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, errors.NewInternalError("failed to create request", map[string]interface{}{
			"error": err.Error(),
			"url":   azureURL,
		})
	}
	
	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", as.config.APIKey)
	req.Header.Set("User-Agent", "Azure-OpenAI-Proxy/1.0")
	
	// Add any custom headers
	if as.config.ProviderType == "azure" {
		req.Header.Set("Accept", "application/json")
	}
	
	// Send request
	resp, err := as.client.Do(req)
	if err != nil {
		return nil, errors.NewUpstreamError("request to Azure OpenAI failed", 500, map[string]interface{}{
			"error":      err.Error(),
			"url":        azureURL,
			"deployment": deploymentName,
		})
	}
	
	return resp, nil
}

// StreamRequest sends a streaming request to Azure OpenAI
func (as *AzureService) StreamRequest(ctx context.Context, endpoint string, payload map[string]interface{}, deploymentName string) (*http.Response, error) {
	// Ensure streaming is enabled
	payload["stream"] = true
	
	return as.ProxyRequest(ctx, endpoint, payload, deploymentName)
}

// buildAzureURL constructs the complete Azure OpenAI URL
func (as *AzureService) buildAzureURL(endpoint string, deploymentName string) string {
	baseURL := as.config.APIBase
	
	// Remove trailing slash
	if baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}
	
	// Map OpenAI endpoints to Azure format
	var azureEndpoint string
	switch endpoint {
	case "/v1/chat/completions":
		azureEndpoint = fmt.Sprintf("/openai/deployments/%s/chat/completions", deploymentName)
	case "/v1/completions":
		azureEndpoint = fmt.Sprintf("/openai/deployments/%s/completions", deploymentName)
	case "/v1/embeddings":
		azureEndpoint = fmt.Sprintf("/openai/deployments/%s/embeddings", deploymentName)
	default:
		azureEndpoint = endpoint
	}
	
	// Add API version
	apiVersion := as.config.APIVersion
	if apiVersion == "" {
		apiVersion = "2024-05-01-preview"
	}
	
	return fmt.Sprintf("%s%s?api-version=%s", baseURL, azureEndpoint, apiVersion)
}

// HealthCheck performs a health check against the Azure OpenAI endpoint
func (as *AzureService) HealthCheck(ctx context.Context) error {
	// Use a simple request to check health
	healthURL := as.buildAzureURL("/models", "")
	
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}
	
	req.Header.Set("api-key", as.config.APIKey)
	req.Header.Set("User-Agent", "Azure-OpenAI-Proxy/1.0")
	
	resp, err := as.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 400 {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}
	
	return nil
}

// ParseErrorResponse parses an error response from Azure OpenAI
func (as *AzureService) ParseErrorResponse(resp *http.Response) *errors.ProxyError {
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.NewUpstreamError("failed to read error response", resp.StatusCode, map[string]interface{}{
			"error": err.Error(),
		})
	}
	
	var errorData map[string]interface{}
	if err := json.Unmarshal(body, &errorData); err != nil {
		return errors.NewUpstreamError("invalid error response format", resp.StatusCode, map[string]interface{}{
			"body": string(body),
		})
	}
	
	// Extract error details
	var message string
	var errorType string
	
	if errorObj, ok := errorData["error"].(map[string]interface{}); ok {
		if msg, ok := errorObj["message"].(string); ok {
			message = msg
		}
		if typ, ok := errorObj["type"].(string); ok {
			errorType = typ
		}
	} else {
		message = string(body)
	}
	
	details := map[string]interface{}{
		"response_body": string(body),
		"error_type":    errorType,
		"status_code":   resp.StatusCode,
	}
	
	return errors.NewUpstreamError(message, resp.StatusCode, details)
}

// GetRetryAfter extracts retry-after header from response
func (as *AzureService) GetRetryAfter(resp *http.Response) int {
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		return 0
	}
	
	// Parse retry-after value (can be seconds or HTTP date)
	if seconds := parseRetryAfterSeconds(retryAfter); seconds > 0 {
		return seconds
	}
	
	return 60 // Default fallback
}

// parseRetryAfterSeconds parses Retry-After header value as seconds
func parseRetryAfterSeconds(value string) int {
	// Try to parse as integer (seconds)
	var seconds int
	if _, err := fmt.Sscanf(value, "%d", &seconds); err == nil {
		return seconds
	}
	
	// Try to parse as HTTP date
	if t, err := time.Parse(time.RFC1123, value); err == nil {
		diff := t.Sub(time.Now())
		if diff > 0 {
			return int(diff.Seconds())
		}
	}
	
	return 0
}

// Close closes the HTTP client connections
func (as *AzureService) Close() error {
	// Close idle connections
	if transport, ok := as.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}