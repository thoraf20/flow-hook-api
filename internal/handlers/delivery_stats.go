package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"flowhook/internal/db"
	"flowhook/internal/logger"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetDeliveryStats handles GET /api/v1/endpoints/:slug/delivery-stats
func GetDeliveryStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/delivery-stats")

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
		logger.Error("Database error: %v", err)
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

	since := time.Now().Add(-time.Duration(hours) * 24 * time.Hour)

	// Get forwarding rules for this endpoint
	rows, err := db.Pool.Query(
		r.Context(),
		`SELECT id, target_url FROM forwarding_rules WHERE endpoint_id = $1 AND enabled = true`,
		endpointID,
	)
	if err != nil {
		logger.Error("Failed to fetch forwarding rules: %v", err)
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type RuleStats struct {
		RuleID     uuid.UUID `json:"rule_id"`
		TargetURL  string    `json:"target_url"`
		Total      int       `json:"total"`
		Successful int       `json:"successful"`
		Failed     int       `json:"failed"`
		SuccessRate float64  `json:"success_rate"`
		AvgDuration *float64 `json:"avg_duration_ms,omitempty"`
	}

	var allStats []RuleStats

	for rows.Next() {
		var ruleID uuid.UUID
		var targetURL string
		if err := rows.Scan(&ruleID, &targetURL); err != nil {
			continue
		}

		// Get stats for this rule
		var stats RuleStats
		err := db.Pool.QueryRow(
			r.Context(),
			`SELECT 
				COUNT(*) as total,
				COUNT(*) FILTER (WHERE status = 'success') as successful,
				COUNT(*) FILTER (WHERE status = 'failed') as failed,
				AVG(duration_ms) as avg_duration
			 FROM forward_attempts 
			 WHERE forwarding_rule_id = $1 AND attempted_at >= $2`,
			ruleID,
			since,
		).Scan(
			&stats.Total,
			&stats.Successful,
			&stats.Failed,
			&stats.AvgDuration,
		)

		if err != nil && err != pgx.ErrNoRows {
			logger.Error("Failed to fetch stats: %v", err)
			continue
		}

		stats.RuleID = ruleID
		stats.TargetURL = targetURL
		if stats.Total > 0 {
			stats.SuccessRate = float64(stats.Successful) / float64(stats.Total) * 100
		}

		allStats = append(allStats, stats)
	}

	// Get hourly breakdown for the first rule (or aggregate)
	var hourlyData []map[string]interface{}
	if len(allStats) > 0 {
		hourlyRows, err := db.Pool.Query(
			r.Context(),
			`SELECT 
				DATE_TRUNC('hour', attempted_at) as hour,
				COUNT(*) as total,
				COUNT(*) FILTER (WHERE status = 'success') as successful,
				COUNT(*) FILTER (WHERE status = 'failed') as failed
			 FROM forward_attempts 
			 WHERE forwarding_rule_id = $1 AND attempted_at >= $2
			 GROUP BY hour
			 ORDER BY hour`,
			allStats[0].RuleID,
			since,
		)
		if err == nil {
			defer hourlyRows.Close()
			for hourlyRows.Next() {
				var hour time.Time
				var total, successful, failed int
				if err := hourlyRows.Scan(&hour, &total, &successful, &failed); err == nil {
					hourlyData = append(hourlyData, map[string]interface{}{
						"hour":      hour.Format(time.RFC3339),
						"total":     total,
						"successful": successful,
						"failed":    failed,
					})
				}
			}
		}
	}

	response := map[string]interface{}{
		"time_range_hours": hours,
		"rules":           allStats,
		"hourly_breakdown": hourlyData,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetRuleDeliveryTimeline handles GET /api/v1/forwarding-rules/:id/timeline
func GetRuleDeliveryTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ruleIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/forwarding-rules/")
	ruleIDStr = strings.TrimSuffix(ruleIDStr, "/timeline")
	ruleID, err := uuid.Parse(ruleIDStr)
	if err != nil {
		http.Error(w, "Invalid rule ID", http.StatusBadRequest)
		return
	}

	// Parse limit (default to 100)
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := time.ParseDuration(limitStr + "h"); err == nil {
			limit = int(l.Hours())
		}
	}

	rows, err := db.Pool.Query(
		r.Context(),
		`SELECT 
			id, request_id, attempt_number, status, response_status, 
			error_message, duration_ms, attempted_at
		 FROM forward_attempts 
		 WHERE forwarding_rule_id = $1 
		 ORDER BY attempted_at DESC 
		 LIMIT $2`,
		ruleID,
		limit,
	)

	if err != nil {
		logger.Error("Failed to fetch timeline: %v", err)
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type TimelineEntry struct {
		ID            uuid.UUID  `json:"id"`
		RequestID     uuid.UUID  `json:"request_id"`
		AttemptNumber int        `json:"attempt_number"`
		Status        string     `json:"status"`
		ResponseStatus *int       `json:"response_status,omitempty"`
		ErrorMessage  *string     `json:"error_message,omitempty"`
		DurationMs    *int        `json:"duration_ms,omitempty"`
		AttemptedAt   time.Time   `json:"attempted_at"`
	}

	var timeline []TimelineEntry
	for rows.Next() {
		var entry TimelineEntry
		err := rows.Scan(
			&entry.ID,
			&entry.RequestID,
			&entry.AttemptNumber,
			&entry.Status,
			&entry.ResponseStatus,
			&entry.ErrorMessage,
			&entry.DurationMs,
			&entry.AttemptedAt,
		)
		if err != nil {
			logger.Error("Failed to scan timeline entry: %v", err)
			continue
		}
		timeline = append(timeline, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(timeline)
}

