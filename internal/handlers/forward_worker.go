package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"flowhook/internal/db"
	"flowhook/internal/models"
	"flowhook/internal/transform"

	"github.com/google/uuid"
)

// triggerForwarding checks for forwarding rules and triggers forwarding
func triggerForwarding(endpointID, requestID uuid.UUID, method, headersJSON string, body []byte) {
	ctx := context.Background()

	// Fetch enabled forwarding rules for this endpoint
	rows, err := db.Pool.Query(
		ctx,
		`SELECT id, endpoint_id, target_url, method, headers, max_retries, backoff_config, condition_type, condition_config
		 FROM forwarding_rules WHERE endpoint_id = $1 AND enabled = TRUE`,
		endpointID,
	)

	if err != nil {
		fmt.Printf("Failed to fetch forwarding rules: %v\n", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var rule models.ForwardingRule
		var headersJSONStr, backoffJSON string
		var conditionConfigJSON []byte
		var ruleMethod, conditionType *string

		err := rows.Scan(
			&rule.ID,
			&rule.EndpointID,
			&rule.TargetURL,
			&ruleMethod,
			&headersJSONStr,
			&rule.MaxRetries,
			&backoffJSON,
			&conditionType,
			&conditionConfigJSON,
		)
		if err != nil {
			fmt.Printf("Failed to scan forwarding rule: %v\n", err)
			continue
		}

		rule.Method = ruleMethod
		rule.ConditionType = conditionType
		json.Unmarshal([]byte(headersJSONStr), &rule.Headers)
		json.Unmarshal([]byte(backoffJSON), &rule.BackoffConfig)
		if len(conditionConfigJSON) > 0 {
			json.Unmarshal(conditionConfigJSON, &rule.ConditionConfig)
		}

		// Check condition if specified
		if rule.ConditionType != nil {
			if !checkForwardingCondition(*rule.ConditionType, rule.ConditionConfig, headersJSON, body) {
				continue // Skip this rule if condition doesn't match
			}
		}

		// Forward asynchronously
		go forwardRequest(ctx, requestID, rule, method, headersJSON, body)
	}
}

// checkForwardingCondition checks if forwarding condition is met
func checkForwardingCondition(conditionType string, conditionConfig map[string]interface{}, headersJSON string, body []byte) bool {
	switch conditionType {
	case "always":
		return true
	case "header_match":
		headerName, ok1 := conditionConfig["header"].(string)
		headerValue, ok2 := conditionConfig["value"].(string)
		if !ok1 || !ok2 {
			return false
		}

		var headers map[string]interface{}
		json.Unmarshal([]byte(headersJSON), &headers)
		if val, exists := headers[headerName]; exists {
			if strVal, ok := val.(string); ok {
				return strVal == headerValue
			}
			if arrVal, ok := val.([]interface{}); ok && len(arrVal) > 0 {
				return fmt.Sprintf("%v", arrVal[0]) == headerValue
			}
		}
		return false
	case "body_match":
		pattern, ok := conditionConfig["pattern"].(string)
		if !ok {
			return false
		}
		// Simple substring match for now
		return bytes.Contains(body, []byte(pattern))
	default:
		return true
	}
}

// forwardRequest performs the forwarding with retry logic
func forwardRequest(ctx context.Context, requestID uuid.UUID, rule models.ForwardingRule, originalMethod, headersJSON string, body []byte) {
	// Determine method
	forwardMethod := originalMethod
	if rule.Method != nil && *rule.Method != "" {
		forwardMethod = *rule.Method
	}

	// Parse original headers
	var originalHeaders map[string]interface{}
	json.Unmarshal([]byte(headersJSON), &originalHeaders)

	// Parse body for transformation
	var bodyData interface{}
	if len(body) > 0 {
		// Try to parse as JSON
		if err := json.Unmarshal(body, &bodyData); err != nil {
			// If not JSON, treat as string
			bodyData = string(body)
		}
	}

	// Apply transformations to request data
	transformedHeaders, transformedBody, err := transform.ApplyRequestTransformations(ctx, rule.EndpointID, originalHeaders, bodyData)
	if err != nil {
		fmt.Printf("Warning: Failed to apply transformations: %v\n", err)
		// Continue with original data if transformation fails
		transformedHeaders = originalHeaders
		transformedBody = bodyData
	}

	// Merge transformed headers with rule headers
	forwardHeaders := make(map[string]interface{})
	for k, v := range transformedHeaders {
		forwardHeaders[k] = v
	}
	for k, v := range rule.Headers {
		forwardHeaders[k] = v
	}

	// Convert transformed body back to bytes
	var forwardBody []byte
	if transformedBody != nil {
		if bodyStr, ok := transformedBody.(string); ok {
			forwardBody = []byte(bodyStr)
		} else {
			// Marshal to JSON
			if bodyBytes, err := json.Marshal(transformedBody); err == nil {
				forwardBody = bodyBytes
			} else {
				// Fallback to original body
				forwardBody = body
			}
		}
	} else {
		forwardBody = body
	}

	// Retry loop
	maxRetries := rule.MaxRetries
	if maxRetries < 1 {
		maxRetries = 1
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		success := executeForward(ctx, requestID, rule.ID, attempt, rule.TargetURL, forwardMethod, forwardHeaders, forwardBody)

		if success {
			return // Success, stop retrying
		}

		// Calculate backoff delay
		if attempt < maxRetries {
			delay := calculateBackoff(attempt, rule.BackoffConfig)
			time.Sleep(delay)
		}
	}
}

// executeForward performs a single forward attempt
func executeForward(ctx context.Context, requestID, ruleID uuid.UUID, attemptNumber int, targetURL, method string, headers map[string]interface{}, body []byte) bool {
	startTime := time.Now()

	// Create HTTP request
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		errMsg := err.Error()
		recordForwardAttempt(requestID, ruleID, attemptNumber, "failed", 0, nil, nil, &errMsg, nil)
		return false
	}

	// Set headers
	for key, value := range headers {
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
		duration := int(time.Since(startTime).Milliseconds())
		errMsg := err.Error()
		recordForwardAttempt(requestID, ruleID, attemptNumber, "failed", 0, nil, nil, &errMsg, &duration)
		return false
	}
	defer resp.Body.Close()

	// Read response body
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // Limit to 1MB
	duration := int(time.Since(startTime).Milliseconds())

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

	// Handle response body
	var respBodyStr *string
	if len(respBody) > 0 {
		if utf8.Valid(respBody) {
			bodyStr := string(respBody)
			respBodyStr = &bodyStr
		} else {
			encoded := base64.StdEncoding.EncodeToString(respBody)
			bodyStr := fmt.Sprintf("[BINARY DATA - Base64 Encoded]\n%s", encoded)
			respBodyStr = &bodyStr
		}
	}

	status := "success"
	if resp.StatusCode >= 400 {
		status = "failed"
	}

	recordForwardAttempt(requestID, ruleID, attemptNumber, status, resp.StatusCode, respHeadersJSON, respBodyStr, nil, &duration)

	return status == "success"
}

