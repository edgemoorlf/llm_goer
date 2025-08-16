package utils

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	
	"github.com/go-redis/redis/v8"
)

// RateLimiter implements Redis-based sliding window rate limiting
type RateLimiter struct {
	instanceID      string
	tokensPerMinute int
	maxInputTokens  int
	windowSeconds   int
	redisKey        string
	redis           *redis.Client
}

// NewRateLimiter creates a new rate limiter for an instance
func NewRateLimiter(instanceID string, tokensPerMinute, maxInputTokens int, redisURL, redisPassword string) (*RateLimiter, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}
	
	if redisPassword != "" {
		opt.Password = redisPassword
	}
	
	client := redis.NewClient(opt)
	
	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis for rate limiting: %w", err)
	}
	
	return &RateLimiter{
		instanceID:      instanceID,
		tokensPerMinute: tokensPerMinute,
		maxInputTokens:  maxInputTokens,
		windowSeconds:   60, // 1 minute window
		redisKey:        fmt.Sprintf("instance:rate_limit:window:%s", instanceID),
		redis:           client,
	}, nil
}

// GetCurrentUsage calculates current token usage within the time window
func (rl *RateLimiter) GetCurrentUsage(ctx context.Context) (int, error) {
	currentTime := time.Now().Unix()
	cutoff := currentTime - int64(rl.windowSeconds)
	
	// Remove old entries
	err := rl.redis.ZRemRangeByScore(ctx, rl.redisKey, "-inf", fmt.Sprintf("%d", cutoff)).Err()
	if err != nil {
		return 0, fmt.Errorf("failed to clean old entries: %w", err)
	}
	
	// Get all entries within the window
	entries, err := rl.redis.ZRangeWithScores(ctx, rl.redisKey, 0, -1).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get entries: %w", err)
	}
	
	// Calculate total token usage
	currentUsage := 0
	for _, entry := range entries {
		tokenValue := strings.Split(entry.Member.(string), ":")[0]
		tokens, err := strconv.Atoi(tokenValue)
		if err != nil {
			continue // Skip invalid entries
		}
		currentUsage += tokens
	}
	
	return currentUsage, nil
}

// CheckCapacity checks if the instance has capacity for the requested tokens
func (rl *RateLimiter) CheckCapacity(ctx context.Context, tokens int) (bool, int, error) {
	// Check input token limit
	if rl.maxInputTokens > 0 && tokens > rl.maxInputTokens {
		return false, 60, nil
	}
	
	currentTime := time.Now().Unix()
	cutoff := currentTime - int64(rl.windowSeconds)
	
	pipe := rl.redis.Pipeline()
	
	// Remove old entries
	pipe.ZRemRangeByScore(ctx, rl.redisKey, "-inf", fmt.Sprintf("%d", cutoff))
	
	// Get current usage
	pipe.ZRangeWithScores(ctx, rl.redisKey, 0, -1)
	
	results, err := pipe.Exec(ctx)
	if err != nil {
		// Fail open on Redis errors to avoid blocking requests
		return true, 0, nil
	}
	
	entries := results[1].(*redis.ZSliceCmd).Val()
	
	// Calculate current usage
	currentUsage := 0
	oldestTime := currentTime
	for _, entry := range entries {
		tokenValue := strings.Split(entry.Member.(string), ":")[0]
		tokenCount, err := strconv.Atoi(tokenValue)
		if err != nil {
			continue
		}
		currentUsage += tokenCount
		if int64(entry.Score) < oldestTime {
			oldestTime = int64(entry.Score)
		}
	}
	
	// Check if adding tokens would exceed limit
	if currentUsage+tokens > rl.tokensPerMinute {
		retryAfter := int(oldestTime - cutoff)
		if retryAfter < 1 {
			retryAfter = 1
		}
		return false, retryAfter, nil
	}
	
	return true, 0, nil
}

