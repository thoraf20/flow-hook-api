package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"flowhook/internal/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetAnalytics handles GET /api/v1/endpoints/:slug/analytics
func GetAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/analytics")
	if slug == "" {
		http.Error(w, "Slug is required", http.StatusBadRequest)
		return
	}

	// Get endpoint ID
	var endpointID uuid.UUID
	err := db.Pool.QueryRow(
		r.Context(),
		`SELECT id FROM endpoints WHERE slug = $1`,
		slug,
	).Scan(&endpointID)

	if err == pgx.ErrNoRows {
		http.Error(w, "Endpoint not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse time range (default to last 24 hours)
	hours := 24
	if hoursStr := r.URL.Query().Get("hours"); hoursStr != "" {
		if h, err := time.ParseDuration(hoursStr + "h"); err == nil {
			hours = int(h.Hours())
		}
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	// Get total requests
	var totalRequests int
	err = db.Pool.QueryRow(
		r.Context(),
		`SELECT COUNT(*) FROM requests WHERE endpoint_id = $1`,
		endpointID,
	).Scan(&totalRequests)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Get requests in time range
	var recentRequests int
	err = db.Pool.QueryRow(
		r.Context(),
		`SELECT COUNT(*) FROM requests WHERE endpoint_id = $1 AND received_at >= $2`,
		endpointID,
		since,
	).Scan(&recentRequests)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Get method distribution
	type MethodCount struct {
		Method string `json:"method"`
		Count  int    `json:"count"`
	}
	methodRows, err := db.Pool.Query(
		r.Context(),
		`SELECT method, COUNT(*) as count 
		 FROM requests 
		 WHERE endpoint_id = $1 AND received_at >= $2
		 GROUP BY method 
		 ORDER BY count DESC`,
		endpointID,
		since,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer methodRows.Close()

	methodDistribution := []MethodCount{}
	for methodRows.Next() {
		var mc MethodCount
		methodRows.Scan(&mc.Method, &mc.Count)
		methodDistribution = append(methodDistribution, mc)
	}

	// Get average request size
	var avgSize *float64
	err = db.Pool.QueryRow(
		r.Context(),
		`SELECT AVG(body_size) FROM requests WHERE endpoint_id = $1 AND received_at >= $2`,
		endpointID,
		since,
	).Scan(&avgSize)
	if err != nil && err != pgx.ErrNoRows {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Get requests per hour (last 24 hours)
	type HourlyCount struct {
		Hour  string `json:"hour"`
		Count int    `json:"count"`
	}
	hourlyRows, err := db.Pool.Query(
		r.Context(),
		`SELECT 
			TO_CHAR(DATE_TRUNC('hour', received_at), 'YYYY-MM-DD HH24:00') as hour,
			COUNT(*) as count
		 FROM requests 
		 WHERE endpoint_id = $1 AND received_at >= $2
		 GROUP BY hour
		 ORDER BY hour`,
		endpointID,
		since,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer hourlyRows.Close()

	hourlyData := []HourlyCount{}
	for hourlyRows.Next() {
		var hc HourlyCount
		hourlyRows.Scan(&hc.Hour, &hc.Count)
		hourlyData = append(hourlyData, hc)
	}

	// Build response
	analytics := map[string]interface{}{
		"total_requests":      totalRequests,
		"recent_requests":     recentRequests,
		"time_range_hours":    hours,
		"method_distribution": methodDistribution,
		"average_size_bytes":  avgSize,
		"hourly_requests":     hourlyData,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(analytics)
}
