package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// refreshResponse is the JSON body returned on a successful token refresh.
// The refresh token itself is never included — it travels only via cookie.
type refreshResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RefreshHandler returns an http.HandlerFunc that validates the incoming
// opaque refresh token (extracted by RefreshTokenMiddleware), rotates it,
// issues a fresh access JWT in the response body, and sets a new HttpOnly
// refresh cookie.
func RefreshHandler(svc *RefreshService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawToken, ok := RawTokenFromContext(r.Context())
		if !ok || rawToken == "" {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		fingerprint := svc.ComputeDeviceFingerprint(r)

		pair, err := svc.ValidateAndRotate(r.Context(), rawToken, fingerprint)
		if err != nil {
			slog.Warn("refresh token rejected", "error", err)
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		setRefreshCookie(w, pair.RefreshToken)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(refreshResponse{
			Token:     pair.AccessToken,
			ExpiresAt: pair.ExpiresAt,
		})
	}
}

// LogoutHandler returns an http.HandlerFunc that revokes the current
// refresh token and clears the HttpOnly cookie.
func LogoutHandler(svc *RefreshService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawToken, ok := RawTokenFromContext(r.Context())
		if !ok || rawToken == "" {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		if err := svc.RevokeRefreshToken(r.Context(), rawToken); err != nil {
			slog.Warn("failed to revoke refresh token on logout", "error", err)
			// Still clear the cookie even if revocation fails.
		}

		clearRefreshCookie(w)
		w.WriteHeader(http.StatusNoContent)
	}
}

// setRefreshCookie writes the opaque refresh token as a strict HttpOnly cookie.
func setRefreshCookie(w http.ResponseWriter, rawToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refreshToken",
		Value:    rawToken,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60, // 30 days
	})
}

// clearRefreshCookie expires the refresh cookie immediately.
func clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refreshToken",
		Value:    "",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   -1,
	})
}
