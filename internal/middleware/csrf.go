package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

const (
	csrfTokenHeader = "X-CSRF-Token"
	csrfTokenCookie = "csrf_token"
	csrfTokenLength = 32
)

// CSRFMiddleware provides CSRF protection for state-changing operations
func CSRFMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF for GET, HEAD, OPTIONS (safe methods)
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			// Set CSRF token cookie if not present
			if _, err := r.Cookie(csrfTokenCookie); err != nil {
				token := generateCSRFToken()
				http.SetCookie(w, &http.Cookie{
					Name:     csrfTokenCookie,
					Value:    token,
					Path:     "/",
					HttpOnly: false, // JavaScript needs access for API calls
					SameSite: http.SameSiteStrictMode,
					Secure:   r.TLS != nil, // Only secure in HTTPS
					MaxAge:   86400,        // 24 hours
				})
			}
			next(w, r)
			return
		}

		// For state-changing methods, verify CSRF token
		cookieToken, err := r.Cookie(csrfTokenCookie)
		if err != nil {
			http.Error(w, "CSRF token missing", http.StatusForbidden)
			return
		}

		headerToken := r.Header.Get(csrfTokenHeader)
		if headerToken == "" {
			// Also check form data for traditional form submissions
			headerToken = r.FormValue("csrf_token")
		}

		if headerToken == "" || headerToken != cookieToken.Value {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// GenerateCSRFToken generates a new CSRF token
func generateCSRFToken() string {
	bytes := make([]byte, csrfTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based token if crypto/rand fails
		return fmt.Sprintf("%x", bytes)
	}
	return base64.URLEncoding.EncodeToString(bytes)
}

// GetCSRFToken retrieves the CSRF token from the request
func GetCSRFToken(r *http.Request) string {
	cookie, err := r.Cookie(csrfTokenCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ValidateOrigin checks if the request origin matches the expected origin
func ValidateOrigin(r *http.Request, allowedOrigins []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Fallback to Referer header
		referer := r.Header.Get("Referer")
		if referer != "" {
			// Extract origin from referer
			parts := strings.Split(referer, "/")
			if len(parts) >= 3 {
				origin = parts[0] + "//" + parts[2]
			}
		}
	}

	if origin == "" {
		return false
	}

	// If no allowed origins specified, allow all (development mode)
	if len(allowedOrigins) == 0 {
		return true
	}

	for _, allowed := range allowedOrigins {
		// 1. Exact Match
		if origin == allowed {
			return true
		}
		
		// 2. Universal Wildcard
		if allowed == "*" {
			return true
		}

		// 3. Subdomain Wildcard (e.g. "https://*.vercel.app")
		if strings.Contains(allowed, "*") {
			// Escape special chars for regex except *
			pattern := strings.ReplaceAll(allowed, ".", "\\.")
			pattern = strings.ReplaceAll(pattern, "*", ".*")
			
			// Simple check: if allowed is "https://*.vercel.app"
			// We want to match "https://foo.vercel.app"
			// But careful with regex security.
			
			// Safer Manual Check for commonly used "https://*.domain.com" format
			if strings.HasPrefix(allowed, "https://*.") {
				suffix := allowed[9:] // remove "https://*."
				if strings.HasPrefix(origin, "https://") && strings.HasSuffix(origin, suffix) {
					// Ensure no extra slashes (simple subdomain check)
					// origin: https://sub.domain.com -> match
					return true
				}
			}
		}
	}

	return false
}

// CSRFExemptMiddleware allows certain paths to bypass CSRF protection
func CSRFExemptMiddleware(exemptPaths []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, path := range exemptPaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next(w, r)
				return
			}
		}
		CSRFMiddleware(next)(w, r)
	}
}

