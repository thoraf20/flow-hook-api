package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"flowhook/internal/db"
	"flowhook/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetRequests handles GET /api/v1/endpoints/:slug/requests
func GetRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/requests")
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

	// Parse query parameters
	limit := 25
	offset := 0
	methodFilter := ""
	searchQuery := ""
	dateFrom := ""
	dateTo := ""

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	if methodStr := r.URL.Query().Get("method"); methodStr != "" {
		methodFilter = methodStr
	}

	if searchStr := r.URL.Query().Get("search"); searchStr != "" {
		searchQuery = strings.ToLower(searchStr)
	}

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		dateFrom = fromStr
	}

	if toStr := r.URL.Query().Get("to"); toStr != "" {
		dateTo = toStr
	}

	// Build query
	query := `SELECT id, endpoint_id, method, path, headers, query_params, ip, body, body_size, content_type, received_at
			  FROM requests
			  WHERE endpoint_id = $1`
	args := []interface{}{endpointID}
	argIndex := 2

	if methodFilter != "" {
		query += fmt.Sprintf(" AND method = $%d", argIndex)
		args = append(args, methodFilter)
		argIndex++
	}

	if dateFrom != "" {
		query += fmt.Sprintf(" AND received_at >= $%d", argIndex)
		args = append(args, dateFrom)
		argIndex++
	}

	if dateTo != "" {
		query += fmt.Sprintf(" AND received_at <= $%d", argIndex)
		args = append(args, dateTo)
		argIndex++
	}

	if searchQuery != "" {
		// Search in headers and path (case-insensitive) - using ILIKE for better performance
		query += fmt.Sprintf(" AND (path ILIKE $%d OR headers::text ILIKE $%d)", argIndex, argIndex)
		searchPattern := "%" + searchQuery + "%"
		args = append(args, searchPattern)
		argIndex++
	}

	query += " ORDER BY received_at DESC LIMIT $" + strconv.Itoa(argIndex) + " OFFSET $" + strconv.Itoa(argIndex+1)
	args = append(args, limit, offset)

	// Get total count
	var total int
	countQuery := `SELECT COUNT(*) FROM requests WHERE endpoint_id = $1`
	countArgs := []interface{}{endpointID}
	countArgIndex := 2

	if methodFilter != "" {
		countQuery += fmt.Sprintf(" AND method = $%d", countArgIndex)
		countArgs = append(countArgs, methodFilter)
		countArgIndex++
	}

	if dateFrom != "" {
		countQuery += fmt.Sprintf(" AND received_at >= $%d", countArgIndex)
		countArgs = append(countArgs, dateFrom)
		countArgIndex++
	}

	if dateTo != "" {
		countQuery += fmt.Sprintf(" AND received_at <= $%d", countArgIndex)
		countArgs = append(countArgs, dateTo)
		countArgIndex++
	}

	if searchQuery != "" {
		// Use ILIKE for case-insensitive search (better performance than LOWER)
		countQuery += fmt.Sprintf(" AND (path ILIKE $%d OR headers::text ILIKE $%d)", countArgIndex, countArgIndex)
		searchPattern := "%" + searchQuery + "%"
		countArgs = append(countArgs, searchPattern)
		countArgIndex++
	}

	err = db.Pool.QueryRow(r.Context(), countQuery, countArgs...).Scan(&total)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to count requests: %v", err), http.StatusInternalServerError)
		return
	}

	// Execute query
	rows, err := db.Pool.Query(r.Context(), query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch requests: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var requests []models.Request
	for rows.Next() {
		var req models.Request
		var headersJSON, queryParamsJSON string
		var path, ip, bodyStr, contentType *string

		err := rows.Scan(
			&req.ID,
			&req.EndpointID,
			&req.Method,
			&path,
			&headersJSON,
			&queryParamsJSON,
			&ip,
			&bodyStr,
			&req.BodySize,
			&contentType,
			&req.ReceivedAt,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to scan request: %v", err), http.StatusInternalServerError)
			return
		}

		req.Path = path
		req.IP = ip
		req.Body = bodyStr
		req.ContentType = contentType

		// Parse JSON fields
		json.Unmarshal([]byte(headersJSON), &req.Headers)
		json.Unmarshal([]byte(queryParamsJSON), &req.QueryParams)

		requests = append(requests, req)
	}

	response := models.RequestListResponse{
		Requests: requests,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetRequestDetail handles GET /api/v1/requests/:id
func GetRequestDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract request ID from path
	requestIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/requests/")
	requestID, err := uuid.Parse(requestIDStr)
	if err != nil {
		http.Error(w, "Invalid request ID", http.StatusBadRequest)
		return
	}

	// Fetch request from database
	var req models.Request
	var headersJSON, queryParamsJSON string
	var path, ip, bodyStr, contentType *string

	err = db.Pool.QueryRow(
		r.Context(),
		`SELECT id, endpoint_id, method, path, headers, query_params, ip, body, body_size, content_type, received_at
		 FROM requests WHERE id = $1`,
		requestID,
	).Scan(
		&req.ID,
		&req.EndpointID,
		&req.Method,
		&path,
		&headersJSON,
		&queryParamsJSON,
		&ip,
		&bodyStr,
		&req.BodySize,
		&contentType,
		&req.ReceivedAt,
	)

	if err == pgx.ErrNoRows {
		http.Error(w, "Request not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	req.Path = path
	req.IP = ip
	req.Body = bodyStr
	req.ContentType = contentType

	// Parse JSON fields
	json.Unmarshal([]byte(headersJSON), &req.Headers)
	json.Unmarshal([]byte(queryParamsJSON), &req.QueryParams)

	// Convert body string to bytes for response
	var body []byte
	if bodyStr != nil && *bodyStr != "" {
		body = []byte(*bodyStr)
	}

	// Add body to response
	type RequestDetailResponse struct {
		models.Request
		Body string `json:"body,omitempty"`
	}

	response := RequestDetailResponse{
		Request: req,
		Body:    string(body),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

