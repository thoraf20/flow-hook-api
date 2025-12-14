package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"flowhook/internal/db"
	"flowhook/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateEndpoint handles POST /api/v1/endpoints
func CreateEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req models.CreateEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Generate unique slug (format: fh_xxxxx)
	slug := generateSlug()

	// Insert endpoint
	var id uuid.UUID
	var name *string
	if req.Name != "" {
		name = &req.Name
	}

	err := db.Pool.QueryRow(
		r.Context(),
		`INSERT INTO endpoints (slug, name) VALUES ($1, $2) RETURNING id`,
		slug, name,
	).Scan(&id)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create endpoint: %v", err), http.StatusInternalServerError)
		return
	}

	// Build endpoint URL
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	endpointURL := fmt.Sprintf("%s/e/%s", baseURL, slug)

	response := models.CreateEndpointResponse{
		ID:   id,
		Slug: slug,
		URL:  endpointURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetEndpoints handles GET /api/v1/endpoints
func GetEndpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Pool.Query(
		r.Context(),
		`SELECT id, slug, name, created_at FROM endpoints ORDER BY created_at DESC`,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch endpoints: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var endpoints []models.Endpoint
	for rows.Next() {
		var ep models.Endpoint
		if err := rows.Scan(&ep.ID, &ep.Slug, &ep.Name, &ep.CreatedAt); err != nil {
			http.Error(w, fmt.Sprintf("Failed to scan endpoint: %v", err), http.StatusInternalServerError)
			return
		}
		endpoints = append(endpoints, ep)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(endpoints)
}

// GetEndpointBySlug handles GET /api/v1/endpoints/:slug
func GetEndpointBySlug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	if slug == "" {
		http.Error(w, "Slug is required", http.StatusBadRequest)
		return
	}

	var ep models.Endpoint
	err := db.Pool.QueryRow(
		r.Context(),
		`SELECT id, slug, name, created_at FROM endpoints WHERE slug = $1`,
		slug,
	).Scan(&ep.ID, &ep.Slug, &ep.Name, &ep.CreatedAt)

	if err == pgx.ErrNoRows {
		http.Error(w, "Endpoint not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch endpoint: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ep)
}

// generateSlug generates a unique slug in format fh_xxxxx
func generateSlug() string {
	uuidStr := uuid.New().String()
	// Remove hyphens and take first 8 chars after "fh_"
	clean := strings.ReplaceAll(uuidStr, "-", "")
	return fmt.Sprintf("fh_%s", clean[:8])
}
