package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"flowhook/internal/db"
	"flowhook/internal/logger"
)

// MetricsResponse represents the metrics data
type MetricsResponse struct {
	Timestamp    time.Time              `json:"timestamp"`
	Uptime       string                 `json:"uptime"`
	System       SystemMetrics          `json:"system"`
	Database     DatabaseMetrics        `json:"database"`
	Endpoints    EndpointMetrics        `json:"endpoints"`
	Requests     RequestMetrics         `json:"requests"`
	Forwarding   ForwardingMetrics      `json:"forwarding"`
}

type SystemMetrics struct {
	GoVersion    string `json:"go_version"`
	NumGoroutine int    `json:"num_goroutines"`
	Memory       struct {
		Alloc      uint64 `json:"alloc_bytes"`
		TotalAlloc uint64 `json:"total_alloc_bytes"`
		Sys        uint64 `json:"sys_bytes"`
		NumGC      uint32 `json:"num_gc"`
	} `json:"memory"`
}

type DatabaseMetrics struct {
	Status           string `json:"status"`
	AcquiredConns    int32  `json:"acquired_connections"`
	IdleConns        int32  `json:"idle_connections"`
	MaxConns         int32  `json:"max_connections"`
	TotalEndpoints   int    `json:"total_endpoints"`
	TotalRequests    int    `json:"total_requests"`
	TotalReplays     int    `json:"total_replays"`
	TotalForwardRules int   `json:"total_forwarding_rules"`
}

type EndpointMetrics struct {
	Total      int `json:"total"`
	Active     int `json:"active"` // endpoints with requests in last 24h
	WithRules  int `json:"with_forwarding_rules"`
	WithTransforms int `json:"with_transformations"`
}

type RequestMetrics struct {
	Total      int `json:"total"`
	Last24h    int `json:"last_24h"`
	LastHour   int `json:"last_hour"`
	ByMethod   map[string]int `json:"by_method"`
}

type ForwardingMetrics struct {
	TotalRules      int `json:"total_rules"`
	EnabledRules    int `json:"enabled_rules"`
	TotalAttempts   int `json:"total_attempts"`
	SuccessAttempts int `json:"success_attempts"`
	FailedAttempts  int `json:"failed_attempts"`
	SuccessRate     float64 `json:"success_rate"`
}

var startTime = time.Now()

// GetMetrics handles GET /api/v1/metrics
func GetMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := MetricsResponse{
		Timestamp: time.Now(),
		Uptime:    time.Since(startTime).String(),
	}

	// System metrics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	metrics.System.GoVersion = runtime.Version()
	metrics.System.NumGoroutine = runtime.NumGoroutine()
	metrics.System.Memory.Alloc = m.Alloc
	metrics.System.Memory.TotalAlloc = m.TotalAlloc
	metrics.System.Memory.Sys = m.Sys
	metrics.System.Memory.NumGC = m.NumGC

	// Database metrics
	if db.Pool != nil {
		stats := db.Pool.Stat()
		metrics.Database.Status = "connected"
		metrics.Database.AcquiredConns = stats.AcquiredConns()
		metrics.Database.IdleConns = stats.IdleConns()
		metrics.Database.MaxConns = stats.MaxConns()

		// Get counts from database
		ctx := r.Context()
		db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM endpoints").Scan(&metrics.Database.TotalEndpoints)
		db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM requests").Scan(&metrics.Database.TotalRequests)
		db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM replays").Scan(&metrics.Database.TotalReplays)
		db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM forwarding_rules").Scan(&metrics.Database.TotalForwardRules)
	} else {
		metrics.Database.Status = "disconnected"
	}

	// Endpoint metrics
	ctx := r.Context()
	var activeEndpoints int
	dayAgo := time.Now().Add(-24 * time.Hour)
	db.Pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT endpoint_id) FROM requests WHERE received_at >= $1`,
		dayAgo,
	).Scan(&activeEndpoints)

	var withRules int
	db.Pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT endpoint_id) FROM forwarding_rules`,
	).Scan(&withRules)

	var withTransforms int
	db.Pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT endpoint_id) FROM transformations`,
	).Scan(&withTransforms)

	metrics.Endpoints.Total = metrics.Database.TotalEndpoints
	metrics.Endpoints.Active = activeEndpoints
	metrics.Endpoints.WithRules = withRules
	metrics.Endpoints.WithTransforms = withTransforms

	// Request metrics
	metrics.Requests.Total = metrics.Database.TotalRequests
	hourAgo := time.Now().Add(-1 * time.Hour)
	db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM requests WHERE received_at >= $1`,
		hourAgo,
	).Scan(&metrics.Requests.LastHour)

	db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM requests WHERE received_at >= $1`,
		dayAgo,
	).Scan(&metrics.Requests.Last24h)

	// Request by method
	rows, err := db.Pool.Query(ctx,
		`SELECT method, COUNT(*) FROM requests GROUP BY method`,
	)
	if err == nil {
		defer rows.Close()
		metrics.Requests.ByMethod = make(map[string]int)
		for rows.Next() {
			var method string
			var count int
			if err := rows.Scan(&method, &count); err == nil {
				metrics.Requests.ByMethod[method] = count
			}
		}
	}

	// Forwarding metrics
	metrics.Forwarding.TotalRules = metrics.Database.TotalForwardRules
	db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM forwarding_rules WHERE enabled = true`,
	).Scan(&metrics.Forwarding.EnabledRules)

	db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM forward_attempts`,
	).Scan(&metrics.Forwarding.TotalAttempts)

	db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM forward_attempts WHERE status = 'success'`,
	).Scan(&metrics.Forwarding.SuccessAttempts)

	db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM forward_attempts WHERE status = 'failed'`,
	).Scan(&metrics.Forwarding.FailedAttempts)

	if metrics.Forwarding.TotalAttempts > 0 {
		metrics.Forwarding.SuccessRate = float64(metrics.Forwarding.SuccessAttempts) / float64(metrics.Forwarding.TotalAttempts) * 100
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		logger.Error("Failed to encode metrics: %v", err)
		http.Error(w, "Failed to encode metrics", http.StatusInternalServerError)
	}
}

// HealthCheck handles GET /health
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"uptime":    time.Since(startTime).String(),
	}

	// Check database connection
	if db.Pool != nil {
		if err := db.Pool.Ping(r.Context()); err != nil {
			status["status"] = "unhealthy"
			status["database"] = "disconnected"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(status)
			return
		}
		status["database"] = "connected"
	} else {
		status["status"] = "unhealthy"
		status["database"] = "not_initialized"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// ReadyCheck handles GET /ready
func ReadyCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ready := true
	checks := map[string]string{}

	// Check database
	if db.Pool != nil {
		if err := db.Pool.Ping(r.Context()); err != nil {
			ready = false
			checks["database"] = fmt.Sprintf("error: %v", err)
		} else {
			checks["database"] = "ok"
		}
	} else {
		ready = false
		checks["database"] = "not_initialized"
	}

	response := map[string]interface{}{
		"ready":    ready,
		"checks":   checks,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if ready {
		json.NewEncoder(w).Encode(response)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(response)
	}
}

