package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"flowhook/internal/db"
	"flowhook/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)


func CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, err := getUserIDFromRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req models.CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Generate API key (64 character hex string)
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		http.Error(w, "Failed to generate key", http.StatusInternalServerError)
		return
	}
	apiKey := "fh_" + hex.EncodeToString(keyBytes)

	// Hash the key for storage
	keyHash, err := bcrypt.GenerateFromPassword([]byte(apiKey), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash key", http.StatusInternalServerError)
		return
	}

	keyID := uuid.New()
	keyPrefix := apiKey[:11] // "fh_" + 8 chars

	// Insert into database
	_, err = db.Pool.Exec(
		r.Context(),
		`INSERT INTO api_keys (id, user_id, name, key_hash, key_prefix, expires_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())`,
		keyID,
		userID,
		req.Name,
		string(keyHash),
		keyPrefix,
		req.ExpiresAt,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create API key: %v", err), http.StatusInternalServerError)
		return
	}

	response := models.CreateAPIKeyResponse{
		ID:        keyID,
		Key:       apiKey, // Only returned once
		KeyPrefix: keyPrefix,
		Name:      req.Name,
		CreatedAt: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func GetAPIKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, err := getUserIDFromRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := db.Pool.Query(
		r.Context(),
		`SELECT id, user_id, name, key_prefix,
			last_used_at, expires_at, created_at
		 FROM api_keys 
		 WHERE user_id = $1 
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var key models.APIKey
		err := rows.Scan(
			&key.ID,
			&key.UserID,
			&key.Name,
			&key.KeyPrefix,
			&key.LastUsedAt,
			&key.ExpiresAt,
			&key.CreatedAt,
		)
		if err != nil {
			continue
		}
		keys = append(keys, key)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

func DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, err := getUserIDFromRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	keyIDStr := strings.TrimPrefix(r.URL.Path, "/api/v1/api-keys/")
	keyID, err := uuid.Parse(keyIDStr)
	if err != nil {
		http.Error(w, "Invalid API key ID", http.StatusBadRequest)
		return
	}

	result, err := db.Pool.Exec(
		r.Context(),
		`DELETE FROM api_keys WHERE id = $1 AND user_id = $2`,
		keyID,
		userID,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		http.Error(w, "API key not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func VerifyAPIKey(ctx context.Context, apiKey string) (uuid.UUID, error) {
	rows, err := db.Pool.Query(
		ctx,
		`SELECT id, user_id, key_hash, expires_at 
		 FROM api_keys 
		 WHERE expires_at IS NULL OR expires_at > NOW()`,
	)
	if err != nil {
		return uuid.Nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var keyID uuid.UUID
		var userID uuid.UUID
		var keyHash string
		var expiresAt *time.Time

		if err := rows.Scan(&keyID, &userID, &keyHash, &expiresAt); err != nil {
			continue
		}

		if err := bcrypt.CompareHashAndPassword([]byte(keyHash), []byte(apiKey)); err == nil {
			db.Pool.Exec(ctx, `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, keyID)
			return userID, nil
		}
	}

	return uuid.Nil, fmt.Errorf("invalid API key")
}
