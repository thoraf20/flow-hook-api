package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"flowhook/internal/db"
	"flowhook/internal/models"
	// "flowhook/internal/validation"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateForwardingRule handles POST /api/v1/endpoints/:slug/forwarding-rules
func CreateForwardingRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/forwarding-rules")
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

	// Parse request body
	var req models.CreateForwardingRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.TargetURL == "" {
		http.Error(w, "target_url is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	maxRetries := 3
	if req.MaxRetries != nil {
		maxRetries = *req.MaxRetries
	}

	backoffConfig := map[string]interface{}{
		"type":    "exponential",
		"base":    2,
		"min_ms":  1000,
		"max_ms":  30000,
	}
	if req.BackoffConfig != nil {
		for k, v := range req.BackoffConfig {
			backoffConfig[k] = v
		}
	}

	headersJSON, _ := json.Marshal(req.Headers)
	backoffJSON, _ := json.Marshal(backoffConfig)
	var conditionConfigJSON []byte
	if req.ConditionConfig != nil {
		conditionConfigJSON, _ = json.Marshal(req.ConditionConfig)
	}

	// Insert forwarding rule
	var ruleID uuid.UUID
	err = db.Pool.QueryRow(
		r.Context(),
		`INSERT INTO forwarding_rules (endpoint_id, target_url, method, headers, max_retries, backoff_config, condition_type, condition_config)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id`,
		endpointID,
		req.TargetURL,
		req.Method,
		string(headersJSON),
		maxRetries,
		string(backoffJSON),
		req.ConditionType,
		conditionConfigJSON,
	).Scan(&ruleID)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create forwarding rule: %v", err), http.StatusInternalServerError)
		return
	}

	// Fetch created rule
	rule, err := getForwardingRuleByID(r.Context(), ruleID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch created rule: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule)
}

// GetForwardingRules handles GET /api/v1/endpoints/:slug/forwarding-rules
func GetForwardingRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/forwarding-rules")
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

	// Fetch forwarding rules
	rows, err := db.Pool.Query(
		r.Context(),
		`SELECT id, endpoint_id, target_url, method, headers, enabled, max_retries, backoff_config, condition_type, condition_config, created_at, updated_at
		 FROM forwarding_rules WHERE endpoint_id = $1 ORDER BY created_at DESC`,
		endpointID,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rules []models.ForwardingRule
	for rows.Next() {
		rule, err := scanForwardingRule(rows)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to scan rule: %v", err), http.StatusInternalServerError)
			return
		}
		rules = append(rules, rule)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rules)
}

