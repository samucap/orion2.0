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
	Email string `json:"email" validate:"required,email"`
	PW    string `json:"password" validate:"required,min=12"` // current password confirmation
}

type ChangePasswordRequest struct {
	CurrentPW string `json:"current_password" validate:"required,min=12"`
	NewPW     string `json:"password" validate:"required,min=12"` // new password
}

type DeleteAccountRequest struct {
	PW string `json:"password" validate:"required,min=12"`
}

// UpdateProfileEmail updates the authenticated user's email.
// PUT /api/profile
func UpdateProfile(w http.ResponseWriter, r *http.Request) {
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

	var req ChangeEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid JSON format"}`, http.StatusBadRequest)
		return
	}
	if err := validate.Struct(req); err != nil {
		http.Error(w, `{"error":"Invalid email or password format"}`, http.StatusBadRequest)
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

// UpdateProfileEmail is a compatibility wrapper used by existing route wiring/tests.
// Behavior is identical to UpdateProfile.
func UpdateProfileEmail(w http.ResponseWriter, r *http.Request) {
	UpdateProfile(w, r)
}

// UpdateProfilePassword updates the authenticated user's password.
// PUT /api/profile/password
func UpdateProfilePassword(w http.ResponseWriter, r *http.Request) {
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

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid JSON format"}`, http.StatusBadRequest)
		return
	}
	if err := validate.Struct(req); err != nil {
		http.Error(w, `{"error":"Invalid password format"}`, http.StatusBadRequest)
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

	w.WriteHeader(http.StatusNoContent)
}

// DeleteAccount deletes the authenticated user's account.
// DELETE /api/profile
func DeleteAccount(w http.ResponseWriter, r *http.Request) {
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

	var req DeleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid JSON format"}`, http.StatusBadRequest)
		return
	}
	if err := validate.Struct(req); err != nil {
		http.Error(w, `{"error":"Invalid password format"}`, http.StatusBadRequest)
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

	w.WriteHeader(http.StatusNoContent)
}
