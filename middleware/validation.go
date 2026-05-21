package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
)

var Validate *validator.Validate

func init() {
	Validate = validator.New()

	// Register custom safe_string validator to prevent basic XSS payloads
	Validate.RegisterValidation("safe_string", func(fl validator.FieldLevel) bool {
		val := fl.Field().String()
		if strings.Contains(val, "<") || strings.Contains(val, ">") {
			return false
		}
		return true
	})
}

// ValidateBody is a generic middleware that decodes a JSON body into struct T,
// validates it using validator/v10, and if successful, passes it to the next handler.
func ValidateBody[T any](next func(http.ResponseWriter, *http.Request, T)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req T
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Warn("Failed to decode JSON body", "error", err)
			http.Error(w, `{"error":"Invalid JSON format"}`, http.StatusBadRequest)
			return
		}

		if err := Validate.Struct(&req); err != nil {
			slog.Warn("Body validation failed", "error", err, "type", fmt.Sprintf("%T", req))
			http.Error(w, `{"error":"Validation failed. Please check your inputs."}`, http.StatusBadRequest)
			return
		}

		next(w, r, req)
	}
}

// ValidateQuery is a generic middleware that decodes URL query parameters into struct T,
// validates it, and passes it to the next handler.
func ValidateQuery[T any](next func(http.ResponseWriter, *http.Request, T)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req T
		
		// Map URL query to a map to easily unmarshal it via JSON as a quick hack for query parsing,
		// or use reflection to populate fields based on `json` tags. 
		// Since generic query parsing is complex, we'll implement a simple json unmarshal technique:
		queryMap := make(map[string]interface{})
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				queryMap[k] = v[0]
			}
		}

		jsonBytes, err := json.Marshal(queryMap)
		if err == nil {
			// Ignore unmarshal errors as some types might not match perfectly, validator will catch missing fields
			_ = json.Unmarshal(jsonBytes, &req) 
		}

		if err := Validate.Struct(&req); err != nil {
			slog.Warn("Query validation failed", "error", err, "type", fmt.Sprintf("%T", req))
			http.Error(w, `{"error":"Invalid query parameters."}`, http.StatusBadRequest)
			return
		}

		next(w, r, req)
	}
}
