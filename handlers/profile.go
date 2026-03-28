package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/middleware"
)

// ProfileResponse is the JSON body returned by the profile endpoint.
type ProfileResponse struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

// Profile returns the authenticated user's basic info.
// GET /api/protected/profile
func Profile(w http.ResponseWriter, r *http.Request) {
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

	user, err := db.Users.GetUserByID(r.Context(), userID)
	if err != nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ProfileResponse{
		UserID: strconv.FormatInt(user.ID, 10),
		Email:  user.Email,
	})
}
