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

// ExportRequest handles GET /api/v1/requests/:id/export?format=curl|json|httpie|har
func ExportRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract request ID
	requestIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/requests/")
	requestIDStr = strings.TrimSuffix(requestIDStr, "/export")
	requestID, err := uuid.Parse(requestIDStr)
	if err != nil {
		http.Error(w, "Invalid request ID", http.StatusBadRequest)
		return
	}

	// Get format
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "curl" // Default
	}

	// Fetch request
	var req struct {
		Method      string
		Path        *string
		Headers     string
		QueryParams string
		Body        *string
		ContentType *string
	}

	err = db.Pool.QueryRow(
		r.Context(),
		`SELECT method, path, headers, query_params, body, content_type
		 FROM requests WHERE id = $1`,
		requestID,
	).Scan(
		&req.Method,
		&req.Path,
		&req.Headers,
		&req.QueryParams,
		&req.Body,
		&req.ContentType,
	)

	if err == pgx.ErrNoRows {
		http.Error(w, "Request not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse headers and query params
	var headers map[string]interface{}
	var queryParams map[string]interface{}
	json.Unmarshal([]byte(req.Headers), &headers)
	json.Unmarshal([]byte(req.QueryParams), &queryParams)

	// Get body from database
	var body string
	if req.Body != nil && *req.Body != "" {
		body = *req.Body
	}

	// Build URL (we'll use a placeholder since we don't have the original endpoint URL)
	url := "https://example.com" + func() string {
		if req.Path != nil {
			return *req.Path
		}
		return "/"
	}()

	// Add query params to URL
	if len(queryParams) > 0 {
		url += "?"
		first := true
		for k, v := range queryParams {
			if !first {
				url += "&"
			}
			if str, ok := v.(string); ok {
				url += fmt.Sprintf("%s=%s", k, str)
			} else {
				url += fmt.Sprintf("%s=%v", k, v)
			}
			first = false
		}
	}

	// Generate export based on format
	var exportContent string
	var contentType string
	var filename string

	switch format {
	case "json":
		exportData := map[string]interface{}{
			"method":  req.Method,
			"url":     url,
			"headers": headers,
			"body":    body,
		}
		jsonBytes, _ := json.MarshalIndent(exportData, "", "  ")
		exportContent = string(jsonBytes)
		contentType = "application/json"
		filename = fmt.Sprintf("request-%s.json", requestID.String()[:8])

	case "httpie":
		exportContent = fmt.Sprintf("%s %s\n", req.Method, url)
		for k, v := range headers {
			if arr, ok := v.([]interface{}); ok {
				for _, val := range arr {
					exportContent += fmt.Sprintf("%s: %v\n", k, val)
				}
			} else {
				exportContent += fmt.Sprintf("%s: %v\n", k, v)
			}
		}
		if body != "" {
			exportContent += "\n" + body
		}
		contentType = "text/plain"
		filename = fmt.Sprintf("request-%s.http", requestID.String()[:8])

	case "har":
		har := generateHAR(req.Method, url, headers, queryParams, body)
		jsonBytes, _ := json.MarshalIndent(har, "", "  ")
		exportContent = string(jsonBytes)
		contentType = "application/json"
		filename = fmt.Sprintf("request-%s.har", requestID.String()[:8])

	default: // curl
		exportContent = generateCurl(req.Method, url, headers, body)
		contentType = "text/plain"
		filename = fmt.Sprintf("request-%s.sh", requestID.String()[:8])
	}

	// Set headers for download
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.Write([]byte(exportContent))
}

// generateCurl generates a cURL command
func generateCurl(method, url string, headers map[string]interface{}, body string) string {
	curl := fmt.Sprintf("curl -X %s \"%s\"", method, url)

	// Add headers
	for k, v := range headers {
		if arr, ok := v.([]interface{}); ok {
			for _, val := range arr {
				curl += fmt.Sprintf(" \\\n  -H \"%s: %v\"", k, val)
			}
		} else {
			curl += fmt.Sprintf(" \\\n  -H \"%s: %v\"", k, v)
		}
	}

	// Add body
	if body != "" {
		// Escape single quotes for shell
		escapedBody := strings.ReplaceAll(body, "'", "'\\''")
		curl += fmt.Sprintf(" \\\n  -d '%s'", escapedBody)
	}

	return curl
}

// generateHAR generates a HAR (HTTP Archive) format
func generateHAR(method, url string, headers map[string]interface{}, queryParams map[string]interface{}, body string) map[string]interface{} {
	harHeaders := []map[string]string{}
	for k, v := range headers {
		if arr, ok := v.([]interface{}); ok {
			for _, val := range arr {
				harHeaders = append(harHeaders, map[string]string{
					"name":  k,
					"value": fmt.Sprintf("%v", val),
				})
			}
		} else {
			harHeaders = append(harHeaders, map[string]string{
				"name":  k,
				"value": fmt.Sprintf("%v", v),
			})
		}
	}

	postData := map[string]interface{}{}
	if body != "" {
		postData["mimeType"] = "application/json"
		postData["text"] = body
	}

	return map[string]interface{}{
		"log": map[string]interface{}{
			"version": "1.2",
			"creator": map[string]string{
				"name":    "FlowHook",
				"version": "1.0",
			},
			"entries": []map[string]interface{}{
				{
					"request": map[string]interface{}{
						"method":      method,
						"url":         url,
						"httpVersion": "HTTP/1.1",
						"headers":     harHeaders,
						"queryString": func() []map[string]string {
							result := []map[string]string{}
							for k, v := range queryParams {
								result = append(result, map[string]string{
									"name":  k,
									"value": fmt.Sprintf("%v", v),
								})
							}
							return result
						}(),
						"postData": postData,
					},
					"startedDateTime": time.Now().Format(time.RFC3339),
					"time":            0,
				},
			},
		},
	}
}
