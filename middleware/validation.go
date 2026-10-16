package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// MaxBodyBytes caps JSON request bodies. The largest legitimate body
// (ChangePasswordRequest) is under 400 bytes, so 16 KiB leaves ~40x headroom.
const MaxBodyBytes = 16 << 10

var Validate *validator.Validate

func init() {
	Validate = validator.New()

	// safe_string rejects angle brackets as defense in depth against HTML
	// injection in values that may later be rendered.
	Validate.RegisterValidation("safe_string", func(fl validator.FieldLevel) bool {
		val := fl.Field().String()
		return !strings.ContainsAny(val, "<>")
	})

	// uint_lte=N accepts a base-10 unsigned integer string whose value is <= N.
	Validate.RegisterValidation("uint_lte", func(fl validator.FieldLevel) bool {
		limit, err := strconv.ParseUint(fl.Param(), 10, 64)
		if err != nil {
			panic("uint_lte: invalid parameter " + fl.Param())
		}
		v, err := strconv.ParseUint(fl.Field().String(), 10, 64)
		return err == nil && v <= limit
	})

	// date_or_rfc3339 accepts a calendar date (2006-01-02) or an RFC 3339 timestamp.
	Validate.RegisterValidation("date_or_rfc3339", func(fl validator.FieldLevel) bool {
		val := fl.Field().String()
		if _, err := time.Parse("2006-01-02", val); err == nil {
			return true
		}
		_, err := time.Parse(time.RFC3339, val)
		return err == nil
	})
}

// ValidateBody is a generic middleware that decodes a JSON body into struct T,
// validates it using validator/v10, and if successful, passes it to the next handler.
// Bodies over MaxBodyBytes get 413; unknown fields or trailing data get 400.
func ValidateBody[T any](next func(http.ResponseWriter, *http.Request, T)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)

		var req T
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, `{"error":"Request body too large"}`, http.StatusRequestEntityTooLarge)
				return
			}
			slog.Warn("Failed to decode JSON body", "error", err, "type", fmt.Sprintf("%T", req))
			http.Error(w, `{"error":"Invalid JSON format"}`, http.StatusBadRequest)
			return
		}

		// Exactly one JSON value is allowed.
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, `{"error":"Request body too large"}`, http.StatusRequestEntityTooLarge)
				return
			}
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

// ValidateQuery is a generic middleware that decodes URL query parameters into
// struct T (matched by `json` tag; fields must be strings), validates it, and
// passes it to the next handler. Unknown or repeated parameters get 400.
func ValidateQuery[T any](next func(http.ResponseWriter, *http.Request, T)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req T

		query, err := parseStrictQuery(r.URL.RawQuery)
		if err != nil {
			slog.Warn("Query parse failed", "error", err, "type", fmt.Sprintf("%T", req))
			http.Error(w, `{"error":"Invalid query parameters."}`, http.StatusBadRequest)
			return
		}

		jsonBytes, err := json.Marshal(query)
		if err != nil {
			http.Error(w, `{"error":"Invalid query parameters."}`, http.StatusBadRequest)
			return
		}
		dec := json.NewDecoder(bytes.NewReader(jsonBytes))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			slog.Warn("Query decode failed", "error", err, "type", fmt.Sprintf("%T", req))
			http.Error(w, `{"error":"Invalid query parameters."}`, http.StatusBadRequest)
			return
		}

		if err := Validate.Struct(&req); err != nil {
			slog.Warn("Query validation failed", "error", err, "type", fmt.Sprintf("%T", req))
			http.Error(w, `{"error":"Invalid query parameters."}`, http.StatusBadRequest)
			return
		}

		next(w, r, req)
	}
}

// parseStrictQuery parses a raw query string into single-valued parameters,
// rejecting malformed encoding and repeated keys (HTTP parameter pollution).
func parseStrictQuery(raw string) (map[string]string, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		if len(v) != 1 {
			return nil, fmt.Errorf("parameter %q repeated", k)
		}
		out[k] = v[0]
	}
	return out, nil
}
