package transform

import (
	"context"
	"encoding/json"
	"fmt"

	"flowhook/internal/db"
	"flowhook/internal/models"

	"github.com/google/uuid"
)

// ApplyTransformations applies all enabled transformations for an endpoint
// Returns transformed data based on apply_to setting
func ApplyTransformations(ctx context.Context, endpointID uuid.UUID, applyTo string, data interface{}) (interface{}, error) {
	// Fetch enabled transformations for this endpoint
	rows, err := db.Pool.Query(
		ctx,
		`SELECT id, name, language, script, apply_to, enabled
		 FROM transformations 
		 WHERE endpoint_id = $1 AND enabled = TRUE AND apply_to IN ($2, 'both')
		 ORDER BY created_at ASC`,
		endpointID,
		applyTo,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transformations: %w", err)
	}
	defer rows.Close()

	result := data

	for rows.Next() {
		var t models.Transformation
		err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.Language,
			&t.Script,
			&t.ApplyTo,
			&t.Enabled,
		)
		if err != nil {
			continue // Skip this transformation on error
		}

		// Apply transformation
		transformed, err := ExecuteTransformation(t.Language, t.Script, result)
		if err != nil {
			// Log error but continue with other transformations
			fmt.Printf("Transformation %s (%s) failed: %v\n", t.Name, t.ID, err)
			continue
		}

		result = transformed
	}

	return result, nil
}

// ApplyRequestTransformations applies transformations to request data
func ApplyRequestTransformations(ctx context.Context, endpointID uuid.UUID, headers map[string]interface{}, body interface{}) (map[string]interface{}, interface{}, error) {
	// Transform headers if needed
	transformedHeaders, err := ApplyTransformations(ctx, endpointID, "request", headers)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to transform headers: %w", err)
	}

	headersMap, ok := transformedHeaders.(map[string]interface{})
	if !ok {
		// Try to convert
		headersJSON, _ := json.Marshal(transformedHeaders)
		json.Unmarshal(headersJSON, &headersMap)
	}

	// Transform body
	transformedBody, err := ApplyTransformations(ctx, endpointID, "request", body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to transform body: %w", err)
	}

	return headersMap, transformedBody, nil
}

// ApplyResponseTransformations applies transformations to response data
func ApplyResponseTransformations(ctx context.Context, endpointID uuid.UUID, responseBody interface{}) (interface{}, error) {
	return ApplyTransformations(ctx, endpointID, "response", responseBody)
}

