package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// clientKey returns the client IP set by chi's ClientIPFromHeader middleware
// (wired in main.go to Vercel's proxy-overwritten X-Real-IP header). Without
// that middleware (local dev), it uses the TCP peer address without the port.
// Request headers are never read here, so clients cannot pick their own key.
func clientKey(r *http.Request) string {
	if ip := chiMiddleware.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// RateLimit returns middleware that enforces a per-IP token-bucket rate limit.
// rps is the steady-state requests per second; burst is the maximum burst size.
// Counters live in process memory, so each server instance limits separately.
func RateLimit(rps float64, burst int) func(http.Handler) http.Handler {
	var (
		mu       sync.Mutex
		visitors = make(map[string]*visitor)
	)

	// Background cleanup: drop entries unseen for 5 minutes.
	go func() {
		for {
			time.Sleep(3 * time.Minute)
			mu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 5*time.Minute {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientKey(r)

			mu.Lock()
			v, exists := visitors[ip]
			if !exists {
				v = &visitor{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
				visitors[ip] = v
			}
			v.lastSeen = time.Now()
			mu.Unlock()

			if !v.limiter.Allow() {
				http.Error(w, `{"error":"Too many requests"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
