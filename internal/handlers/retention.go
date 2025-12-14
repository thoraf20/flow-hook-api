package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"flowhook/internal/db"
	"flowhook/internal/logger"
	"flowhook/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetRetentionPolicy handles GET /api/v1/endpoints/:slug/retention
func GetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/retention")

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

	var policy models.RetentionPolicy
	err = db.Pool.QueryRow(
		r.Context(),
		`SELECT id, endpoint_id, retention_days, auto_delete, archive_enabled, archive_path, created_at, updated_at
		 FROM retention_policies WHERE endpoint_id = $1`,
		endpointID,
	).Scan(
		&policy.ID,
		&policy.EndpointID,
		&policy.RetentionDays,
		&policy.AutoDelete,
		&policy.ArchiveEnabled,
		&policy.ArchivePath,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		// Return default policy
		policy.EndpointID = endpointID
		policy.RetentionDays = 30
		policy.AutoDelete = false
		policy.ArchiveEnabled = false
	} else if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(policy)
}

// UpdateRetentionPolicy handles PUT /api/v1/endpoints/:slug/retention
func UpdateRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/endpoints/")
	slug = strings.TrimSuffix(slug, "/retention")

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

	var req models.CreateRetentionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Upsert policy
	retentionDays := 30
	if req.RetentionDays != nil {
		retentionDays = *req.RetentionDays
	}

	autoDelete := false
	if req.AutoDelete != nil {
		autoDelete = *req.AutoDelete
	}

	archiveEnabled := false
	if req.ArchiveEnabled != nil {
		archiveEnabled = *req.ArchiveEnabled
	}

	_, err = db.Pool.Exec(
		r.Context(),
		`INSERT INTO retention_policies (endpoint_id, retention_days, auto_delete, archive_enabled, archive_path, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT (endpoint_id) 
		 DO UPDATE SET 
		   retention_days = $2,
		   auto_delete = $3,
		   archive_enabled = $4,
		   archive_path = COALESCE($5, retention_policies.archive_path),
		   updated_at = now()`,
		endpointID,
		retentionDays,
		autoDelete,
		archiveEnabled,
		req.ArchivePath,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update retention policy: %v", err), http.StatusInternalServerError)
		return
	}

	GetRetentionPolicy(w, r)
}

// CleanupOldRequests runs cleanup based on retention policies
func CleanupOldRequests(ctx context.Context) error {
	// Get all retention policies
	rows, err := db.Pool.Query(
		ctx,
		`SELECT endpoint_id, retention_days, auto_delete 
		 FROM retention_policies WHERE auto_delete = true`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var endpointID uuid.UUID
		var retentionDays int
		var autoDelete bool

		if err := rows.Scan(&endpointID, &retentionDays, &autoDelete); err != nil {
			continue
		}

		if !autoDelete {
			continue
		}

		cutoffDate := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

		// Delete old requests
		result, err := db.Pool.Exec(
			ctx,
			`DELETE FROM requests WHERE endpoint_id = $1 AND received_at < $2`,
			endpointID,
			cutoffDate,
		)
		if err != nil {
			logger.Error("Failed to cleanup requests for endpoint %s: %v", endpointID, err)
		} else {
			rowsAffected := result.RowsAffected()
			if rowsAffected > 0 {
				logger.Info("Cleaned up %d old requests for endpoint %s", rowsAffected, endpointID)
			}
		}
	}

	return nil
}

