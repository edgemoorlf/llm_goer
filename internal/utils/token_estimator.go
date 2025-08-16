package utils

import (
	"fmt"
	"strings"
	"sync"
	
	"github.com/pkoukk/tiktoken-go"
)

// TokenEstimator handles token counting for different models and providers
type TokenEstimator struct {
	encoders map[string]*tiktoken.Tiktoken
	mutex    sync.RWMutex
}

// NewTokenEstimator creates a new token estimator
func NewTokenEstimator() *TokenEstimator {
	return &TokenEstimator{
		encoders: make(map[string]*tiktoken.Tiktoken),
	}
}

// GetEncoderForModel gets or creates an encoder for a specific model and provider
func (te *TokenEstimator) GetEncoderForModel(model, provider string) (*tiktoken.Tiktoken, error) {
	te.mutex.RLock()
	if encoder, exists := te.encoders[model]; exists {
		te.mutex.RUnlock()
		return encoder, nil
	}
	te.mutex.RUnlock()
	
	te.mutex.Lock()
	defer te.mutex.Unlock()
	
	// Double-check after acquiring write lock
	if encoder, exists := te.encoders[model]; exists {
		return encoder, nil
	}
	
	// Model mapping for different providers
	actualModel := model
	if provider == "azure" {
		modelMapping := map[string]string{
			"gpt-4":         "gpt-4-0613",
			"gpt-4o":        "gpt-4o-2024-05-13",
			"gpt-35-turbo":  "gpt-3.5-turbo-0613",
			"gpt-3.5-turbo": "gpt-3.5-turbo-0613",
		}
		if mapped, exists := modelMapping[strings.ToLower(model)]; exists {
			actualModel = mapped
		}
	}
	
	encoder, err := tiktoken.EncodingForModel(actualModel)
	if err != nil {
		// Fallback to cl100k_base encoding
		encoder, err = tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			return nil, fmt.Errorf("failed to get encoder: %w", err)
		}
	}
	
	te.encoders[model] = encoder
	return encoder, nil
}

// EstimateChatTokens estimates tokens for chat completion requests
func (te *TokenEstimator) EstimateChatTokens(messages []map[string]interface{}, functions []map[string]interface{}, model, provider string) (int, error) {
	encoder, err := te.GetEncoderForModel(model, provider)
	if err != nil {
		return 0, err
	}
	
	// Adjust tokens per message based on model
	tokensPerMessage := 3 // Default for GPT-3.5
	if strings.Contains(strings.ToLower(model), "gpt-4") {
		tokensPerMessage = 4 // GPT-4 uses 4 tokens per message
	}
	tokensPerFunction := 6 // function_call + name + arguments
	
	tokenCount := 0
	
	// Count message tokens
	for _, message := range messages {
		tokenCount += tokensPerMessage
		for key, value := range message {
			if key == "content" {
				switch v := value.(type) {
				case string:
					tokens := encoder.Encode(v, nil, nil)
					tokenCount += len(tokens)
				case []interface{}:
					// Handle multimodal content
					for _, item := range v {
						if itemMap, ok := item.(map[string]interface{}); ok {
							if text, ok := itemMap["text"].(string); ok {
								tokens := encoder.Encode(text, nil, nil)
								tokenCount += len(tokens)
							}
							// For images, add estimated token count
							if itemMap["type"] == "image_url" {
								tokenCount += 85 // Base tokens for image
								if detail, ok := itemMap["detail"].(string); ok && detail == "high" {
									tokenCount += 170 // Additional tokens for high detail
								}
							}
						} else if str, ok := item.(string); ok {
							tokens := encoder.Encode(str, nil, nil)
							tokenCount += len(tokens)
						}
					}
				}
			} else {
				if str, ok := value.(string); ok {
					tokens := encoder.Encode(str, nil, nil)
					tokenCount += len(tokens)
				}
			}
		}
	}
	
	// Count function tokens
	if functions != nil {
		for _, function := range functions {
			tokenCount += tokensPerFunction
			if functionStr, err := te.stringifyFunction(function); err == nil {
				tokens := encoder.Encode(functionStr, nil, nil)
				tokenCount += len(tokens)
			}
		}
	}
	
	// Add per-request overhead
	tokenCount += 3 // every reply is primed with <|start|>assistant<|message|>
	
	if tokenCount < 1 {
		tokenCount = 1 // Ensure at least 1 token
	}
	
	return tokenCount, nil
}

