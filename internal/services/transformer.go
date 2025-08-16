package services

import (
	"context"
	"fmt"
	"strings"
	
	"azure-openai-proxy/internal/utils"
)

// RequestTransformer handles request transformation between OpenAI and Azure formats
type RequestTransformer struct {
	tokenEstimator *utils.TokenEstimator
}

// NewRequestTransformer creates a new request transformer
func NewRequestTransformer() *RequestTransformer {
	return &RequestTransformer{
		tokenEstimator: utils.NewTokenEstimator(),
	}
}

// TransformResult contains the result of request transformation
type TransformResult struct {
	OriginalModel  string                 `json:"original_model"`
	Payload        map[string]interface{} `json:"payload"`
	RequiredTokens int                    `json:"required_tokens"`
	Endpoint       string                 `json:"endpoint"`
	Method         string                 `json:"method"`
}

// TransformOpenAIToAzure transforms an OpenAI request to Azure OpenAI format
func (rt *RequestTransformer) TransformOpenAIToAzure(ctx context.Context, endpoint string, payload map[string]interface{}, deploymentName string) (*TransformResult, error) {
	// Extract model name
	modelName, ok := payload["model"].(string)
	if !ok || modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	
	// Clone payload and remove model field (Azure uses deployment names in URL)
	azurePayload := make(map[string]interface{})
	for k, v := range payload {
		if k != "model" {
			azurePayload[k] = v
		}
	}
	
	// Estimate tokens
	requiredTokens, err := rt.estimateTokens(endpoint, azurePayload, modelName)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate tokens: %w", err)
	}
	
	// Apply max_tokens optimization for Azure S0 tier
	if maxTokens, ok := azurePayload["max_tokens"]; ok {
		if maxTokensFloat, ok := maxTokens.(float64); ok {
			maxTokensInt := int(maxTokensFloat)
			if maxTokensInt > 5000 && 
			   maxTokensInt > requiredTokens+5000 && 
			   !strings.Contains(modelName, "2024-05-13") {
				azurePayload["max_tokens"] = requiredTokens + 5000
			}
		}
	}
	
	return &TransformResult{
		OriginalModel:  strings.ToLower(modelName),
		Payload:        azurePayload,
		RequiredTokens: requiredTokens,
		Endpoint:       endpoint,
		Method:         "POST",
	}, nil
}

// TransformAzureToOpenAI transforms an Azure response back to OpenAI format
func (rt *RequestTransformer) TransformAzureToOpenAI(ctx context.Context, azureResponse map[string]interface{}, originalModel string) (map[string]interface{}, error) {
	// Clone the response
	openaiResponse := make(map[string]interface{})
	for k, v := range azureResponse {
		openaiResponse[k] = v
	}
	
	// Restore the original model name
	openaiResponse["model"] = originalModel
	
	// Handle specific Azure response fields that need transformation
	if choices, ok := openaiResponse["choices"].([]interface{}); ok {
		for i, choice := range choices {
			if choiceMap, ok := choice.(map[string]interface{}); ok {
				// Transform any Azure-specific fields back to OpenAI format
				choices[i] = choiceMap
			}
		}
		openaiResponse["choices"] = choices
	}
	
	return openaiResponse, nil
}

// estimateTokens estimates the number of tokens for a request
func (rt *RequestTransformer) estimateTokens(endpoint string, payload map[string]interface{}, modelName string) (int, error) {
	switch endpoint {
	case "/v1/chat/completions":
		messages, ok := payload["messages"].([]interface{})
		if !ok {
			return 100, nil // Default minimum
		}
		
		// Convert to proper format
		messagesMapped := make([]map[string]interface{}, len(messages))
		for i, msg := range messages {
			if msgMap, ok := msg.(map[string]interface{}); ok {
				messagesMapped[i] = msgMap
			}
		}
		
		var functions []map[string]interface{}
		if funcs, ok := payload["functions"].([]interface{}); ok {
			functions = make([]map[string]interface{}, len(funcs))
			for i, fn := range funcs {
				if fnMap, ok := fn.(map[string]interface{}); ok {
					functions[i] = fnMap
				}
			}
		}
		
		// Also check for tools (newer format)
		if tools, ok := payload["tools"].([]interface{}); ok {
			for _, tool := range tools {
				if toolMap, ok := tool.(map[string]interface{}); ok {
					if function, ok := toolMap["function"].(map[string]interface{}); ok {
						functions = append(functions, function)
					}
				}
			}
		}
		
		return rt.tokenEstimator.EstimateChatTokens(messagesMapped, functions, modelName, "azure")
		
	case "/v1/completions":
		if prompt, ok := payload["prompt"].(string); ok {
			return rt.tokenEstimator.EstimateCompletionTokens(prompt, modelName, "azure")
		}
		return 100, nil
		
	case "/v1/embeddings":
		if input, ok := payload["input"]; ok {
			return rt.tokenEstimator.EstimateEmbeddingTokens(input, modelName, "azure")
		}
		return 50, nil
		
	default:
		return 100, nil // Minimum for unknown endpoints
	}
}

