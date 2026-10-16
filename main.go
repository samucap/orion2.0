package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
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

	currEnv := os.Getenv("ENV")
	// Initialize structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Token config is required: refuse to start with a missing or weak JWT_SECRET.
	authCfg, err := auth.LoadConfig()
	if err != nil {
		slog.Error("Invalid auth configuration", "error", err)
		os.Exit(1)
	}
	handlers.SetAuthConfig(authCfg)

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
		refreshSvc := auth.NewRefreshService(refreshStore, authCfg)
		refreshSvc.SetUserRole(func(ctx context.Context, userID int64) (string, error) {
			user, err := db.Users.GetUserByID(ctx, userID)
			if err != nil {
				return "", err
			}
			return user.Role, nil
		})
		handlers.SetRefreshService(refreshSvc)
		slog.Info("Refresh token service initialized")

		demoCtx, demoCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := handlers.EnsureDemoUser(demoCtx); err != nil {
			slog.Error("Failed to ensure demo user", "error", err)
			os.Exit(1)
		}
		demoCancel()
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
	// Vercel's edge proxy overwrites X-Real-IP with the connecting client's IP
	// and drops client-supplied values. Vercel publishes no edge IP ranges, so
	// trusted-CIDR X-Forwarded-For parsing is not possible there. Off Vercel,
	// only use this behind a proxy that also overwrites X-Real-IP.
	r.Use(chiMiddleware.ClientIPFromHeader("X-Real-IP"))
	r.Use(chiMiddleware.NoCache)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))
	// An empty AllowedOrigins list means "allow all" in go-chi/cors, so always
	// set it. Origins include scheme and port, hence the localhost wildcard.
	allowedOrigins := []string{"http://localhost:*"}
	if clientOrigin := os.Getenv("CLIENT_URL"); clientOrigin != "" {
		// Browsers send Origin without a trailing slash.
		allowedOrigins = []string{strings.TrimRight(clientOrigin, "/")}
	}
	corsOpts := cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		// Required for the browser to send/store the refresh cookie cross-origin.
		AllowCredentials: true,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	}

	r.Use(cors.Handler(corsOpts))
	r.Use(securityHeaders)

	// Health check (always public)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Only JSON bodies. A JSON Content-Type forces a CORS preflight, so
		// cross-site HTML forms (text/plain, urlencoded) cannot reach handlers.
		r.Use(chiMiddleware.AllowContentType("application/json"))

		r.Route("/auth", func(r chi.Router) {
			r.Use(middleware.RateLimit(2, 5))
			// TODO: limit failed login attempts
			r.Post("/", middleware.ValidateBody(handlers.Login))
			r.Post("/demo", middleware.ValidateBody(handlers.DemoLogin))
			r.Post("/signup", middleware.ValidateBody(handlers.Signup))
			r.With(middleware.Auth(authCfg)).Post("/logout", handlers.Logout)
			// Cookie-only auth: reject cross-site callers before reading the cookie.
			r.With(middleware.RequireOrigin(allowedOrigins), middleware.RefreshToken).Post("/refresh-token", handlers.OpaqueRefresh)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimit(5, 10))
			r.Use(middleware.Auth(authCfg))
			r.Get("/events-v2", middleware.ValidateQuery(handlers.GetEventsV2))
			r.Get("/top-nav", middleware.ValidateQuery(handlers.GetTopNav))
			r.Get("/dashboard/stats", handlers.DashboardStats)
			r.Get("/dashboard/trending", handlers.DashboardTrending)
			r.Get("/dashboard/activity", handlers.DashboardActivity)
			r.Get("/dashboard/treemap", handlers.DashboardTreemap)
			r.Get("/dashboard/edge", handlers.DashboardEdge)
			r.Get("/dashboard/whales", handlers.DashboardWhales)
			r.Get("/dashboard/depth", handlers.DashboardDepth)
			r.Get("/dashboard/correlation", handlers.DashboardCorrelation)
			r.Get("/dashboard/arbitrage", handlers.DashboardArbitrage)
			r.Get("/profile", handlers.Profile)
			r.With(middleware.RequireRole(auth.RoleUser)).Put("/profile", middleware.ValidateBody(handlers.UpdateProfile))
			r.With(middleware.RequireRole(auth.RoleUser)).Put("/profile/password", middleware.ValidateBody(handlers.UpdateProfilePassword))
			r.With(middleware.RequireRole(auth.RoleUser)).Delete("/profile", middleware.ValidateBody(handlers.DeleteAccount))
			r.With(middleware.RequireRole(auth.RoleUser)).Post("/cache/clear", handlers.ClearCache)
		})
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
		slog.Info("Starting server", "currEnv", currEnv, "port", port)
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