// UpdateForwardingRule handles PUT /api/v1/forwarding-rules/:id
func UpdateForwardingRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract rule ID
	ruleIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/forwarding-rules/")
	ruleID, err := uuid.Parse(ruleIDStr)
	if err != nil {
		http.Error(w, "Invalid rule ID", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		TargetURL      *string                `json:"target_url,omitempty"`
		Method         *string                 `json:"method,omitempty"`
		Headers        map[string]interface{} `json:"headers,omitempty"`
		Enabled        *bool                  `json:"enabled,omitempty"`
		MaxRetries     *int                   `json:"max_retries,omitempty"`
		BackoffConfig  map[string]interface{} `json:"backoff_config,omitempty"`
		ConditionType  *string                 `json:"condition_type,omitempty"`
		ConditionConfig map[string]interface{} `json:"condition_config,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Build update query dynamically
	updates := []string{}
	args := []interface{}{}
	argIndex := 1

	if req.TargetURL != nil {
		updates = append(updates, fmt.Sprintf("target_url = $%d", argIndex))
		args = append(args, *req.TargetURL)
		argIndex++
	}
	if req.Method != nil {
		updates = append(updates, fmt.Sprintf("method = $%d", argIndex))
		args = append(args, *req.Method)
		argIndex++
	}
	if req.Headers != nil {
		headersJSON, _ := json.Marshal(req.Headers)
		updates = append(updates, fmt.Sprintf("headers = $%d", argIndex))
		args = append(args, string(headersJSON))
		argIndex++
	}
	if req.Enabled != nil {
		updates = append(updates, fmt.Sprintf("enabled = $%d", argIndex))
		args = append(args, *req.Enabled)
		argIndex++
	}
	if req.MaxRetries != nil {
		updates = append(updates, fmt.Sprintf("max_retries = $%d", argIndex))
		args = append(args, *req.MaxRetries)
		argIndex++
	}
	if req.BackoffConfig != nil {
		backoffJSON, _ := json.Marshal(req.BackoffConfig)
		updates = append(updates, fmt.Sprintf("backoff_config = $%d", argIndex))
		args = append(args, string(backoffJSON))
		argIndex++
	}
	if req.ConditionType != nil {
		updates = append(updates, fmt.Sprintf("condition_type = $%d", argIndex))
		args = append(args, *req.ConditionType)
		argIndex++
	}
	if req.ConditionConfig != nil {
		conditionJSON, _ := json.Marshal(req.ConditionConfig)
		updates = append(updates, fmt.Sprintf("condition_config = $%d", argIndex))
		args = append(args, conditionJSON)
		argIndex++
	}

	if len(updates) == 0 {
		http.Error(w, "No fields to update", http.StatusBadRequest)
		return
	}

	updates = append(updates, "updated_at = now()")
	args = append(args, ruleID)

	query := fmt.Sprintf("UPDATE forwarding_rules SET %s WHERE id = $%d", strings.Join(updates, ", "), argIndex)
	_, err = db.Pool.Exec(r.Context(), query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update rule: %v", err), http.StatusInternalServerError)
		return
	}

	// Fetch updated rule
	rule, err := getForwardingRuleByID(r.Context(), ruleID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch updated rule: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule)
}

// DeleteForwardingRule handles DELETE /api/v1/forwarding-rules/:id
func DeleteForwardingRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract rule ID
	ruleIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/forwarding-rules/")
	ruleID, err := uuid.Parse(ruleIDStr)
	if err != nil {
		http.Error(w, "Invalid rule ID", http.StatusBadRequest)
		return
	}

	_, err = db.Pool.Exec(r.Context(), "DELETE FROM forwarding_rules WHERE id = $1", ruleID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete rule: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetForwardAttempts handles GET /api/v1/requests/:id/forward-attempts
func GetForwardAttempts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract request ID
	requestIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/requests/")
	requestIDStr = strings.TrimSuffix(requestIDStr, "/forward-attempts")
	requestID, err := uuid.Parse(requestIDStr)
	if err != nil {
		http.Error(w, "Invalid request ID", http.StatusBadRequest)
		return
	}

	rows, err := db.Pool.Query(
		r.Context(),
		`SELECT id, request_id, forwarding_rule_id, attempt_number, status, response_status, response_headers, response_body, error_message, duration_ms, attempted_at
		 FROM forward_attempts WHERE request_id = $1 ORDER BY attempted_at DESC`,
		requestID,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var attempts []models.ForwardAttempt
	for rows.Next() {
		var attempt models.ForwardAttempt
		var responseHeadersJSON []byte

		err := rows.Scan(
			&attempt.ID,
			&attempt.RequestID,
			&attempt.ForwardingRuleID,
			&attempt.AttemptNumber,
			&attempt.Status,
			&attempt.ResponseStatus,
			&responseHeadersJSON,
			&attempt.ResponseBody,
			&attempt.ErrorMessage,
			&attempt.DurationMs,
			&attempt.AttemptedAt,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to scan attempt: %v", err), http.StatusInternalServerError)
			return
		}

		if len(responseHeadersJSON) > 0 {
			json.Unmarshal(responseHeadersJSON, &attempt.ResponseHeaders)
		}

		attempts = append(attempts, attempt)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attempts)
}

// Helper functions
func getForwardingRuleByID(ctx context.Context, ruleID uuid.UUID) (models.ForwardingRule, error) {
	row := db.Pool.QueryRow(
		ctx,
		`SELECT id, endpoint_id, target_url, method, headers, enabled, max_retries, backoff_config, condition_type, condition_config, created_at, updated_at
		 FROM forwarding_rules WHERE id = $1`,
		ruleID,
	)
	return scanForwardingRule(row)
}

func scanForwardingRule(scanner interface {
	Scan(dest ...interface{}) error
}) (models.ForwardingRule, error) {
	var rule models.ForwardingRule
	var headersJSON, backoffJSON string
	var conditionConfigJSON []byte
	var method, conditionType *string

	err := scanner.Scan(
		&rule.ID,
		&rule.EndpointID,
		&rule.TargetURL,
		&method,
		&headersJSON,
		&rule.Enabled,
		&rule.MaxRetries,
		&backoffJSON,
		&conditionType,
		&conditionConfigJSON,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	if err != nil {
		return rule, err
	}

	rule.Method = method
	rule.ConditionType = conditionType

	json.Unmarshal([]byte(headersJSON), &rule.Headers)
	json.Unmarshal([]byte(backoffJSON), &rule.BackoffConfig)
	if len(conditionConfigJSON) > 0 {
		json.Unmarshal(conditionConfigJSON, &rule.ConditionConfig)
	}

	return rule, nil
}

