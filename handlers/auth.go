package handlers

import (
	"crypto/rand"
	"encoding/json"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/golang-jwt/jwt/v5"
	"github.com/samucap/orion2.0/internal/auth"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/middleware"
	"golang.org/x/crypto/bcrypt"
)

// AuthRequest represents the request body for signup/login
type AuthRequest struct {
	Email string `json:"email" validate:"required,email"`
	PW    string `json:"password" validate:"required,min=12"`
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

var validate = validator.New()

var refreshSvc *auth.RefreshService

// SetRefreshService injects the refresh token service so Login/Signup
// can issue HttpOnly refresh cookies alongside the access JWT.
func SetRefreshService(s *auth.RefreshService) { refreshSvc = s }

// Signup handles user registration
// POST /api/auth/signup
// Body: {"email": "user@example.com", "password": "Str0ng!Pass#2026"}
func Signup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Error("Failed to decode signup request", "error", err)
		http.Error(w, `{"error":"Invalid JSON format"}`, http.StatusBadRequest)
		return
	}

	// Validate request
	if err := validate.Struct(req); err != nil {
		slog.Error("Signup validation failed", "error", err, "email", req.Email)
		http.Error(w, `{"error":"Invalid email or password format"}`, http.StatusBadRequest)
		return
	}

	// Validate password complexity
	if err := validatePassword(req.PW); err != nil {
		slog.Error("Password validation failed", "error", err, "email", req.Email)
		http.Error(w, `{"error":"Password must be at least 12 characters with uppercase, lowercase, digit, and special character"}`, http.StatusBadRequest)
		return
	}

	// Hash password
	hashedPW, err := hashPassword(req.PW)
	if err != nil {
		slog.Error("Failed to hash password", "error", err, "email", req.Email)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Create user
	user, err := db.Users.CreateUser(ctx, req.Email, hashedPW)
	if err != nil {
		slog.Error("Failed to create user", "error", err, "email", req.Email)
		// Generic error to prevent email enumeration
		http.Error(w, `{"error":"Unable to create account. Email may already be registered"}`, http.StatusConflict)
		return
	}

	// Generate JWT
	token, expiresAt, err := generateJWT(user.ID)
	if err != nil {
		slog.Error("Failed to generate JWT", "error", err, "user_id", user.ID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	setRefreshCookieIfAvailable(w, r, user.ID)

	// Return success response
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
func Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Error("Failed to decode login request", "error", err)
		http.Error(w, `{"error":"Invalid JSON format"}`, http.StatusBadRequest)
		return
	}

	// Validate request
	if err := validate.Struct(req); err != nil {
		slog.Error("Login validation failed", "error", err, "email", req.Email)
		http.Error(w, `{"error":"Invalid email or password format"}`, http.StatusBadRequest)
		return
	}

	// Get user by email
	user, err := db.Users.GetUserByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			slog.Info("Login attempt for non-existent user", "email", req.Email)
		} else {
			slog.Error("Failed to get user by email", "error", err, "email", req.Email)
		}
		// Generic error for both "not found" and DB errors to prevent email enumeration
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PW), []byte(req.PW)); err != nil {
		slog.Info("Invalid password for user", "user_id", user.ID, "email", req.Email)
		// Generic error to prevent email enumeration
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	// Update last login
	if err := db.Users.UpdateLastLogin(ctx, user.ID); err != nil {
		slog.Error("Failed to update last login", "error", err, "user_id", user.ID)
		// Don't fail the login for this
	}

	// Generate JWT
	token, expiresAt, err := generateJWT(user.ID)
	if err != nil {
		slog.Error("Failed to generate JWT", "error", err, "user_id", user.ID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	setRefreshCookieIfAvailable(w, r, user.ID)

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(AuthResponse{
		Token:     token,
		ExpiresAt: expiresAt,
	})
}

// RefreshToken handles token refresh.
// POST /api/auth/refresh
func RefreshToken(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	token, expiresAt, err := generateJWT(userID)
	if err != nil {
		slog.Error("Failed to generate refreshed JWT", "error", err, "user_id", userID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AuthResponse{
		Token:     token,
		ExpiresAt: expiresAt,
	})
}

// Logout handles server-side token revocation (logout).
// POST /api/auth/logout
func Logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if claims.ID == "" || claims.ExpiresAt == nil {
		http.Error(w, `{"error":"Invalid token"}`, http.StatusUnauthorized)
		return
	}

	// Blacklist the current token id (jti) until it expires.
	if err := db.TokenBlacklist.BlacklistToken(r.Context(), claims.ID, userID, claims.ExpiresAt.Time); err != nil {
		slog.Error("Failed to blacklist token", "error", err, "user_id", userID, "jti", claims.ID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// hashPassword hashes a password using bcrypt
func hashPassword(password string) (string, error) {
	costStr := os.Getenv("BCRYPT_COST")
	cost := 12 // default
	if costStr != "" {
		if parsedCost, err := strconv.Atoi(costStr); err == nil && parsedCost >= 4 && parsedCost <= 31 {
			cost = parsedCost
		}
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hashedBytes), nil
}

// validatePassword enforces password complexity requirements
func validatePassword(password string) error {
	if len(password) < 12 {
		return errors.New("password too short")
	}

	// Check for at least one uppercase letter
	if !regexp.MustCompile(`[A-Z]`).MatchString(password) {
		return errors.New("password must contain at least one uppercase letter")
	}

	// Check for at least one lowercase letter
	if !regexp.MustCompile(`[a-z]`).MatchString(password) {
		return errors.New("password must contain at least one lowercase letter")
	}

	// Check for at least one digit
	if !regexp.MustCompile(`[0-9]`).MatchString(password) {
		return errors.New("password must contain at least one digit")
	}

	// Check for at least one special character
	if !regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?~]`).MatchString(password) {
		return errors.New("password must contain at least one special character")
	}

	return nil
}

// setRefreshCookieIfAvailable issues an opaque refresh token and sets it
// as an HttpOnly cookie when the RefreshService has been configured.
// Failure to create the token is logged but does not block the response.
func setRefreshCookieIfAvailable(w http.ResponseWriter, r *http.Request, userID int64) {
	if refreshSvc == nil {
		return
	}
	fp := refreshSvc.ComputeDeviceFingerprint(r)
	rawRefresh, err := refreshSvc.CreateRefreshToken(r.Context(), userID, fp)
	if err != nil {
		slog.Error("Failed to create refresh token", "error", err, "user_id", userID)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "refreshToken",
		Value:    rawRefresh,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60,
	})
}

// generateJWT creates a JWT token for a user
func generateJWT(userID int64) (string, time.Time, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return "", time.Time{}, errors.New("JWT_SECRET not set")
	}

	expiryMinutesStr := os.Getenv("JWT_EXPIRY_MINUTES")
	expiryMinutes := 15 // default
	if expiryMinutesStr != "" {
		if parsedMinutes, err := strconv.Atoi(expiryMinutesStr); err == nil && parsedMinutes > 0 {
			expiryMinutes = parsedMinutes
		}
	}

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(time.Duration(expiryMinutes) * time.Minute)

	// jti lets the backend revoke/blacklist a specific issued token.
	var jtiBytes [16]byte
	if _, err := rand.Read(jtiBytes[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate token id: %w", err)
	}
	jti := hex.EncodeToString(jtiBytes[:])

	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		ID:        jti,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", time.Time{}, err
	}

	return tokenString, expiresAt, nil
}
