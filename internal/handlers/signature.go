package handlers

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"flowhook/internal/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// VerifySignature verifies HMAC signature for a request
func VerifySignature(endpointID uuid.UUID, r *http.Request, body []byte) (bool, error) {
	// Get endpoint settings
	var secret *string
	var algorithm string
	err := db.Pool.QueryRow(
		r.Context(),
		`SELECT hmac_secret, hmac_algorithm FROM endpoint_settings WHERE endpoint_id = $1`,
		endpointID,
	).Scan(&secret, &algorithm)

	if err == pgx.ErrNoRows {
		// No signature verification configured
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to fetch settings: %w", err)
	}

	if secret == nil || *secret == "" {
		// No secret configured
		return true, nil
	}

	// Get signature from header (common patterns)
	signature := r.Header.Get("X-Signature")
	if signature == "" {
		signature = r.Header.Get("X-Hub-Signature-256") // GitHub
	}
	if signature == "" {
		signature = r.Header.Get("X-Stripe-Signature") // Stripe (needs special handling)
	}
	if signature == "" {
		signature = r.Header.Get("Signature")
	}

	if signature == "" {
		return false, fmt.Errorf("no signature header found")
	}

	// Remove algorithm prefix if present (e.g., "sha256=...")
	signature = strings.TrimPrefix(signature, algorithm+"=")
	signature = strings.TrimPrefix(signature, "sha256=")
	signature = strings.TrimPrefix(signature, "sha1=")
	signature = strings.TrimPrefix(signature, "sha512=")

	// Calculate expected signature
	var expectedSignature string
	switch algorithm {
	case "sha1":
		mac := hmac.New(sha1.New, []byte(*secret))
		mac.Write(body)
		expectedSignature = hex.EncodeToString(mac.Sum(nil))
	case "sha512":
		mac := hmac.New(sha512.New, []byte(*secret))
		mac.Write(body)
		expectedSignature = hex.EncodeToString(mac.Sum(nil))
	default: // sha256
		mac := hmac.New(sha256.New, []byte(*secret))
		mac.Write(body)
		expectedSignature = hex.EncodeToString(mac.Sum(nil))
	}

	// Compare signatures (constant-time comparison)
	return hmac.Equal([]byte(signature), []byte(expectedSignature)), nil
}
