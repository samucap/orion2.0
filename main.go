package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/auth"
	"github.com/samucap/orion2.0/internal/cache"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/middleware"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		slog.Warn("No .env file found, using system environment variables")
	}

	// Initialize structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Initialize database connection (optional)
	dbPool, err := db.InitDB()
	if err != nil {
		slog.Warn("Failed to initialize database, continuing without DB features", "error", err)
		dbPool = nil
	} else {
		defer dbPool.Close()
		slog.Info("Database connection established")
		// Cleanup expired token revocations periodically to avoid unbounded growth.
		go func() {
			ticker := time.NewTicker(15 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = db.TokenBlacklist.CleanupExpiredTokens(cleanupCtx)
				cancel()
			}
		}()

		refreshStore := auth.NewPgRefreshTokenStore(dbPool)
		refreshSvc := auth.NewRefreshService(refreshStore)
		handlers.SetRefreshService(refreshSvc)
		slog.Info("Refresh token service initialized")
	}

	// Initialize cache
	// TODO: replace with redis
	eventCache := cache.NewInMemoryCache()
	handlers.SetCache(eventCache)
	slog.Info("Cache initialized")

	// Create router
	r := chi.NewRouter()

	// Middleware stack
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))
	r.Use(securityHeaders)

	// Health check (always public)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Cache management (always public)
	r.Post("/cache/clear", handlers.ClearCache)

	// Auth routes: public, rate-limited to prevent brute-force
	r.Route("/api/auth", func(r chi.Router) {
		r.Use(middleware.RateLimit(2, 5))
		// TODO: need to sanitize /login, /signup,
		r.Post("/", handlers.Login)
		r.Post("/signup", handlers.Signup)
		r.With(middleware.Auth).Post("/refresh", handlers.RefreshToken)
		r.With(middleware.Auth).Post("/logout", handlers.Logout)
		r.With(middleware.RefreshToken).Post("/refresh-token", handlers.OpaqueRefresh)
		r.With(middleware.RefreshToken).Post("/logout-token", handlers.OpaqueLogout)
	})

	// Protected routes: always require valid JWT regardless of AUTH_ENABLED
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.RateLimit(5, 10))
		r.Use(middleware.Auth)
		r.Get("/events-v2", handlers.GetEventsV2)
		r.Get("/top-nav", handlers.GetTopNav)
		r.Get("/profile", handlers.Profile)
		r.Put("/profile", handlers.UpdateProfile)
		r.Delete("/profile", handlers.DeleteAccount)
	})

	// Server configuration
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for interrupt signal
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		slog.Info("Starting server", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	<-done
	slog.Info("Shutting down server...")

	// Create a deadline for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("Server exited")
}

// securityHeaders adds basic security headers to all responses
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		next.ServeHTTP(w, r)
	})
}
