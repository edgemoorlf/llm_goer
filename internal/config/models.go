package config

import (
	"time"
)

// InstanceStatus represents the health status of an instance
type InstanceStatus string

const (
	StatusHealthy     InstanceStatus = "healthy"
	StatusRateLimited InstanceStatus = "rate_limited"
	StatusError       InstanceStatus = "error"
)

// InstanceConfig represents static configuration for an API instance
type InstanceConfig struct {
	Name             string            `json:"name" yaml:"name" validate:"required"`
	ProviderType     string            `json:"provider_type" yaml:"provider_type" validate:"required,oneof=azure openai"`
	APIKey           string            `json:"api_key" yaml:"api_key" validate:"required"`
	APIBase          string            `json:"api_base" yaml:"api_base" validate:"required,url"`
	APIVersion       string            `json:"api_version" yaml:"api_version"`
	ProxyURL         *string           `json:"proxy_url,omitempty" yaml:"proxy_url,omitempty"`
	Priority         int               `json:"priority" yaml:"priority" validate:"min=0"`
	Weight           int               `json:"weight" yaml:"weight" validate:"min=1"`
	MaxTPM           int               `json:"max_tpm" yaml:"max_tpm" validate:"min=1"`
	MaxInputTokens   int               `json:"max_input_tokens" yaml:"max_input_tokens" validate:"min=0"`
	SupportedModels  []string          `json:"supported_models" yaml:"supported_models"`
	ModelDeployments map[string]string `json:"model_deployments" yaml:"model_deployments"`
	Enabled          bool              `json:"enabled" yaml:"enabled"`
	TimeoutSeconds   float64           `json:"timeout_seconds" yaml:"timeout_seconds" validate:"min=0"`
	RetryCount       int               `json:"retry_count" yaml:"retry_count" validate:"min=0"`
	RateLimitEnabled bool              `json:"rate_limit_enabled" yaml:"rate_limit_enabled"`
}

// InstanceState represents dynamic runtime state for an API instance
type InstanceState struct {
	Name               string                 `json:"name"`
	Status             InstanceStatus         `json:"status"`
	HealthStatus       string                 `json:"health_status"`
	ConnectionStatus   string                 `json:"connection_status"`
	
	// Error tracking
	ErrorCount         int                    `json:"error_count"`
	LastError          *string                `json:"last_error,omitempty"`
	LastErrorTime      *int64                 `json:"last_error_time,omitempty"`
	
	// Instance-level errors
	TotalErrors500     int                    `json:"total_errors_500"`
	TotalErrors503     int                    `json:"total_errors_503"`
	TotalOtherErrors   int                    `json:"total_other_errors"`
	Error500Window     map[int64]int          `json:"error_500_window"`
	Error503Window     map[int64]int          `json:"error_503_window"`
	ErrorOtherWindow   map[int64]int          `json:"error_other_window"`
	CurrentErrorRate   float64                `json:"current_error_rate"`
	Current500Rate     float64                `json:"current_500_rate"`
	Current503Rate     float64                `json:"current_503_rate"`
	
	// Client-level errors
	TotalClientErrors500    int           `json:"total_client_errors_500"`
	TotalClientErrors503    int           `json:"total_client_errors_503"`
	TotalClientErrorsOther  int           `json:"total_client_errors_other"`
	ClientError500Window    map[int64]int `json:"client_error_500_window"`
	ClientError503Window    map[int64]int `json:"client_error_503_window"`
	ClientErrorOtherWindow  map[int64]int `json:"client_error_other_window"`
	CurrentClientErrorRate  float64       `json:"current_client_error_rate"`
	CurrentClient500Rate    float64       `json:"current_client_500_rate"`
	CurrentClient503Rate    float64       `json:"current_client_503_rate"`
	
	// Upstream errors
	TotalUpstream429Errors  int           `json:"total_upstream_429_errors"`
	TotalUpstream400Errors  int           `json:"total_upstream_400_errors"`
	TotalUpstream500Errors  int           `json:"total_upstream_500_errors"`
	TotalUpstreamOtherErrors int          `json:"total_upstream_other_errors"`
	Upstream429Window       map[int64]int `json:"upstream_429_window"`
	Upstream400Window       map[int64]int `json:"upstream_400_window"`
	Upstream500Window       map[int64]int `json:"upstream_500_window"`
	UpstreamOtherWindow     map[int64]int `json:"upstream_other_window"`
	CurrentUpstreamErrorRate float64      `json:"current_upstream_error_rate"`
	CurrentUpstream429Rate   float64      `json:"current_upstream_429_rate"`
	CurrentUpstream400Rate   float64      `json:"current_upstream_400_rate"`
	
	// Rate limiting
	RateLimitedUntil *int64                `json:"rate_limited_until,omitempty"`
	
	// Usage metrics
	CurrentTPM         int                  `json:"current_tpm"`
	CurrentRPM         int                  `json:"current_rpm"`
	TotalRequests      int                  `json:"total_requests"`
	SuccessfulRequests int                  `json:"successful_requests"`
	TotalTokensServed  int64                `json:"total_tokens_served"`
	
	// Usage windows (timestamp -> count)
	UsageWindow   map[int64]int            `json:"usage_window"`
	RequestWindow map[int64]int            `json:"request_window"`
	
	// Performance metrics
	AvgLatencyMs          *float64         `json:"avg_latency_ms,omitempty"`
	UtilizationPercentage float64          `json:"utilization_percentage"`
	
	// Timestamps
	LastUsed time.Time                    `json:"last_used"`
}

