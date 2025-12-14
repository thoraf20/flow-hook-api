package handlers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"flowhook/internal/db"

	"github.com/google/uuid"
)

// Rate limiter using sliding window
type rateLimiter struct {
	requests map[string][]time.Time
	mu       sync.RWMutex
}

var globalRateLimiter = &rateLimiter{
	requests: make(map[string][]time.Time),
}

// CheckRateLimit checks if request should be allowed based on rate limits
func CheckRateLimit(ctx context.Context, endpointID uuid.UUID) (bool, error) {
	// Get endpoint settings
	var rateLimitPerMin, rateLimitPerHour, rateLimitPerDay *int
	err := db.Pool.QueryRow(
		ctx,
		`SELECT rate_limit_per_minute, rate_limit_per_hour, rate_limit_per_day 
		 FROM endpoint_settings WHERE endpoint_id = $1`,
		endpointID,
	).Scan(&rateLimitPerMin, &rateLimitPerHour, &rateLimitPerDay)

	if err != nil {
		// No rate limits configured
		return true, nil
	}

	key := endpointID.String()
	now := time.Now()

	globalRateLimiter.mu.Lock()
	defer globalRateLimiter.mu.Unlock()

	// Clean old entries
	if requests, exists := globalRateLimiter.requests[key]; exists {
		// Keep only last hour of requests
		cutoff := now.Add(-1 * time.Hour)
		validRequests := []time.Time{}
		for _, t := range requests {
			if t.After(cutoff) {
				validRequests = append(validRequests, t)
			}
		}
		globalRateLimiter.requests[key] = validRequests
	}

	// Check limits
	if rateLimitPerMin != nil {
		oneMinAgo := now.Add(-1 * time.Minute)
		count := 0
		for _, t := range globalRateLimiter.requests[key] {
			if t.After(oneMinAgo) {
				count++
			}
		}
		if count >= *rateLimitPerMin {
			return false, fmt.Errorf("rate limit exceeded: %d requests per minute", *rateLimitPerMin)
		}
	}

	if rateLimitPerHour != nil {
		oneHourAgo := now.Add(-1 * time.Hour)
		count := 0
		for _, t := range globalRateLimiter.requests[key] {
			if t.After(oneHourAgo) {
				count++
			}
		}
		if count >= *rateLimitPerHour {
			return false, fmt.Errorf("rate limit exceeded: %d requests per hour", *rateLimitPerHour)
		}
	}

	if rateLimitPerDay != nil {
		oneDayAgo := now.Add(-24 * time.Hour)
		count := 0
		for _, t := range globalRateLimiter.requests[key] {
			if t.After(oneDayAgo) {
				count++
			}
		}
		if count >= *rateLimitPerDay {
			return false, fmt.Errorf("rate limit exceeded: %d requests per day", *rateLimitPerDay)
		}
	}

	// Record this request
	globalRateLimiter.requests[key] = append(globalRateLimiter.requests[key], now)

	return true, nil
}
