package middleware

import (
	"context"
	"net/http"
)

const refreshTokenKey ctxKey = "refresh_raw_token"

// RefreshToken is middleware that extracts the opaque refresh token from
// the "refreshToken" HttpOnly cookie and stores it in the request context.
// Requests without the cookie receive a 401.
//
// Apply only to routes that consume refresh tokens
// (e.g. /api/auth/refresh-token, /api/auth/logout-token).
func RefreshToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("refreshToken")
		if err != nil || cookie.Value == "" {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), refreshTokenKey, cookie.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RawRefreshTokenFromContext retrieves the raw opaque refresh token
// placed in the context by RefreshToken middleware.
func RawRefreshTokenFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(refreshTokenKey).(string)
	return v, ok
}
