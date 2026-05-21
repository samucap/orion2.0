package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samucap/orion2.0/middleware"
)

type TestBody struct {
	Name string `json:"name" validate:"required,safe_string"`
	Age  int    `json:"age" validate:"required,min=18"`
}

func TestValidationMiddleware(t *testing.T) {
	// A dummy handler that we wrap
	dummyHandler := func(w http.ResponseWriter, r *http.Request, req TestBody) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(req)
	}

	handler := middleware.ValidateBody(dummyHandler)

	tests := []struct {
		name         string
		body         interface{}
		expectedCode int
	}{
		{
			name: "Valid request",
			body: TestBody{
				Name: "John Doe",
				Age:  25,
			},
			expectedCode: http.StatusOK,
		},
		{
			name: "Missing required field",
			body: map[string]interface{}{
				"age": 25,
			},
			expectedCode: http.StatusBadRequest,
		},
		{
			name: "Invalid age (min=18)",
			body: TestBody{
				Name: "John",
				Age:  15,
			},
			expectedCode: http.StatusBadRequest,
		},
		{
			name: "XSS attempt in safe_string field",
			body: TestBody{
				Name: "<script>alert(1)</script>",
				Age:  20,
			},
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.body)
			req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tc.expectedCode {
				t.Errorf("Expected status code %d, got %d", tc.expectedCode, w.Code)
			}
		})
	}
}

type TestQuery struct {
	Category string `json:"cat" validate:"omitempty,alphanum"`
	Limit    string `json:"limit" validate:"omitempty,numeric"`
}

func TestQueryValidationMiddleware(t *testing.T) {
	dummyHandler := func(w http.ResponseWriter, r *http.Request, req TestQuery) {
		w.WriteHeader(http.StatusOK)
	}

	handler := middleware.ValidateQuery(dummyHandler)

	tests := []struct {
		name         string
		url          string
		expectedCode int
	}{
		{
			name:         "Valid query params",
			url:          "/?cat=sports&limit=10",
			expectedCode: http.StatusOK,
		},
		{
			name:         "Invalid limit type",
			url:          "/?cat=sports&limit=ten",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Invalid chars in alphanum",
			url:          "/?cat=sports-betting&limit=10", // - is not alphanum
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.url, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tc.expectedCode {
				t.Errorf("Expected status code %d, got %d for url %s", tc.expectedCode, w.Code, tc.url)
			}
		})
	}
}
