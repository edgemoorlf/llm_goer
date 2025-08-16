package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	
	"gopkg.in/yaml.v3"
)

// Loader handles configuration loading and environment variable resolution
type Loader struct {
	environment string
}

// NewLoader creates a new configuration loader
func NewLoader() *Loader {
	return &Loader{
		environment: getEnv("ENVIRONMENT", "development"),
	}
}

// LoadConfig loads configuration from YAML files with environment variable resolution
func (l *Loader) LoadConfig(configDir string) (*AppConfig, error) {
	var config AppConfig
	
	// Load base configuration
	baseConfig, err := l.loadConfigFile(fmt.Sprintf("%s/base.yaml", configDir))
	if err != nil {
		return nil, fmt.Errorf("failed to load base config: %w", err)
	}
	
	// Load environment-specific configuration (optional)
	envConfig, err := l.loadConfigFile(fmt.Sprintf("%s/%s.yaml", configDir, l.environment))
	if err != nil {
		// Environment config is optional
		envConfig = make(map[string]interface{})
	}
	
	// Merge configurations (environment overrides base)
	merged := l.deepMerge(baseConfig, envConfig)
	
	// Resolve environment variables
	resolved := l.resolveEnvVars(merged)
	
	// Marshal to YAML and unmarshal to struct for validation
	yamlData, err := yaml.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged config: %w", err)
	}
	
	if err := yaml.Unmarshal(yamlData, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	
	// Validate configuration
	if err := l.validateConfig(&config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}
	
	return &config, nil
}

// loadConfigFile loads a single YAML configuration file
func (l *Loader) loadConfigFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	
	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	
	return config, nil
}

// deepMerge recursively merges two configuration maps
func (l *Loader) deepMerge(base, override map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	
	// Copy base
	for k, v := range base {
		result[k] = v
	}
	
	// Apply overrides
	for k, v := range override {
		if baseVal, exists := result[k]; exists {
			if baseMap, ok := baseVal.(map[string]interface{}); ok {
				if overrideMap, ok := v.(map[string]interface{}); ok {
					result[k] = l.deepMerge(baseMap, overrideMap)
					continue
				}
			}
		}
		result[k] = v
	}
	
	return result
}

// resolveEnvVars recursively resolves environment variables in configuration
func (l *Loader) resolveEnvVars(config interface{}) interface{} {
	switch v := config.(type) {
	case string:
		return l.resolveEnvVar(v)
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = l.resolveEnvVars(val)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = l.resolveEnvVars(val)
		}
		return result
	default:
		return config
	}
}

// resolveEnvVar resolves environment variables in a string
// Supports formats: ${ENV_VAR} and ${ENV_VAR:default_value}
func (l *Loader) resolveEnvVar(value string) string {
	// Pattern: ${ENV_VAR} or ${ENV_VAR:default_value}
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(value, func(match string) string {
		// Extract variable name (remove ${ and })
		varName := match[2 : len(match)-1]
		
		// Check for default value
		parts := strings.SplitN(varName, ":", 2)
		envVar := parts[0]
		defaultValue := ""
		if len(parts) > 1 {
			defaultValue = parts[1]
		}
		
		// Get environment variable value
		if envValue := os.Getenv(envVar); envValue != "" {
			return envValue
		}
		
		return defaultValue
	})
}

// validateConfig performs basic validation on the loaded configuration
func (l *Loader) validateConfig(config *AppConfig) error {
	// Validate required fields
	if config.Port <= 0 || config.Port > 65535 {
		return fmt.Errorf("invalid port: %d", config.Port)
	}
	
	if len(config.Instances) == 0 {
		return fmt.Errorf("no instances configured")
	}
	
	// Validate instances
	for i, instance := range config.Instances {
		if err := l.validateInstance(&instance); err != nil {
			return fmt.Errorf("instance %d validation failed: %w", i, err)
		}
	}
	
	// Validate routing strategy
	validStrategies := map[string]bool{
		"failover":    true,
		"weighted":    true,
		"round_robin": true,
	}
	if !validStrategies[config.Routing.Strategy] {
		return fmt.Errorf("invalid routing strategy: %s", config.Routing.Strategy)
	}
	
	return nil
}

// validateInstance validates a single instance configuration
func (l *Loader) validateInstance(instance *InstanceConfig) error {
	if instance.Name == "" {
		return fmt.Errorf("instance name is required")
	}
	
	if instance.APIKey == "" {
		return fmt.Errorf("API key is required for instance %s", instance.Name)
	}
	
	if instance.APIBase == "" {
		return fmt.Errorf("API base URL is required for instance %s", instance.Name)
	}
	
	validProviders := map[string]bool{
		"azure":  true,
		"openai": true,
	}
	if !validProviders[instance.ProviderType] {
		return fmt.Errorf("invalid provider type for instance %s: %s", instance.Name, instance.ProviderType)
	}
	
	if instance.Weight <= 0 {
		return fmt.Errorf("instance weight must be positive for instance %s", instance.Name)
	}
	
	if instance.MaxTPM <= 0 {
		return fmt.Errorf("max TPM must be positive for instance %s", instance.Name)
	}
	
	if instance.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout must be positive for instance %s", instance.Name)
	}
	
	return nil
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}