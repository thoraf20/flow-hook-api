package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"flowhook/internal/db"
	"flowhook/internal/models"
	"flowhook/internal/transform"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReplayRequest handles POST /api/v1/requests/:id/replay
func ReplayRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract request ID from path
	requestIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/requests/")
	requestIDStr = strings.TrimSuffix(requestIDStr, "/replay")
	requestID, err := uuid.Parse(requestIDStr)
	if err != nil {
		http.Error(w, "Invalid request ID", http.StatusBadRequest)
		return
	}

	// Parse replay request body
	var replayReq models.CreateReplayRequest
	if err := json.NewDecoder(r.Body).Decode(&replayReq); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if replayReq.TargetURL == "" {
		http.Error(w, "target_url is required", http.StatusBadRequest)
		return
	}

	// Fetch original request
	var originalReq models.Request
	var headersJSON, queryParamsJSON string
	var path, ip, bodyStr, contentType *string

	err = db.Pool.QueryRow(
		r.Context(),
		`SELECT id, endpoint_id, method, path, headers, query_params, ip, body, body_size, content_type, received_at
		 FROM requests WHERE id = $1`,
		requestID,
	).Scan(
		&originalReq.ID,
		&originalReq.EndpointID,
		&originalReq.Method,
		&path,
		&headersJSON,
		&queryParamsJSON,
		&ip,
		&bodyStr,
		&originalReq.BodySize,
		&contentType,
		&originalReq.ReceivedAt,
	)

	if err == pgx.ErrNoRows {
		http.Error(w, "Request not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse original headers
	json.Unmarshal([]byte(headersJSON), &originalReq.Headers)

	// Get original body from database
	var originalBody []byte
	if bodyStr != nil && *bodyStr != "" {
		originalBody = []byte(*bodyStr)
	}

	// Determine method, headers, and body for replay
	replayMethod := originalReq.Method
	if replayReq.Method != nil && *replayReq.Method != "" {
		replayMethod = *replayReq.Method
	}

	replayHeaders := make(map[string]interface{})
	// If user provided headers, use them (they can override or start fresh)
	if len(replayReq.Headers) > 0 {
		replayHeaders = replayReq.Headers
	} else {
		// Otherwise, use original headers
		for k, v := range originalReq.Headers {
			replayHeaders[k] = v
		}
	}

	replayBody := string(originalBody)
	if replayReq.Body != nil {
		replayBody = *replayReq.Body
	}

	// Apply transformations to replay data
	var bodyData interface{}
	if replayBody != "" {
		// Try to parse as JSON
		if err := json.Unmarshal([]byte(replayBody), &bodyData); err != nil {
			// If not JSON, treat as string
			bodyData = replayBody
		}
	}

	// Apply transformations
	transformedHeaders, transformedBody, err := transform.ApplyRequestTransformations(r.Context(), originalReq.EndpointID, replayHeaders, bodyData)
	if err != nil {
		// Log but continue - transformations are optional
		fmt.Printf("Warning: Failed to apply transformations during replay: %v\n", err)
		transformedHeaders = replayHeaders
		transformedBody = bodyData
	}

	// Convert transformed body back to string
	var finalBody string
	if transformedBody != nil {
		if bodyStr, ok := transformedBody.(string); ok {
			finalBody = bodyStr
		} else {
			// Marshal to JSON
			if bodyBytes, err := json.Marshal(transformedBody); err == nil {
				finalBody = string(bodyBytes)
			} else {
				finalBody = replayBody
			}
		}
	} else {
		finalBody = replayBody
	}

	// Create replay record
	replayID := uuid.New()
	replayHeadersJSON, _ := json.Marshal(transformedHeaders)

	// Insert replay record
	_, err = db.Pool.Exec(
		r.Context(),
		`INSERT INTO replays (id, request_id, target_url, method, headers, body, status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending')`,
		replayID,
		requestID,
		replayReq.TargetURL,
		replayMethod,
		string(replayHeadersJSON),
		finalBody,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create replay: %v", err), http.StatusInternalServerError)
		return
	}

	// Execute replay asynchronously
	go executeReplay(replayID, replayReq.TargetURL, replayMethod, transformedHeaders, finalBody)

	response := models.CreateReplayResponse{
		ReplayID: replayID,
		Status:   "pending",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// executeReplay performs the actual HTTP request and updates the replay record
func executeReplay(replayID uuid.UUID, targetURL, method string, headers map[string]interface{}, body string) {
	ctx := context.Background()

	// Create HTTP request
	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewReader([]byte(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		errMsg := err.Error()
		updateReplayStatus(replayID, "failed", 0, nil, nil, &errMsg)
		return
	}

	// Set headers
	for key, value := range headers {
		// Handle array values (like Accept: [application/json])
		if arr, ok := value.([]interface{}); ok {
			for _, v := range arr {
				req.Header.Set(key, fmt.Sprintf("%v", v))
			}
		} else {
			req.Header.Set(key, fmt.Sprintf("%v", value))
		}
	}

	// Execute request
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		errMsg := err.Error()
		updateReplayStatus(replayID, "failed", 0, nil, nil, &errMsg)
		return
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // Limit to 1MB
	if err != nil {
		errMsg := fmt.Sprintf("Failed to read response: %v", err)
		updateReplayStatus(replayID, "failed", resp.StatusCode, nil, nil, &errMsg)
		return
	}

	// Convert response headers to JSON
	respHeaders := make(map[string]interface{})
	for k, v := range resp.Header {
		if len(v) == 1 {
			respHeaders[k] = v[0]
		} else {
			respHeaders[k] = v
		}
	}
	respHeadersJSON, _ := json.Marshal(respHeaders)

	// Handle response body - check if it's valid UTF-8
	var respBodyStr *string
	if len(respBody) > 0 {
		// Check if the body is valid UTF-8
		if utf8.Valid(respBody) {
			// Valid UTF-8, store as string
			bodyStr := string(respBody)
			respBodyStr = &bodyStr
		} else {
			// Binary data, encode as base64
			encoded := base64.StdEncoding.EncodeToString(respBody)
			bodyStr := fmt.Sprintf("[BINARY DATA - Base64 Encoded]\n%s", encoded)
			respBodyStr = &bodyStr
		}
	}

	status := "success"
	if resp.StatusCode >= 400 {
		status = "failed"
	}

	updateReplayStatus(replayID, status, resp.StatusCode, respHeadersJSON, respBodyStr, nil)
}

// updateReplayStatus updates the replay record with the result
func updateReplayStatus(replayID uuid.UUID, status string, responseStatus int, responseHeaders []byte, responseBody *string, errorMsg *string) {
	ctx := context.Background()

	query := `UPDATE replays 
			  SET status = $1, attempts = attempts + 1, last_attempt_at = now(),
			      response_status = $2, response_headers = $3, response_body = $4, error_message = $5
			  WHERE id = $6`

	_, err := db.Pool.Exec(
		ctx,
		query,
		status,
		responseStatus,
		responseHeaders,
		responseBody,
		errorMsg,
		replayID,
	)

	if err != nil {
		// Log error but don't fail - this is async
		fmt.Printf("Failed to update replay status: %v\n", err)
	}
}

// GetReplays handles GET /api/v1/requests/:id/replays
func GetReplays(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract request ID from path
	requestIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/requests/")
	requestIDStr = strings.TrimSuffix(requestIDStr, "/replays")
	requestID, err := uuid.Parse(requestIDStr)
	if err != nil {
		http.Error(w, "Invalid request ID", http.StatusBadRequest)
		return
	}

	// Fetch replays for this request
	rows, err := db.Pool.Query(
		r.Context(),
		`SELECT id, request_id, target_url, method, headers, body, attempts, status,
		        response_status, response_headers, response_body, error_message, last_attempt_at, created_at
		 FROM replays WHERE request_id = $1 ORDER BY created_at DESC`,
		requestID,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var replays []models.Replay
	for rows.Next() {
		var replay models.Replay
		var headersJSON string
		var responseHeadersJSON []byte

		err := rows.Scan(
			&replay.ID,
			&replay.RequestID,
			&replay.TargetURL,
			&replay.Method,
			&headersJSON,
			&replay.Body,
			&replay.Attempts,
			&replay.Status,
			&replay.ResponseStatus,
			&responseHeadersJSON,
			&replay.ResponseBody,
			&replay.ErrorMessage,
			&replay.LastAttemptAt,
			&replay.CreatedAt,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to scan replay: %v", err), http.StatusInternalServerError)
			return
		}

		// Parse JSON fields
		json.Unmarshal([]byte(headersJSON), &replay.Headers)
		if len(responseHeadersJSON) > 0 {
			json.Unmarshal(responseHeadersJSON, &replay.ResponseHeaders)
		}

		replays = append(replays, replay)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(replays)
}
