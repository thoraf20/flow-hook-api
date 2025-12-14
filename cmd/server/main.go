package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"flowhook/internal/config"
	"flowhook/internal/db"
	"flowhook/internal/handlers"
	"flowhook/internal/middleware"
)

func main() {
	// Load configuration first (before any config access)
	config.Load()
	
	// Initialize database
	if err := db.Init(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Run migrations
	ctx := context.Background()
	if err := db.RunMigrations(ctx); err != nil {
		log.Printf("Warning: Failed to run migrations: %v", err)
		// Check if tables already exist
		var exists bool
		err = db.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_schema = 'public' 
				AND table_name = 'endpoints'
			)
		`).Scan(&exists)
		if err != nil || !exists {
			log.Fatalf("Database not initialized. Please run migrations manually.")
		}
		log.Println("Tables already exist, skipping migrations")
	}

	// Setup routes
	mux := http.NewServeMux()

	// Apply compression middleware to all routes
	handler := middleware.GzipMiddleware(mux)

	// CORS middleware with origin validation
	corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			var allowedOrigin string
			
			// When credentials are included, we must use a specific origin, not "*"
			if origin != "" {
				// If allowed origins are configured, validate the origin
				if config.AppConfig != nil && len(config.AppConfig.AllowedOrigins) > 0 {
					if middleware.ValidateOrigin(r, config.AppConfig.AllowedOrigins) {
						allowedOrigin = origin
					}
					// If validation fails, allowedOrigin remains empty (no CORS header)
				} else {
					// Development mode: allow any origin when no restrictions configured
					allowedOrigin = origin
				}
			}

			if allowedOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token")
			w.Header().Set("Access-Control-Expose-Headers", "X-CSRF-Token")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	}

	// CSRF protection middleware (only for state-changing operations)
	csrfMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		if config.AppConfig != nil && config.AppConfig.CSRFEnabled {
			// Exempt webhook capture endpoint and health checks
			exemptPaths := []string{"/e/", "/health", "/ready", "/api/v1/metrics"}
			return middleware.CSRFExemptMiddleware(exemptPaths, next)
		}
		return next
	}

	// Health and metrics endpoints
	mux.HandleFunc("/health", handlers.HealthCheck)
	mux.HandleFunc("/ready", handlers.ReadyCheck)
	mux.HandleFunc("/api/v1/metrics", corsMiddleware(handlers.GetMetrics))
	mux.HandleFunc("/api/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./api/openapi.yaml")
	})

	// API routes (with CSRF protection for state-changing operations)
	mux.HandleFunc("/api/v1/endpoints", corsMiddleware(csrfMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handlers.CreateEndpoint(w, r)
		} else if r.Method == http.MethodGet {
			handlers.GetEndpoints(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	mux.HandleFunc("/api/v1/endpoints/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/requests") {
			handlers.GetRequests(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/analytics") {
			handlers.GetAnalytics(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/delivery-stats") {
			handlers.GetDeliveryStats(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/settings") {
			if r.Method == http.MethodGet {
				handlers.GetEndpointSettings(w, r)
			} else if r.Method == http.MethodPut {
				handlers.UpdateEndpointSettings(w, r)
			}
		} else if strings.HasSuffix(r.URL.Path, "/retention") {
			if r.Method == http.MethodGet {
				handlers.GetRetentionPolicy(w, r)
			} else if r.Method == http.MethodPut {
				handlers.UpdateRetentionPolicy(w, r)
			}
		} else if strings.HasSuffix(r.URL.Path, "/templates") {
			if r.Method == http.MethodPost {
				handlers.CreateRequestTemplate(w, r)
			} else if r.Method == http.MethodGet {
				handlers.GetRequestTemplates(w, r)
			}
		} else if strings.HasSuffix(r.URL.Path, "/forwarding-rules") {
			if r.Method == http.MethodPost {
				handlers.CreateForwardingRule(w, r)
			} else if r.Method == http.MethodGet {
				handlers.GetForwardingRules(w, r)
			}
		} else if strings.HasSuffix(r.URL.Path, "/transformations") {
			if r.Method == http.MethodPost {
				handlers.CreateTransformation(w, r)
			} else if r.Method == http.MethodGet {
				handlers.GetTransformations(w, r)
			}
		} else {
			handlers.GetEndpointBySlug(w, r)
		}
	}))

	mux.HandleFunc("/api/v1/templates/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/send") {
			handlers.SendTemplateRequest(w, r)
		} else if r.Method == http.MethodDelete {
			handlers.DeleteRequestTemplate(w, r)
		}
	}))

	mux.HandleFunc("/api/v1/auth/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/register") {
			handlers.Register(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/login") {
			handlers.Login(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/me") {
			handlers.GetCurrentUser(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/logout") {
			handlers.Logout(w, r)
		}
	}))

	mux.HandleFunc("/api/v1/api-keys", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handlers.CreateAPIKey(w, r)
		} else if r.Method == http.MethodGet {
			handlers.GetAPIKeys(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	mux.HandleFunc("/api/v1/api-keys/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			handlers.DeleteAPIKey(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	mux.HandleFunc("/api/v1/requests/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/replay") {
			handlers.ReplayRequest(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/replays") {
			handlers.GetReplays(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/forward-attempts") {
			handlers.GetForwardAttempts(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/export") {
			handlers.ExportRequest(w, r)
		} else {
			handlers.GetRequestDetail(w, r)
		}
	}))

	mux.HandleFunc("/api/v1/forwarding-rules/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/timeline") {
			handlers.GetRuleDeliveryTimeline(w, r)
		} else if r.Method == http.MethodPut {
			handlers.UpdateForwardingRule(w, r)
		} else if r.Method == http.MethodDelete {
			handlers.DeleteForwardingRule(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	mux.HandleFunc("/api/v1/transformations/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/test") {
			handlers.TestTransformation(w, r)
		} else if r.Method == http.MethodPut {
			handlers.UpdateTransformation(w, r)
		} else if r.Method == http.MethodDelete {
			handlers.DeleteTransformation(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	mux.HandleFunc("/api/v1/realtime", corsMiddleware(handlers.RealtimeHandler))

	// Webhook capture endpoint
	mux.HandleFunc("/e/", corsMiddleware(handlers.CaptureHandler))

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		log.Printf("Server starting on port %s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

