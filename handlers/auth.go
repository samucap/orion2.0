package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/samucap/orion2.0/internal/auth"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/middleware"
	"golang.org/x/crypto/bcrypt"
)

// maxPasswordBytes is bcrypt's input limit; longer passwords fail to hash.
const maxPasswordBytes = 72

// AuthRequest represents the request body for signup/login
type AuthRequest struct {
	Email string `json:"email" validate:"required,email,max=254,safe_string"`
	PW    string `json:"password" validate:"required,min=12,max=72"`
}

// AuthResponse represents the response body for successful auth
type AuthResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SignupRequest represents the request body for signup
type SignupRequest struct {
	AuthRequest
}

// LoginRequest represents the request body for login
type LoginRequest struct {
	AuthRequest
}

var (
	refreshSvc *auth.RefreshService
	authCfg    auth.Config
)

// SetRefreshService injects the refresh token service so Login/Signup
// can issue HttpOnly refresh cookies alongside the access JWT.
func SetRefreshService(s *auth.RefreshService) { refreshSvc = s }

// SetAuthConfig injects the token config (secret and lifetimes) loaded at startup.
func SetAuthConfig(cfg auth.Config) { authCfg = cfg }

// Signup handles user registration
// POST /api/auth/signup
// Body: {"email": "user@example.com", "password": "Str0ng!Pass#2026"}
func Signup(w http.ResponseWriter, r *http.Request, req SignupRequest) {
	ctx := r.Context()

	if err := validatePassword(req.PW); err != nil {
		http.Error(w, `{"error":"Password must be 12-72 characters with uppercase, lowercase, digit, and special character"}`, http.StatusBadRequest)
		return
	}

	hashedPW, err := hashPassword(req.PW)
	if err != nil {
		slog.Error("Failed to hash password on signup", "error", err)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	user, err := db.Users.CreateUser(ctx, req.Email, hashedPW)
	if err != nil {
		if !errors.Is(err, db.ErrEmailAlreadyExists) {
			slog.Error("Failed to create user", "error", err)
			http.Error(w, `{"error":"Service unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		// One generic response for every failure so the endpoint does not
		// confirm which emails are registered.
		http.Error(w, `{"error":"Unable to create account"}`, http.StatusBadRequest)
		return
	}

	token, expiresAt, err := auth.IssueAccessToken(authCfg, user.ID, roleOrUser(user))
	if err != nil {
		slog.Error("Failed to generate JWT", "error", err, "user_id", user.ID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	setRefreshCookieIfAvailable(w, r, user.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(AuthResponse{
		Token:     token,
		ExpiresAt: expiresAt,
	})
}

// Login handles user authentication
// POST /api/auth
// Body: {"email": "user@example.com", "password": "Str0ng!Pass#2026"}
func Login(w http.ResponseWriter, r *http.Request, req LoginRequest) {
	ctx := r.Context()

	user, err := db.Users.GetUserByEmail(ctx, req.Email)
	if err != nil {
		if !errors.Is(err, db.ErrUserNotFound) {
			slog.Error("Failed to look up user on login", "error", err)
			http.Error(w, `{"error":"Service unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		// Spend the same bcrypt time as a real check so response timing
		// does not reveal whether the email exists.
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash(), []byte(req.PW))
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PW), []byte(req.PW)); err != nil {
		slog.Info("Login failed: invalid password", "user_id", user.ID)
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	if err := db.Users.UpdateLastLogin(ctx, user.ID); err != nil {
		slog.Error("Failed to update last login", "error", err, "user_id", user.ID)
		// Don't fail the login for this
	}

	token, expiresAt, err := auth.IssueAccessToken(authCfg, user.ID, roleOrUser(user))
	if err != nil {
		slog.Error("Failed to generate JWT", "error", err, "user_id", user.ID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	setRefreshCookieIfAvailable(w, r, user.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(AuthResponse{
		Token:     token,
		ExpiresAt: expiresAt,
	})
}

// DemoRequest is an empty body. Unknown fields are rejected by ValidateBody,
// so a client cannot choose the email, password, or role.
type DemoRequest struct{}

// DemoLogin signs in the server-configured demo user.
// POST /api/auth/demo
// Body: {}
func DemoLogin(w http.ResponseWriter, r *http.Request, _ DemoRequest) {
	email := os.Getenv("DEMO_USER_EMAIL")
	if email == "" || os.Getenv("DEMO_USER_PW") == "" {
		http.Error(w, `{"error":"Demo login is not configured"}`, http.StatusServiceUnavailable)
		return
	}

	user, err := db.Users.GetUserByEmail(r.Context(), email)
	if err != nil || user.Role != auth.RoleDemo {
		if err != nil && !errors.Is(err, db.ErrUserNotFound) {
			slog.Error("Failed to look up demo user", "error", err)
		}
		http.Error(w, `{"error":"Demo login is not configured"}`, http.StatusServiceUnavailable)
		return
	}

	token, expiresAt, err := auth.IssueAccessToken(authCfg, user.ID, auth.RoleDemo)
	if err != nil {
		slog.Error("Failed to generate demo JWT", "error", err, "user_id", user.ID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	setDemoRefreshCookie(w, r, user.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(AuthResponse{
		Token:     token,
		ExpiresAt: expiresAt,
	})
}

func roleOrUser(user *db.User) string {
	if user.Role == "" {
		return auth.RoleUser
	}
	return user.Role
}

// EnsureDemoUser creates or updates the demo account from the environment.
// The password is hashed and never logged. Both variables must be set, or neither.
func EnsureDemoUser(ctx context.Context) error {
	email := os.Getenv("DEMO_USER_EMAIL")
	password := os.Getenv("DEMO_USER_PW")
	if email == "" && password == "" {
		return nil
	}
	if email == "" || password == "" {
		return errors.New("DEMO_USER_EMAIL and DEMO_USER_PW must both be set")
	}
	if err := validatePassword(password); err != nil {
		return err
	}

	hashed, err := hashPassword(password)
	if err != nil {
		return err
	}

	user, err := db.Users.GetUserByEmail(ctx, email)
	if errors.Is(err, db.ErrUserNotFound) {
		return db.CreateDemoUser(ctx, email, hashed)
	}
	if err != nil {
		return err
	}
	if err := db.Users.UpdateUserPassword(ctx, user.ID, hashed); err != nil {
		return err
	}
	return db.SetUserRole(ctx, user.ID, auth.RoleDemo)
}

// Logout revokes the current session: it blacklists the access token (jti),
// revokes the refresh token from the cookie when present, and clears the cookie.
// POST /api/auth/logout (requires Authorization: Bearer <access token>)
func Logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.UserFromContext(r.Context())
	userID, idOK := middleware.UserID(r.Context())
	if !ok || !idOK {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if claims.ID == "" || claims.ExpiresAt == nil {
		http.Error(w, `{"error":"Invalid token"}`, http.StatusUnauthorized)
		return
	}

	if err := db.TokenBlacklist.BlacklistToken(r.Context(), claims.ID, userID, claims.ExpiresAt.Time); err != nil {
		slog.Error("Failed to blacklist token", "error", err, "user_id", userID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	if cookie, err := r.Cookie(refreshCookieName); err == nil && cookie.Value != "" && refreshSvc != nil {
		if err := refreshSvc.RevokeRefreshToken(r.Context(), cookie.Value); err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
			slog.Warn("Failed to revoke refresh token on logout", "error", err, "user_id", userID)
		}
	}

	clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// OpaqueRefresh validates the incoming opaque refresh token (extracted
// by middleware.RefreshToken), rotates it, issues a fresh access JWT in
// the response body, and sets a new HttpOnly refresh cookie.
// POST /api/auth/refresh-token
func OpaqueRefresh(w http.ResponseWriter, r *http.Request) {
	rawToken, ok := middleware.RawRefreshTokenFromContext(r.Context())
	if !ok || rawToken == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if refreshSvc == nil {
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	fingerprint := refreshSvc.ComputeDeviceFingerprint(r)

	pair, err := refreshSvc.ValidateAndRotate(r.Context(), rawToken, fingerprint)
	if err != nil {
		slog.Warn("Refresh token rejected", "error", err)
		clearRefreshCookie(w)
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	setRefreshCookie(w, pair.RefreshToken, pair.RefreshExpiresAt)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AuthResponse{
		Token:     pair.AccessToken,
		ExpiresAt: pair.ExpiresAt,
	})
}

// hashPassword hashes a password using bcrypt
func hashPassword(password string) (string, error) {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost())
	if err != nil {
		return "", err
	}
	return string(hashedBytes), nil
}

func bcryptCost() int {
	cost := 12
	if v := os.Getenv("BCRYPT_COST"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= bcrypt.MinCost && parsed <= bcrypt.MaxCost {
			cost = parsed
		}
	}
	return cost
}

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

// dummyPasswordHash returns a bcrypt hash at the configured cost, used to
// equalize login timing for unknown emails.
func dummyPasswordHash() []byte {
	dummyHashOnce.Do(func() {
		h, err := bcrypt.GenerateFromPassword([]byte("timing-equalizer-not-a-real-password"), bcryptCost())
		if err != nil {
			panic("bcrypt dummy hash: " + err.Error())
		}
		dummyHash = h
	})
	return dummyHash
}

var (
	reUpper   = regexp.MustCompile(`[A-Z]`)
	reLower   = regexp.MustCompile(`[a-z]`)
	reDigit   = regexp.MustCompile(`[0-9]`)
	reSpecial = regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?~]`)
)

// validatePassword enforces password complexity requirements
func validatePassword(password string) error {
	if len(password) < 12 {
		return errors.New("password too short")
	}
	if len(password) > maxPasswordBytes {
		return errors.New("password exceeds 72 bytes")
	}
	if !reUpper.MatchString(password) {
		return errors.New("password must contain at least one uppercase letter")
	}
	if !reLower.MatchString(password) {
		return errors.New("password must contain at least one lowercase letter")
	}
	if !reDigit.MatchString(password) {
		return errors.New("password must contain at least one digit")
	}
	if !reSpecial.MatchString(password) {
		return errors.New("password must contain at least one special character")
	}
	return nil
}

const (
	refreshCookieName = "refreshToken"
	// refreshCookiePath limits the cookie to the auth endpoints that consume it.
	refreshCookiePath = "/api/auth"
)

// SameSite=Strict relies on the frontend proxying /api/* through its own
// domain, which makes this a first-party cookie. Calling the API directly
// from another site would need SameSite=None, which reopens CSRF.
// See README "Refresh cookie and CSRF".
func setRefreshCookie(w http.ResponseWriter, rawToken string, expiresAt time.Time) {
	// Ceil so a deadline a fraction of a second out still covers that second.
	maxAge := int(math.Ceil(time.Until(expiresAt).Seconds()))
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    rawToken,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     refreshCookiePath,
		MaxAge:   maxAge,
	})
}

func refreshTTL() time.Duration {
	if authCfg.RefreshTTL > 0 {
		return authCfg.RefreshTTL
	}
	return auth.DefaultRefreshTTL
}

// clearRefreshCookie expires the cookie at its current path and at the
// legacy "/" path used before the path was narrowed.
func clearRefreshCookie(w http.ResponseWriter) {
	for _, path := range []string{refreshCookiePath, "/"} {
		http.SetCookie(w, &http.Cookie{
			Name:     refreshCookieName,
			Value:    "",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			Path:     path,
			MaxAge:   -1,
		})
	}
}

// setRefreshCookieIfAvailable issues an opaque refresh token and sets it
// as an HttpOnly cookie when the RefreshService has been configured.
// Failure to create the token is logged but does not block the response.
func setRefreshCookieIfAvailable(w http.ResponseWriter, r *http.Request, userID int64) {
	if refreshSvc == nil {
		return
	}
	fp := refreshSvc.ComputeDeviceFingerprint(r)
	expiresAt := time.Now().Add(refreshTTL())
	rawRefresh, err := refreshSvc.CreateRefreshToken(r.Context(), userID, fp)
	if err != nil {
		slog.Error("Failed to create refresh token", "error", err, "user_id", userID)
		return
	}
	setRefreshCookie(w, rawRefresh, expiresAt)
}

// setDemoRefreshCookie issues a refresh token whose deadline is one hour
// from now and does not slide on later rotations.
func setDemoRefreshCookie(w http.ResponseWriter, r *http.Request, userID int64) {
	if refreshSvc == nil {
		return
	}
	expiresAt := time.Now().Add(auth.DemoRefreshTTL)
	fp := refreshSvc.ComputeDeviceFingerprint(r)
	rawRefresh, err := refreshSvc.CreateRefreshTokenUntil(r.Context(), userID, fp, expiresAt)
	if err != nil {
		slog.Error("Failed to create demo refresh token", "error", err, "user_id", userID)
		return
	}
	setRefreshCookie(w, rawRefresh, expiresAt)
}
