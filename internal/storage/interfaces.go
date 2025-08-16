package storage

import (
	"context"
	"azure-openai-proxy/internal/config"
)

// StateStore defines the interface for storing instance state
type StateStore interface {
	// Get retrieves the state for a specific instance
	Get(ctx context.Context, instanceName string) (*config.InstanceState, error)
	
	// Set stores the state for a specific instance
	Set(ctx context.Context, instanceName string, state *config.InstanceState) error
	
	// Delete removes the state for a specific instance
	Delete(ctx context.Context, instanceName string) error
	
	// List returns all instance names that have stored state
	List(ctx context.Context) ([]string, error)
	
	// GetAll retrieves states for all instances
	GetAll(ctx context.Context) (map[string]*config.InstanceState, error)
	
	// Close closes the storage connection
	Close() error
}

// ConfigStore defines the interface for storing configuration data
type ConfigStore interface {
	// SaveConfig saves the application configuration
	SaveConfig(ctx context.Context, config *config.AppConfig) error
	
	// LoadConfig loads the application configuration
	LoadConfig(ctx context.Context) (*config.AppConfig, error)
	
	// SaveInstanceConfig saves a specific instance configuration
	SaveInstanceConfig(ctx context.Context, instance *config.InstanceConfig) error
	
	// LoadInstanceConfig loads a specific instance configuration
	LoadInstanceConfig(ctx context.Context, name string) (*config.InstanceConfig, error)
	
	// DeleteInstanceConfig deletes a specific instance configuration
	DeleteInstanceConfig(ctx context.Context, name string) error
	
	// ListInstanceConfigs returns all instance configuration names
	ListInstanceConfigs(ctx context.Context) ([]string, error)
	
	// Close closes the storage connection
	Close() error
}