// EstimateCompletionTokens estimates tokens for text completion requests
func (te *TokenEstimator) EstimateCompletionTokens(prompt, model, provider string) (int, error) {
	encoder, err := te.GetEncoderForModel(model, provider)
	if err != nil {
		return 0, err
	}
	
	tokens := encoder.Encode(prompt, nil, nil)
	count := len(tokens)
	
	if count < 1 {
		count = 1
	}
	
	return count, nil
}

// EstimateEmbeddingTokens estimates tokens for embedding requests
func (te *TokenEstimator) EstimateEmbeddingTokens(input interface{}, model, provider string) (int, error) {
	encoder, err := te.GetEncoderForModel(model, provider)
	if err != nil {
		return 0, err
	}
	
	var totalTokens int
	
	switch v := input.(type) {
	case string:
		tokens := encoder.Encode(v, nil, nil)
		totalTokens = len(tokens)
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				tokens := encoder.Encode(str, nil, nil)
				totalTokens += len(tokens)
			}
		}
	case []string:
		for _, str := range v {
			tokens := encoder.Encode(str, nil, nil)
			totalTokens += len(tokens)
		}
	default:
		return 0, fmt.Errorf("unsupported input type for embedding estimation")
	}
	
	if totalTokens < 1 {
		totalTokens = 1
	}
	
	return totalTokens, nil
}

// stringifyFunction converts a function definition to a string for token counting
func (te *TokenEstimator) stringifyFunction(function map[string]interface{}) (string, error) {
	var parts []string
	
	if name, ok := function["name"].(string); ok {
		parts = append(parts, name)
	}
	
	if description, ok := function["description"].(string); ok {
		parts = append(parts, description)
	}
	
	if parameters, ok := function["parameters"].(map[string]interface{}); ok {
		if paramStr := te.stringifyParameters(parameters); paramStr != "" {
			parts = append(parts, paramStr)
		}
	}
	
	return strings.Join(parts, " "), nil
}

// stringifyParameters converts function parameters to a string
func (te *TokenEstimator) stringifyParameters(params map[string]interface{}) string {
	var parts []string
	
	// Add type information
	if paramType, ok := params["type"].(string); ok {
		parts = append(parts, paramType)
	}
	
	// Add properties
	if properties, ok := params["properties"].(map[string]interface{}); ok {
		for propName, propDef := range properties {
			parts = append(parts, propName)
			if propMap, ok := propDef.(map[string]interface{}); ok {
				if propType, ok := propMap["type"].(string); ok {
					parts = append(parts, propType)
				}
				if description, ok := propMap["description"].(string); ok {
					parts = append(parts, description)
				}
			}
		}
	}
	
	// Add required fields
	if required, ok := params["required"].([]interface{}); ok {
		for _, req := range required {
			if reqStr, ok := req.(string); ok {
				parts = append(parts, reqStr)
			}
		}
	}
	
	return strings.Join(parts, " ")
}

// GetModelInfo returns information about a model's token limits and characteristics
func (te *TokenEstimator) GetModelInfo(model string) map[string]interface{} {
	modelInfo := map[string]interface{}{
		"model":            model,
		"max_tokens":       4096, // Default
		"supports_vision":  false,
		"supports_functions": true,
	}
	
	modelLower := strings.ToLower(model)
	
	switch {
	case strings.Contains(modelLower, "gpt-4o"):
		modelInfo["max_tokens"] = 128000
		modelInfo["supports_vision"] = true
	case strings.Contains(modelLower, "gpt-4"):
		if strings.Contains(modelLower, "32k") {
			modelInfo["max_tokens"] = 32768
		} else if strings.Contains(modelLower, "turbo") {
			modelInfo["max_tokens"] = 128000
		} else {
			modelInfo["max_tokens"] = 8192
		}
		modelInfo["supports_vision"] = strings.Contains(modelLower, "vision")
	case strings.Contains(modelLower, "gpt-3.5") || strings.Contains(modelLower, "gpt-35"):
		if strings.Contains(modelLower, "16k") {
			modelInfo["max_tokens"] = 16384
		} else {
			modelInfo["max_tokens"] = 4096
		}
	case strings.Contains(modelLower, "text-embedding"):
		modelInfo["max_tokens"] = 8191
		modelInfo["supports_functions"] = false
	case strings.Contains(modelLower, "whisper"):
		modelInfo["max_tokens"] = 0 // Audio model
		modelInfo["supports_functions"] = false
	case strings.Contains(modelLower, "tts"):
		modelInfo["max_tokens"] = 4096
		modelInfo["supports_functions"] = false
	}
	
	return modelInfo
}