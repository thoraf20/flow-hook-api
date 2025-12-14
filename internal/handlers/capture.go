package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"flowhook/internal/config"
	"flowhook/internal/db"
	"flowhook/internal/models"

	"net"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CaptureHandler handles ANY /e/:slug - captures incoming webhooks
func CaptureHandler(w http.ResponseWriter, r *http.Request) {
	// Extract slug from path: /e/:slug
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/e/"), "/")
	slug := pathParts[0]
	if slug == "" {
		http.Error(w, "Invalid endpoint", http.StatusBadRequest)
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

	// Read request body
	maxBodySize := int64(10 * 1024 * 1024) // 10MB default
	if config.AppConfig != nil {
		maxBodySize = config.AppConfig.MaxBodySize
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	if int64(len(body)) > maxBodySize {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Check rate limit
	allowed, err := CheckRateLimit(r.Context(), endpointID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	if !allowed {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Verify HMAC signature if configured
	verified, err := VerifySignature(endpointID, r, body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Signature verification error: %v", err), http.StatusInternalServerError)
		return
	}
	if !verified {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Convert headers to JSON
	headersJSON, _ := json.Marshal(r.Header)

	// Convert query params to JSON
	queryParamsJSON, _ := json.Marshal(r.URL.Query())

	// Get client IP and clean it for PostgreSQL INET type
	var ip *string
	rawIP := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		rawIP = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}

	// Extract IP address (remove port and brackets for IPv6)
	cleanedIP := cleanIPAddress(rawIP)
	if cleanedIP != "" {
		ip = &cleanedIP
	}

	// Generate request ID
	requestID := uuid.New()

	// Convert body to string for storage (handle both text and binary)
	var bodyStr *string
	if len(body) > 0 {
		// Check if body is valid UTF-8 text
		if utf8.Valid(body) {
			bodyString := string(body)
			bodyStr = &bodyString
		} else {
			// For binary data, encode as base64
			encoded := base64.StdEncoding.EncodeToString(body)
			bodyStr = &encoded
		}
	}

	// Get content type
	contentType := r.Header.Get("Content-Type")
	var contentTypePtr *string
	if contentType != "" {
		contentTypePtr = &contentType
	}

	// Insert request into database with body stored directly
	_, err = db.Pool.Exec(
		r.Context(),
		`INSERT INTO requests (id, endpoint_id, method, path, headers, query_params, ip, body, body_size, content_type)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		requestID,
		endpointID,
		r.Method,
		r.URL.Path,
		string(headersJSON),
		string(queryParamsJSON),
		ip,
		bodyStr,
		len(body),
		contentTypePtr,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save request: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish event for realtime updates
	publishRequestEvent(endpointID, requestID, r.Method)

	// Trigger forwarding asynchronously
	go triggerForwarding(endpointID, requestID, r.Method, string(headersJSON), body)

	// Return success response
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// cleanIPAddress extracts the IP address from various formats and returns it in a format suitable for PostgreSQL INET type
// Handles formats like:
// - "192.168.1.1:8080" -> "192.168.1.1"
// - "[::1]:59698" -> "::1"
// - "::1" -> "::1"
// - "192.168.1.1" -> "192.168.1.1"
func cleanIPAddress(addr string) string {
	// Remove brackets if present (IPv6 with port: [::1]:59698)
	addr = strings.Trim(addr, "[]")

	// Try to parse as IP:port
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		// Successfully split, host contains the IP
		return host
	}

	// If SplitHostPort failed, try parsing as just an IP
	parsedIP := net.ParseIP(addr)
	if parsedIP != nil {
		return parsedIP.String()
	}

	// If all else fails, return empty string (will be stored as NULL)
	return ""
}

// publishRequestEvent publishes a request event for SSE subscribers
func publishRequestEvent(endpointID, requestID uuid.UUID, method string) {
	event := models.Request{
		ID:         requestID,
		EndpointID: endpointID,
		Method:     method,
	}

	// Send to all SSE connections for this endpoint
	broadcastToSSE(endpointID.String(), event)
}