// IsHealthy checks if the instance is in a healthy state
func (s *InstanceState) IsHealthy() bool {
	return s.Status == StatusHealthy
}

// NewInstanceState creates a new instance state with initialized maps
func NewInstanceState(name string) *InstanceState {
	return &InstanceState{
		Name:                   name,
		Status:                 StatusHealthy,
		HealthStatus:          "unknown",
		ConnectionStatus:      "unknown",
		Error500Window:        make(map[int64]int),
		Error503Window:        make(map[int64]int),
		ErrorOtherWindow:      make(map[int64]int),
		ClientError500Window:  make(map[int64]int),
		ClientError503Window:  make(map[int64]int),
		ClientErrorOtherWindow: make(map[int64]int),
		Upstream429Window:     make(map[int64]int),
		Upstream400Window:     make(map[int64]int),
		Upstream500Window:     make(map[int64]int),
		UpstreamOtherWindow:   make(map[int64]int),
		UsageWindow:           make(map[int64]int),
		RequestWindow:         make(map[int64]int),
		LastUsed:              time.Now(),
	}
}

// RoutingConfig represents routing strategy configuration
type RoutingConfig struct {
	Strategy string `json:"strategy" yaml:"strategy" validate:"oneof=failover weighted round_robin"`
	Retries  int    `json:"retries" yaml:"retries" validate:"min=0"`
	Timeout  int    `json:"timeout" yaml:"timeout" validate:"min=1"`
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level         string  `json:"level" yaml:"level" validate:"oneof=DEBUG INFO WARN ERROR"`
	File          string  `json:"file" yaml:"file"`
	MaxSize       int     `json:"max_size" yaml:"max_size" validate:"min=1"`
	BackupCount   int     `json:"backup_count" yaml:"backup_count" validate:"min=0"`
	FeishuWebhook *string `json:"feishu_webhook,omitempty" yaml:"feishu_webhook,omitempty"`
}

// MonitoringConfig represents monitoring configuration
type MonitoringConfig struct {
	StatsWindowMinutes  int   `json:"stats_window_minutes" yaml:"stats_window_minutes" validate:"min=1"`
	AdditionalWindows   []int `json:"additional_windows" yaml:"additional_windows"`
}

// AppConfig represents the main application configuration
type AppConfig struct {
	Name       string             `json:"name" yaml:"name"`
	Version    string             `json:"version" yaml:"version"`
	Port       int                `json:"port" yaml:"port" validate:"min=1,max=65535"`
	Instances  []InstanceConfig   `json:"instances" yaml:"instances"`
	Routing    RoutingConfig      `json:"routing" yaml:"routing"`
	Logging    LoggingConfig      `json:"logging" yaml:"logging"`
	Monitoring MonitoringConfig   `json:"monitoring" yaml:"monitoring"`
}