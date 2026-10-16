package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/middleware"
	"golang.org/x/crypto/bcrypt"
)

type ChangeEmailRequest struct {
	Email string `json:"email" validate:"required,email,max=254,safe_string"`
	PW    string `json:"password" validate:"required,min=12,max=72"` // current password confirmation
}

type ChangePasswordRequest struct {
	CurrentPW string `json:"current_password" validate:"required,min=12,max=72"`
	NewPW     string `json:"password" validate:"required,min=12,max=72"` // new password
}

type DeleteAccountRequest struct {
	PW string `json:"password" validate:"required,min=12,max=72"`
}

func UpdateProfile(w http.ResponseWriter, r *http.Request, req ChangeEmailRequest) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	user, err := db.Users.GetUserByID(r.Context(), userID)
	if err != nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PW), []byte(req.PW)); err != nil {
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	if err := db.Users.UpdateUserEmail(r.Context(), userID, req.Email); err != nil {
		if errors.Is(err, db.ErrEmailAlreadyExists) {
			http.Error(w, `{"error":"Unable to update email"}`, http.StatusConflict)
			return
		}
		slog.Error("Failed to update email", "error", err, "user_id", userID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"user_id": strconv.FormatInt(userID, 10),
		"email":   req.Email,
	})
}

// UpdateProfilePassword updates the authenticated user's password.
// PUT /api/profile/password
func UpdateProfilePassword(w http.ResponseWriter, r *http.Request, req ChangePasswordRequest) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if err := validatePassword(req.NewPW); err != nil {
		http.Error(w, `{"error":"New password does not meet complexity requirements"}`, http.StatusBadRequest)
		return
	}

	user, err := db.Users.GetUserByID(r.Context(), userID)
	if err != nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PW), []byte(req.CurrentPW)); err != nil {
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	hashedPW, err := hashPassword(req.NewPW)
	if err != nil {
		slog.Error("Failed to hash new password", "error", err, "user_id", userID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	if err := db.Users.UpdateUserPassword(r.Context(), userID, hashedPW); err != nil {
		slog.Error("Failed to update password", "error", err, "user_id", userID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Sign out every other session: revoke all refresh tokens, then give
	// this browser a fresh one so the caller stays logged in.
	if refreshSvc != nil {
		if err := refreshSvc.RevokeAllForUser(r.Context(), userID); err != nil {
			slog.Error("Failed to revoke refresh tokens after password change", "error", err, "user_id", userID)
			http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
			return
		}
		setRefreshCookieIfAvailable(w, r, userID)
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteAccount deletes the authenticated user's account.
// DELETE /api/profile
func DeleteAccount(w http.ResponseWriter, r *http.Request, req DeleteAccountRequest) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	user, err := db.Users.GetUserByID(r.Context(), userID)
	if err != nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PW), []byte(req.PW)); err != nil {
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	if err := db.Users.DeleteUser(r.Context(), userID); err != nil {
		slog.Error("Failed to delete user", "error", err, "user_id", userID)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Refresh tokens are deleted with the user (ON DELETE CASCADE).
	clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
