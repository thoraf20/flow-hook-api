package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"flowhook/internal/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetEndpointSettings handles GET /api/v1/endpoints/:slug/settings
func GetEndpointSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/settings")

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

	var settings struct {
		HMACSecret       *string `json:"hmac_secret,omitempty"`
		HMACAlgorithm    string  `json:"hmac_algorithm"`
		RateLimitPerMin  *int    `json:"rate_limit_per_minute,omitempty"`
		RateLimitPerHour *int    `json:"rate_limit_per_hour,omitempty"`
		RateLimitPerDay  *int    `json:"rate_limit_per_day,omitempty"`
	}

	err = db.Pool.QueryRow(
		r.Context(),
		`SELECT hmac_secret, hmac_algorithm, rate_limit_per_minute, rate_limit_per_hour, rate_limit_per_day
		 FROM endpoint_settings WHERE endpoint_id = $1`,
		endpointID,
	).Scan(
		&settings.HMACSecret,
		&settings.HMACAlgorithm,
		&settings.RateLimitPerMin,
		&settings.RateLimitPerHour,
		&settings.RateLimitPerDay,
	)

	if err == pgx.ErrNoRows {
		// Return defaults
		settings.HMACAlgorithm = "sha256"
	} else if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Don't return secret value, just indicate if it's set
	if settings.HMACSecret != nil && *settings.HMACSecret != "" {
		secretSet := "***"
		settings.HMACSecret = &secretSet
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// UpdateEndpointSettings handles PUT /api/v1/endpoints/:slug/settings
func UpdateEndpointSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/settings")

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

	var req struct {
		HMACSecret       *string `json:"hmac_secret,omitempty"`
		HMACAlgorithm    *string `json:"hmac_algorithm,omitempty"`
		RateLimitPerMin  *int    `json:"rate_limit_per_minute,omitempty"`
		RateLimitPerHour *int    `json:"rate_limit_per_hour,omitempty"`
		RateLimitPerDay  *int    `json:"rate_limit_per_day,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Upsert settings
	_, err = db.Pool.Exec(
		r.Context(),
		`INSERT INTO endpoint_settings (endpoint_id, hmac_secret, hmac_algorithm, rate_limit_per_minute, rate_limit_per_hour, rate_limit_per_day, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, now())
		 ON CONFLICT (endpoint_id) 
		 DO UPDATE SET 
		   hmac_secret = COALESCE($2, endpoint_settings.hmac_secret),
		   hmac_algorithm = COALESCE($3, endpoint_settings.hmac_algorithm),
		   rate_limit_per_minute = COALESCE($4, endpoint_settings.rate_limit_per_minute),
		   rate_limit_per_hour = COALESCE($5, endpoint_settings.rate_limit_per_hour),
		   rate_limit_per_day = COALESCE($6, endpoint_settings.rate_limit_per_day),
		   updated_at = now()`,
		endpointID,
		req.HMACSecret,
		req.HMACAlgorithm,
		req.RateLimitPerMin,
		req.RateLimitPerHour,
		req.RateLimitPerDay,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update settings: %v", err), http.StatusInternalServerError)
		return
	}

	// Return updated settings
	GetEndpointSettings(w, r)
}
