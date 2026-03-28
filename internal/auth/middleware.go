package auth

import (
	"context"
	"net/http"
)

type ctxKey string

const rawTokenKey ctxKey = "refresh_raw_token"

// RefreshTokenMiddleware returns middleware that extracts the opaque
// refresh token from the "refreshToken" HttpOnly cookie and stores it
// in the request context.  Requests without the cookie receive a 401.
//
// Apply this middleware only to routes that consume refresh tokens
// (e.g. /auth/refresh-token, /auth/logout-token).
func RefreshTokenMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("refreshToken")
			if err != nil || cookie.Value == "" {
				http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), rawTokenKey, cookie.Value)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RawTokenFromContext retrieves the raw opaque refresh token placed
// in the context by RefreshTokenMiddleware.
func RawTokenFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(rawTokenKey).(string)
	return v, ok
}