// recordForwardAttempt records a forward attempt in the database
func recordForwardAttempt(requestID, ruleID uuid.UUID, attemptNumber int, status string, responseStatus int, responseHeaders []byte, responseBody *string, errorMsg *string, durationMs *int) {
	ctx := context.Background()

	_, err := db.Pool.Exec(
		ctx,
		`INSERT INTO forward_attempts (request_id, forwarding_rule_id, attempt_number, status, response_status, response_headers, response_body, error_message, duration_ms)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		requestID,
		ruleID,
		attemptNumber,
		status,
		responseStatus,
		responseHeaders,
		responseBody,
		errorMsg,
		durationMs,
	)

	if err != nil {
		fmt.Printf("Failed to record forward attempt: %v\n", err)
	}
}

// calculateBackoff calculates the delay for retry based on backoff config
func calculateBackoff(attempt int, config map[string]interface{}) time.Duration {
	backoffType, _ := config["type"].(string)
	base, _ := config["base"].(float64)
	minMs, _ := config["min_ms"].(float64)
	maxMs, _ := config["max_ms"].(float64)

	if base == 0 {
		base = 2
	}
	if minMs == 0 {
		minMs = 1000
	}
	if maxMs == 0 {
		maxMs = 30000
	}

	var delayMs float64
	switch backoffType {
	case "exponential":
		delayMs = minMs * (base * float64(attempt-1))
	case "linear":
		delayMs = minMs * float64(attempt)
	default:
		delayMs = minMs
	}

	if delayMs > maxMs {
		delayMs = maxMs
	}
	if delayMs < minMs {
		delayMs = minMs
	}

	return time.Duration(delayMs) * time.Millisecond
}
