package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	
	"azure-openai-proxy/internal/config"
	"azure-openai-proxy/internal/handlers"
	"azure-openai-proxy/internal/instance"
	"azure-openai-proxy/internal/middleware"
	
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHealthEndpoint(t *testing.T) {
	// Setup test router
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	// Add basic middleware
	router.Use(middleware.RequestID())
	router.Use(middleware.CORS())
	
	// Setup health endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})
	
	// Create test request
	req, _ := http.NewRequest("GET", "/health", nil)
	resp := httptest.NewRecorder()
	
	// Perform request
	router.ServeHTTP(resp, req)
	
	// Assert response
	assert.Equal(t, 200, resp.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "healthy", response["status"])
}

func TestProxyHandlerValidation(t *testing.T) {
	// Setup test configuration
	testConfigs := []config.InstanceConfig{
		{
			Name:            "test-instance",
			ProviderType:    "azure",
			APIKey:          "test-key",
			APIBase:         "https://test.openai.azure.com",
			APIVersion:      "2024-05-01-preview",
			Priority:        1,
			Weight:          10,
			MaxTPM:          60000,
			MaxInputTokens:  8000,
			SupportedModels: []string{"gpt-4", "gpt-35-turbo"},
			ModelDeployments: map[string]string{
				"gpt-4": "gpt-4-deployment",
			},
			Enabled:          true,
			TimeoutSeconds:   30.0,
			RetryCount:       3,
			RateLimitEnabled: false, // Disabled for testing
		},
	}
	
	// Create mock stores
	stateStore := &MockStateStore{}
	configStore := &MockConfigStore{}
	
	// Create instance manager
	instanceManager, err := instance.NewManager(testConfigs, "weighted", stateStore, configStore)
	assert.NoError(t, err)
	
	// Create proxy handler
	proxyHandler := handlers.NewProxyHandler(instanceManager)
	
	// Setup test router
	router := gin.New()
	router.Use(middleware.RequestID())
	router.POST("/v1/chat/completions", proxyHandler.ChatCompletions)
	
	// Test invalid JSON request
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	
	router.ServeHTTP(resp, req)
	
	assert.Equal(t, 400, resp.Code)
	
	var errorResponse map[string]interface{}
	err = json.Unmarshal(resp.Body.Bytes(), &errorResponse)
	assert.NoError(t, err)
	assert.Contains(t, errorResponse, "error")
}

func TestAdminEndpoints(t *testing.T) {
	// Create test configuration
	testConfigs := []config.InstanceConfig{
		{
			Name:         "test-instance",
			ProviderType: "azure",
			APIKey:       "test-key",
			APIBase:      "https://test.openai.azure.com",
			Enabled:      true,
		},
	}
	
	// Create mock stores
	stateStore := &MockStateStore{}
	configStore := &MockConfigStore{}
	
	// Create instance manager
	instanceManager, err := instance.NewManager(testConfigs, "weighted", stateStore, configStore)
	assert.NoError(t, err)
	
	// Create admin handler
	adminHandler := handlers.NewAdminHandler(instanceManager)
	
	// Setup test router
	router := gin.New()
	router.GET("/admin/config", adminHandler.GetConfig)
	
	// Test get config endpoint
	req, _ := http.NewRequest("GET", "/admin/config", nil)
	resp := httptest.NewRecorder()
	
	router.ServeHTTP(resp, req)
	
	assert.Equal(t, 200, resp.Code)
	
	var response map[string]interface{}
	err = json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "instances")
}

// Mock implementations for testing

type MockStateStore struct{}

func (m *MockStateStore) Get(ctx context.Context, instanceName string) (*config.InstanceState, error) {
	return config.NewInstanceState(instanceName), nil
}

func (m *MockStateStore) Set(ctx context.Context, instanceName string, state *config.InstanceState) error {
	return nil
}

func (m *MockStateStore) Delete(ctx context.Context, instanceName string) error {
	return nil
}

func (m *MockStateStore) List(ctx context.Context) ([]string, error) {
	return []string{"test-instance"}, nil
}

func (m *MockStateStore) GetAll(ctx context.Context) (map[string]*config.InstanceState, error) {
	return map[string]*config.InstanceState{
		"test-instance": config.NewInstanceState("test-instance"),
	}, nil
}

func (m *MockStateStore) Close() error {
	return nil
}

type MockConfigStore struct{}

func (m *MockConfigStore) SaveConfig(ctx context.Context, config *config.AppConfig) error {
	return nil
}

func (m *MockConfigStore) LoadConfig(ctx context.Context) (*config.AppConfig, error) {
	return &config.AppConfig{}, nil
}

func (m *MockConfigStore) SaveInstanceConfig(ctx context.Context, instance *config.InstanceConfig) error {
	return nil
}

func (m *MockConfigStore) LoadInstanceConfig(ctx context.Context, name string) (*config.InstanceConfig, error) {
	return &config.InstanceConfig{Name: name}, nil
}

func (m *MockConfigStore) DeleteInstanceConfig(ctx context.Context, name string) error {
	return nil
}

func (m *MockConfigStore) ListInstanceConfigs(ctx context.Context) ([]string, error) {
	return []string{"test-instance"}, nil
}

func (m *MockConfigStore) Close() error {
	return nil
}