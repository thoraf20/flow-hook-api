package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"flowhook/internal/db"
	"flowhook/internal/models"
	"flowhook/internal/transform"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateTransformation handles POST /api/v1/endpoints/:slug/transformations
func CreateTransformation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/transformations")
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
	var req models.CreateTransformationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Script == "" {
		http.Error(w, "script is required", http.StatusBadRequest)
		return
	}

	// Validate language
	validLanguages := map[string]bool{"jsonata": true, "jq": true, "javascript": true}
	if !validLanguages[req.Language] {
		req.Language = "jsonata" // Default
	}

	// Validate apply_to
	validApplyTo := map[string]bool{"request": true, "response": true, "both": true}
	if !validApplyTo[req.ApplyTo] {
		req.ApplyTo = "request" // Default
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// Insert transformation
	var transformID uuid.UUID
	err = db.Pool.QueryRow(
		r.Context(),
		`INSERT INTO transformations (endpoint_id, name, language, script, apply_to, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`,
		endpointID,
		req.Name,
		req.Language,
		req.Script,
		req.ApplyTo,
		enabled,
	).Scan(&transformID)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create transformation: %v", err), http.StatusInternalServerError)
		return
	}

	// Fetch created transformation
	transform, err := getTransformationByID(r.Context(), transformID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch created transformation: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transform)
}

// GetTransformations handles GET /api/v1/endpoints/:slug/transformations
func GetTransformations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/transformations")
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

	// Fetch transformations
	rows, err := db.Pool.Query(
		r.Context(),
		`SELECT id, endpoint_id, name, language, script, apply_to, enabled, created_at, updated_at
		 FROM transformations WHERE endpoint_id = $1 ORDER BY created_at DESC`,
		endpointID,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var transformations []models.Transformation
	for rows.Next() {
		var transform models.Transformation
		err := rows.Scan(
			&transform.ID,
			&transform.EndpointID,
			&transform.Name,
			&transform.Language,
			&transform.Script,
			&transform.ApplyTo,
			&transform.Enabled,
			&transform.CreatedAt,
			&transform.UpdatedAt,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to scan transformation: %v", err), http.StatusInternalServerError)
			return
		}
		transformations = append(transformations, transform)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transformations)
}

// UpdateTransformation handles PUT /api/v1/transformations/:id
func UpdateTransformation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract transformation ID
	transformIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/transformations/")
	transformID, err := uuid.Parse(transformIDStr)
	if err != nil {
		http.Error(w, "Invalid transformation ID", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		Name     *string `json:"name,omitempty"`
		Language *string `json:"language,omitempty"`
		Script   *string `json:"script,omitempty"`
		ApplyTo  *string `json:"apply_to,omitempty"`
		Enabled  *bool   `json:"enabled,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Build update query dynamically
	updates := []string{}
	args := []interface{}{}
	argIndex := 1

	if req.Name != nil {
		updates = append(updates, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *req.Name)
		argIndex++
	}
	if req.Language != nil {
		updates = append(updates, fmt.Sprintf("language = $%d", argIndex))
		args = append(args, *req.Language)
		argIndex++
	}
	if req.Script != nil {
		updates = append(updates, fmt.Sprintf("script = $%d", argIndex))
		args = append(args, *req.Script)
		argIndex++
	}
	if req.ApplyTo != nil {
		updates = append(updates, fmt.Sprintf("apply_to = $%d", argIndex))
		args = append(args, *req.ApplyTo)
		argIndex++
	}
	if req.Enabled != nil {
		updates = append(updates, fmt.Sprintf("enabled = $%d", argIndex))
		args = append(args, *req.Enabled)
		argIndex++
	}

	if len(updates) == 0 {
		http.Error(w, "No fields to update", http.StatusBadRequest)
		return
	}

	updates = append(updates, "updated_at = now()")
	args = append(args, transformID)

	query := fmt.Sprintf("UPDATE transformations SET %s WHERE id = $%d", strings.Join(updates, ", "), argIndex)
	_, err = db.Pool.Exec(r.Context(), query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update transformation: %v", err), http.StatusInternalServerError)
		return
	}

	// Fetch updated transformation
	transform, err := getTransformationByID(r.Context(), transformID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch updated transformation: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transform)
}

// DeleteTransformation handles DELETE /api/v1/transformations/:id
func DeleteTransformation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract transformation ID
	transformIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/transformations/")
	transformID, err := uuid.Parse(transformIDStr)
	if err != nil {
		http.Error(w, "Invalid transformation ID", http.StatusBadRequest)
		return
	}

	_, err = db.Pool.Exec(r.Context(), "DELETE FROM transformations WHERE id = $1", transformID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete transformation: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestTransformation handles POST /api/v1/transformations/:id/test
func TestTransformation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract transformation ID
	transformIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/transformations/")
	transformIDStr = strings.TrimSuffix(transformIDStr, "/test")
	transformID, err := uuid.Parse(transformIDStr)
	if err != nil {
		http.Error(w, "Invalid transformation ID", http.StatusBadRequest)
		return
	}

	// Get transformation
	transformation, err := getTransformationByID(r.Context(), transformID)
	if err != nil {
		http.Error(w, "Transformation not found", http.StatusNotFound)
		return
	}

	// Parse test request body
	var testReq struct {
		Input interface{} `json:"input"` // Can be JSON object, array, or string
	}
	if err := json.NewDecoder(r.Body).Decode(&testReq); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Execute the transformation
	output, err := transform.ExecuteTransformation(transformation.Language, transformation.Script, testReq.Input)
	if err != nil {
		result := map[string]interface{}{
			"transformation_id": transformID,
			"language":          transformation.Language,
			"script":            transformation.Script,
			"input":             testReq.Input,
			"error":             err.Error(),
			"success":           false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(result)
		return
	}

	result := map[string]interface{}{
		"transformation_id": transformID,
		"language":          transformation.Language,
		"script":            transformation.Script,
		"input":             testReq.Input,
		"output":            output,
		"success":           true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// Helper functions
func getTransformationByID(ctx context.Context, transformID uuid.UUID) (models.Transformation, error) {
	var transform models.Transformation
	err := db.Pool.QueryRow(
		ctx,
		`SELECT id, endpoint_id, name, language, script, apply_to, enabled, created_at, updated_at
		 FROM transformations WHERE id = $1`,
		transformID,
	).Scan(
		&transform.ID,
		&transform.EndpointID,
		&transform.Name,
		&transform.Language,
		&transform.Script,
		&transform.ApplyTo,
		&transform.Enabled,
		&transform.CreatedAt,
		&transform.UpdatedAt,
	)
	return transform, err
}
