package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/samucap/orion2.0/internal/auth"
	"github.com/samucap/orion2.0/internal/db"
)

type ctxKey string

const (
	claimsKey ctxKey = "auth_claims"
	userIDKey ctxKey = "auth_user_id"
)

// Claims represents the JWT claims issued by the auth handlers.
type Claims struct {
	jwt.RegisteredClaims
}

// UserFromContext extracts the authenticated claims from the request context.
// Returns nil, false if the request is unauthenticated.
func UserFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

// UserID returns the authenticated user's ID parsed from the JWT subject.
func UserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}

// Auth returns middleware that validates HS256 JWTs signed with cfg.JWTSecret.
// On success it stores *Claims (UserFromContext) and the int64 user ID
// (UserID) in the request context.
func Auth(cfg auth.Config) func(http.Handler) http.Handler {
	jwtSecret := cfg.JWTSecret
	if len(jwtSecret) == 0 {
		panic("middleware.Auth: auth config has no JWT secret; call auth.LoadConfig at startup")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"Authorization header required"}`, http.StatusUnauthorized)
				return
			}

			const bearerPrefix = "Bearer "
			if !strings.HasPrefix(authHeader, bearerPrefix) {
				http.Error(w, `{"error":"Invalid authorization header format"}`, http.StatusUnauthorized)
				return
			}

			tokenString := strings.TrimPrefix(authHeader, bearerPrefix)

			claims := &Claims{}
			token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return jwtSecret, nil
			}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())

			if err != nil || !token.Valid {
				http.Error(w, `{"error":"Invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			if claims.ID == "" {
				http.Error(w, `{"error":"Invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			userID, err := strconv.ParseInt(claims.Subject, 10, 64)
			if err != nil || userID <= 0 {
				http.Error(w, `{"error":"Invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			blacklisted, err := db.TokenBlacklist.IsTokenBlacklisted(r.Context(), claims.ID)
			if err != nil {
				http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
				return
			}
			if blacklisted {
				http.Error(w, `{"error":"Token revoked"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			ctx = context.WithValue(ctx, userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
