package health

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Checker provides health check functionality
type Checker struct {
	httpClient *http.Client
}

// NewChecker creates a new health checker
func NewChecker() *Checker {
	return &Checker{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CheckEndpoint performs a health check on an API endpoint
func (c *Checker) CheckEndpoint(ctx context.Context, url string, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url+"/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	// Add authorization header
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "Azure-OpenAI-Proxy/1.0")
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 400 {
		return fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	
	return nil
}

// CheckRedis performs a health check on Redis
func (c *Checker) CheckRedis(ctx context.Context, redisURL string) error {
	// This would use the Redis client to ping
	// For now, just return nil
	return nil
}

// CheckSQLite performs a health check on SQLite
func (c *Checker) CheckSQLite(ctx context.Context, dbPath string) error {
	// This would open and ping the database
	// For now, just return nil
	return nil
}