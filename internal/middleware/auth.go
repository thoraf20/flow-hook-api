package middleware

import (
	"context"
	"net/http"
	"strings"

	"flowhook/internal/handlers"

	"github.com/google/uuid"
)

// AuthMiddleware authenticates requests using either session token or API key
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var userID uuid.UUID
		var err error

		// Try API key first (for programmatic access)
		apiKey := getAPIKeyFromRequest(r)
		if apiKey != "" {
			userID, err = handlers.VerifyAPIKey(r.Context(), apiKey)
			if err == nil {
				// Add user ID to context
				ctx := context.WithValue(r.Context(), "user_id", userID)
				next(w, r.WithContext(ctx))
				return
			}
		}

		// Fall back to session token
		userID, err = handlers.GetUserIDFromRequest(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Add user ID to context
		ctx := context.WithValue(r.Context(), "user_id", userID)
		next(w, r.WithContext(ctx))
	}
}

func getAPIKeyFromRequest(r *http.Request) string {
	// Check Authorization header: "Bearer fh_..."
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if strings.HasPrefix(token, "fh_") {
			return token
		}
	}

	// Check X-API-Key header
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return apiKey
	}

	return ""
}