// GetDeploymentName maps a model name to its deployment name
func (rt *RequestTransformer) GetDeploymentName(modelName string, deployments map[string]string) string {
	modelLower := strings.ToLower(modelName)
	
	// Check direct mapping first
	if deployment, exists := deployments[modelLower]; exists {
		return deployment
	}
	
	// Check common variations
	variations := []string{
		modelLower,
		strings.ReplaceAll(modelLower, ".", ""),
		strings.ReplaceAll(modelLower, "-", ""),
		strings.ReplaceAll(modelLower, "_", ""),
	}
	
	for _, variation := range variations {
		if deployment, exists := deployments[variation]; exists {
			return deployment
		}
	}
	
	// Fallback to model name
	return modelLower
}

// ValidateRequest validates a request payload
func (rt *RequestTransformer) ValidateRequest(endpoint string, payload map[string]interface{}) error {
	switch endpoint {
	case "/v1/chat/completions":
		if _, ok := payload["messages"]; !ok {
			return fmt.Errorf("messages field is required for chat completions")
		}
		
		if messages, ok := payload["messages"].([]interface{}); ok {
			if len(messages) == 0 {
				return fmt.Errorf("messages array cannot be empty")
			}
			
			// Validate message format
			for i, msg := range messages {
				if msgMap, ok := msg.(map[string]interface{}); ok {
					if _, hasRole := msgMap["role"]; !hasRole {
						return fmt.Errorf("message %d missing required 'role' field", i)
					}
					if _, hasContent := msgMap["content"]; !hasContent {
						return fmt.Errorf("message %d missing required 'content' field", i)
					}
				} else {
					return fmt.Errorf("message %d is not a valid object", i)
				}
			}
		} else {
			return fmt.Errorf("messages must be an array")
		}
		
	case "/v1/completions":
		if _, ok := payload["prompt"]; !ok {
			return fmt.Errorf("prompt field is required for completions")
		}
		
	case "/v1/embeddings":
		if _, ok := payload["input"]; !ok {
			return fmt.Errorf("input field is required for embeddings")
		}
	}
	
	return nil
}

// AddRequestMetadata adds metadata to the request for tracking
func (rt *RequestTransformer) AddRequestMetadata(payload map[string]interface{}, metadata map[string]interface{}) {
	// Add internal metadata that won't be sent to the API
	if metadata != nil {
		for k, v := range metadata {
			// Use internal prefix to avoid conflicts
			payload["_internal_"+k] = v
		}
	}
}

// CleanRequestMetadata removes internal metadata before sending to API
func (rt *RequestTransformer) CleanRequestMetadata(payload map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{})
	
	for k, v := range payload {
		// Skip internal metadata fields
		if !strings.HasPrefix(k, "_internal_") {
			cleaned[k] = v
		}
	}
	
	return cleaned
}

// EstimateResponseTokens estimates response tokens based on request
func (rt *RequestTransformer) EstimateResponseTokens(payload map[string]interface{}) int {
	// Check max_tokens setting
	if maxTokens, ok := payload["max_tokens"]; ok {
		if maxTokensFloat, ok := maxTokens.(float64); ok {
			return int(maxTokensFloat)
		}
		if maxTokensInt, ok := maxTokens.(int); ok {
			return maxTokensInt
		}
	}
	
	// Default estimation based on endpoint
	return 150 // Conservative default
}