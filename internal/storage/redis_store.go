package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	
	"azure-openai-proxy/internal/config"
	
	"github.com/go-redis/redis/v8"
)

// RedisStore implements StateStore using Redis
type RedisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore creates a new Redis-based state store
func NewRedisStore(redisURL, password string) (*RedisStore, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}
	
	if password != "" {
		opt.Password = password
	}
	
	client := redis.NewClient(opt)
	
	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}
	
	return &RedisStore{
		client: client,
		prefix: "proxy:instance:state:",
	}, nil
}

// Get retrieves the state for a specific instance
func (r *RedisStore) Get(ctx context.Context, instanceName string) (*config.InstanceState, error) {
	key := r.prefix + instanceName
	
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			// Return new state if not found
			return config.NewInstanceState(instanceName), nil
		}
		return nil, fmt.Errorf("failed to get instance state from Redis: %w", err)
	}
	
	var state config.InstanceState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal instance state: %w", err)
	}
	
	return &state, nil
}

// Set stores the state for a specific instance
func (r *RedisStore) Set(ctx context.Context, instanceName string, state *config.InstanceState) error {
	key := r.prefix + instanceName
	
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal instance state: %w", err)
	}
	
	// Set with expiration (24 hours)
	err = r.client.Set(ctx, key, data, 24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to set instance state in Redis: %w", err)
	}
	
	return nil
}

// Delete removes the state for a specific instance
func (r *RedisStore) Delete(ctx context.Context, instanceName string) error {
	key := r.prefix + instanceName
	
	err := r.client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete instance state from Redis: %w", err)
	}
	
	return nil
}

// List returns all instance names that have stored state
func (r *RedisStore) List(ctx context.Context) ([]string, error) {
	pattern := r.prefix + "*"
	
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list instance states from Redis: %w", err)
	}
	
	instances := make([]string, len(keys))
	for i, key := range keys {
		instances[i] = key[len(r.prefix):]
	}
	
	return instances, nil
}

// GetAll retrieves states for all instances
func (r *RedisStore) GetAll(ctx context.Context) (map[string]*config.InstanceState, error) {
	instances, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	
	states := make(map[string]*config.InstanceState)
	
	for _, instanceName := range instances {
		state, err := r.Get(ctx, instanceName)
		if err != nil {
			// Skip instances with errors but continue processing
			continue
		}
		states[instanceName] = state
	}
	
	return states, nil
}

// Close closes the Redis connection
func (r *RedisStore) Close() error {
	return r.client.Close()
}

// GetUsageWindow retrieves usage data for a specific time window
func (r *RedisStore) GetUsageWindow(ctx context.Context, instanceName string, windowSeconds int) (map[int64]int, error) {
	key := fmt.Sprintf("proxy:usage:window:%s", instanceName)
	
	currentTime := time.Now().Unix()
	cutoff := currentTime - int64(windowSeconds)
	
	// Remove old entries
	err := r.client.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", cutoff)).Err()
	if err != nil {
		return nil, fmt.Errorf("failed to clean old usage entries: %w", err)
	}
	
	// Get all entries within the window
	entries, err := r.client.ZRangeWithScores(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get usage entries: %w", err)
	}
	
	// Build usage map
	usage := make(map[int64]int)
	for _, entry := range entries {
		timestamp := int64(entry.Score)
		tokens := 0
		
		// Parse token count from member string
		if tokenStr, ok := entry.Member.(string); ok {
			fmt.Sscanf(tokenStr, "%d", &tokens)
		}
		
		usage[timestamp] += tokens
	}
	
	return usage, nil
}

// UpdateUsage adds token usage for an instance
func (r *RedisStore) UpdateUsage(ctx context.Context, instanceName string, tokens int) error {
	key := fmt.Sprintf("proxy:usage:window:%s", instanceName)
	
	currentTime := time.Now()
	uniqueKey := fmt.Sprintf("%d:%d", tokens, currentTime.UnixNano())
	
	pipe := r.client.Pipeline()
	
	// Add new usage entry
	pipe.ZAdd(ctx, key, &redis.Z{
		Score:  float64(currentTime.Unix()),
		Member: uniqueKey,
	})
	
	// Clean up old entries (older than 24 hours)
	cutoff := currentTime.Unix() - 24*60*60
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", cutoff))
	
	// Set expiration on the key
	pipe.Expire(ctx, key, 25*time.Hour)
	
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update usage in Redis: %w", err)
	}
	
	return nil
}