// UpdateUsage records token usage for rate limiting
func (rl *RateLimiter) UpdateUsage(ctx context.Context, tokens int) error {
	currentTime := time.Now()
	uniqueTokenKey := fmt.Sprintf("%d:%d", tokens, currentTime.UnixNano())
	
	pipe := rl.redis.Pipeline()
	
	// Add new token usage
	pipe.ZAdd(ctx, rl.redisKey, &redis.Z{
		Score:  float64(currentTime.Unix()),
		Member: uniqueTokenKey,
	})
	
	// Clean up old entries
	cutoff := currentTime.Unix() - int64(rl.windowSeconds)
	pipe.ZRemRangeByScore(ctx, rl.redisKey, "-inf", fmt.Sprintf("%d", cutoff))
	
	// Set expiration on the key (cleanup)
	pipe.Expire(ctx, rl.redisKey, time.Duration(rl.windowSeconds+60)*time.Second)
	
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update usage: %w", err)
	}
	
	return nil
}

// Reset clears all usage data for the instance
func (rl *RateLimiter) Reset(ctx context.Context) error {
	err := rl.redis.Del(ctx, rl.redisKey).Err()
	if err != nil {
		return fmt.Errorf("failed to reset rate limiter: %w", err)
	}
	return nil
}

// GetUsageStats returns detailed usage statistics
func (rl *RateLimiter) GetUsageStats(ctx context.Context) (map[string]interface{}, error) {
	currentTime := time.Now().Unix()
	cutoff := currentTime - int64(rl.windowSeconds)
	
	// Get all entries within the window
	entries, err := rl.redis.ZRangeWithScores(ctx, rl.redisKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get usage stats: %w", err)
	}
	
	// Calculate statistics
	totalTokens := 0
	totalRequests := len(entries)
	timeSlots := make(map[int64]int)
	
	for _, entry := range entries {
		tokenValue := strings.Split(entry.Member.(string), ":")[0]
		tokens, err := strconv.Atoi(tokenValue)
		if err != nil {
			continue
		}
		
		totalTokens += tokens
		timeSlot := int64(entry.Score) / 10 * 10 // Group by 10-second slots
		timeSlots[timeSlot] += tokens
	}
	
	// Calculate utilization
	utilization := float64(totalTokens) / float64(rl.tokensPerMinute) * 100
	
	stats := map[string]interface{}{
		"total_tokens":         totalTokens,
		"total_requests":       totalRequests,
		"tokens_per_minute":    rl.tokensPerMinute,
		"max_input_tokens":     rl.maxInputTokens,
		"utilization_percent":  utilization,
		"window_seconds":       rl.windowSeconds,
		"time_slots":           timeSlots,
		"cutoff_time":          cutoff,
		"current_time":         currentTime,
	}
	
	return stats, nil
}

// SetLimits updates the rate limiting parameters
func (rl *RateLimiter) SetLimits(tokensPerMinute, maxInputTokens int) {
	rl.tokensPerMinute = tokensPerMinute
	rl.maxInputTokens = maxInputTokens
}

// GetLimits returns current rate limiting parameters
func (rl *RateLimiter) GetLimits() (int, int) {
	return rl.tokensPerMinute, rl.maxInputTokens
}

// Close closes the Redis connection
func (rl *RateLimiter) Close() error {
	return rl.redis.Close()
}

// IsRateLimited checks if the instance is currently rate limited
func (rl *RateLimiter) IsRateLimited(ctx context.Context, tokens int) (bool, error) {
	hasCapacity, _, err := rl.CheckCapacity(ctx, tokens)
	if err != nil {
		return false, err
	}
	return !hasCapacity, nil
}

// GetTimeUntilAvailable returns seconds until the requested tokens become available
func (rl *RateLimiter) GetTimeUntilAvailable(ctx context.Context, tokens int) (int, error) {
	hasCapacity, retryAfter, err := rl.CheckCapacity(ctx, tokens)
	if err != nil {
		return 0, err
	}
	
	if hasCapacity {
		return 0, nil
	}
	
	return retryAfter, nil
}