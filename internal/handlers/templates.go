package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"flowhook/internal/db"
	"flowhook/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateRequestTemplate handles POST /api/v1/endpoints/:slug/templates
func CreateRequestTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/templates")

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

	var req models.CreateRequestTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Method == "" || req.URL == "" {
		http.Error(w, "name, method, and url are required", http.StatusBadRequest)
		return
	}

	headersJSON, _ := json.Marshal(req.Headers)

	var templateID uuid.UUID
	err = db.Pool.QueryRow(
		r.Context(),
		`INSERT INTO request_templates (endpoint_id, name, method, url, headers, body, description)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		endpointID,
		req.Name,
		req.Method,
		req.URL,
		headersJSON,
		req.Body,
		req.Description,
	).Scan(&templateID)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create template: %v", err), http.StatusInternalServerError)
		return
	}

	template, err := getRequestTemplateByID(r.Context(), templateID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch created template: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(template)
}

// GetRequestTemplates handles GET /api/v1/endpoints/:slug/templates
func GetRequestTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/templates")

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

	rows, err := db.Pool.Query(
		r.Context(),
		`SELECT id, endpoint_id, name, method, url, headers, body, description, created_at, updated_at
		 FROM request_templates WHERE endpoint_id = $1 ORDER BY created_at DESC`,
		endpointID,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var templates []models.RequestTemplate
	for rows.Next() {
		template, err := scanRequestTemplate(rows)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to scan template: %v", err), http.StatusInternalServerError)
			return
		}
		templates = append(templates, template)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(templates)
}

// DeleteRequestTemplate handles DELETE /api/v1/templates/:id
func DeleteRequestTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	templateIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/templates/")
	templateID, err := uuid.Parse(templateIDStr)
	if err != nil {
		http.Error(w, "Invalid template ID", http.StatusBadRequest)
		return
	}

	_, err = db.Pool.Exec(r.Context(), "DELETE FROM request_templates WHERE id = $1", templateID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete template: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SendTemplateRequest handles POST /api/v1/templates/:id/send
func SendTemplateRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	templateIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/templates/")
	templateIDStr = strings.TrimSuffix(templateIDStr, "/send")
	templateID, err := uuid.Parse(templateIDStr)
	if err != nil {
		http.Error(w, "Invalid template ID", http.StatusBadRequest)
		return
	}

	template, err := getRequestTemplateByID(r.Context(), templateID)
	if err != nil {
		http.Error(w, "Template not found", http.StatusNotFound)
		return
	}

	// Send HTTP request using template
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(template.Method, template.URL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	// Add headers
	for k, v := range template.Headers {
		if str, ok := v.(string); ok {
			req.Header.Set(k, str)
		} else if arr, ok := v.([]interface{}); ok {
			for _, val := range arr {
				req.Header.Add(k, fmt.Sprintf("%v", val))
			}
		}
	}

	// Add body if present
	if template.Body != nil && *template.Body != "" {
		req.Body = io.NopCloser(strings.NewReader(*template.Body))
		req.ContentLength = int64(len(*template.Body))
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send request: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Read response
	body, _ := io.ReadAll(resp.Body)

	result := map[string]interface{}{
		"status":      resp.StatusCode,
		"headers":     resp.Header,
		"body":        string(body),
		"template_id": templateID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func getRequestTemplateByID(ctx context.Context, templateID uuid.UUID) (models.RequestTemplate, error) {
	row := db.Pool.QueryRow(
		ctx,
		`SELECT id, endpoint_id, name, method, url, headers, body, description, created_at, updated_at
		 FROM request_templates WHERE id = $1`,
		templateID,
	)
	return scanRequestTemplate(row)
}

func scanRequestTemplate(row interface{ Scan(...interface{}) error }) (models.RequestTemplate, error) {
	var template models.RequestTemplate
	var headersJSON []byte
	var body *string
	var description *string

	err := row.Scan(
		&template.ID,
		&template.EndpointID,
		&template.Name,
		&template.Method,
		&template.URL,
		&headersJSON,
		&body,
		&description,
		&template.CreatedAt,
		&template.UpdatedAt,
	)
	if err != nil {
		return template, err
	}

	json.Unmarshal(headersJSON, &template.Headers)
	template.Body = body
	template.Description = description

	return template, nil
}
