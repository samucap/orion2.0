package handlers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type strictBody struct {
	Email string `json:"email" validate:"required,email,max=254"`
}

func strictBodyHandler() http.HandlerFunc {
	return middleware.ValidateBody(func(w http.ResponseWriter, r *http.Request, req strictBody) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestValidateBody_Strictness(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid", `{"email":"a@b.co"}`, http.StatusOK},
		{"unknown field", `{"email":"a@b.co","role":"admin"}`, http.StatusBadRequest},
		{"trailing JSON value", `{"email":"a@b.co"}{"email":"c@d.co"}`, http.StatusBadRequest},
		{"trailing garbage", `{"email":"a@b.co"} x`, http.StatusBadRequest},
		{"oversized body", `{"email":"` + strings.Repeat("a", middleware.MaxBodyBytes) + `@b.co"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()
			strictBodyHandler().ServeHTTP(w, req)
			assert.Equal(t, tt.want, w.Code, w.Body.String())
		})
	}
}

func TestValidateBody_PasswordOver72BytesRejected(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Post("/signup", middleware.ValidateBody(handlers.Signup))

	body := `{"email":"a@b.co","password":"Aa1!` + strings.Repeat("x", 80) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func eventsQueryHandler(got *handlers.GetEventsV2Request) http.HandlerFunc {
	return middleware.ValidateQuery(func(w http.ResponseWriter, r *http.Request, req handlers.GetEventsV2Request) {
		*got = req
		w.WriteHeader(http.StatusOK)
	})
}

func TestValidateQuery_EventsV2(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"empty", "", http.StatusOK},
		{"typical", "tag_id=100215&limit=20&offset=40&order=volume24hr&ascending=false&end_date_max=2026-01-01", http.StatusOK},
		{"rfc3339 date", "start_date_min=2026-01-01T00:00:00Z", http.StatusOK},
		{"decimal amount", "volume_min=1000.5", http.StatusOK},
		{"unknown param", "tag_id=1&admin=true", http.StatusBadRequest},
		{"repeated param", "tag_id=1&tag_id=2", http.StatusBadRequest},
		{"injected param in date", "end_date_max=" + url.QueryEscape("2026-01-01&limit=1000"), http.StatusBadRequest},
		{"injected param in order", "order=" + url.QueryEscape("volume&closed=true"), http.StatusBadRequest},
		{"limit too large", "limit=101", http.StatusBadRequest},
		{"negative limit", "limit=-1", http.StatusBadRequest},
		{"offset too large", "offset=10001", http.StatusBadRequest},
		{"non-digit tag id", "tag_id=1.5", http.StatusBadRequest},
		{"bad boolean", "active=yes", http.StatusBadRequest},
		{"malformed escape", "tag_id=%zz", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got handlers.GetEventsV2Request
			req := httptest.NewRequest(http.MethodGet, "/events-v2?"+tt.query, nil)
			w := httptest.NewRecorder()
			eventsQueryHandler(&got).ServeHTTP(w, req)
			assert.Equal(t, tt.want, w.Code, w.Body.String())
		})
	}
}

func TestValidateQuery_TopNavRejectsNonNumericCat(t *testing.T) {
	h := middleware.ValidateQuery(func(w http.ResponseWriter, r *http.Request, req handlers.GetTopNavRequest) {
		w.WriteHeader(http.StatusOK)
	})
	for query, want := range map[string]int{
		"cat=100215":      http.StatusOK,
		"cat=abc":         http.StatusBadRequest,
		"cat=1&cat=2":     http.StatusBadRequest,
		"cat=1&other=bad": http.StatusBadRequest,
	} {
		req := httptest.NewRequest(http.MethodGet, "/top-nav?"+query, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		assert.Equal(t, want, w.Code, query)
	}
}

func TestBuildGammaEventsURL_EncodesValues(t *testing.T) {
	raw := handlers.BuildGammaEventsURL(handlers.GetEventsV2Request{
		TagID:      "100215",
		EndDateMax: "2026-01-01T00:00:00+02:00",
	})
	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "gamma-api.polymarket.com", u.Host)
	assert.Equal(t, "/events", u.Path)
	assert.Equal(t, "100215", q.Get("tag_id"))
	assert.Equal(t, "true", q.Get("related_tags"))
	assert.Equal(t, "2026-01-01T00:00:00+02:00", q.Get("end_date_max"), "+ must be percent-encoded, not decoded as a space")
	assert.Equal(t, "50", q.Get("limit"))
	assert.Equal(t, "volume24hr", q.Get("order"))
	for k, v := range q {
		assert.Len(t, v, 1, "parameter %q must appear once", k)
	}
}

func TestBuildGammaEventsURL_CannotInjectParams(t *testing.T) {
	// Even if a value bypassed validation, url.Values keeps it inside one parameter.
	raw := handlers.BuildGammaEventsURL(handlers.GetEventsV2Request{Order: "volume&closed=true"})
	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "volume&closed=true", u.Query().Get("order"))
	assert.Equal(t, []string{"false"}, u.Query()["closed"])
}

func TestRateLimit_KeysOnClientIPAndIgnoresSpoofedHeader(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("without ClientIP middleware a spoofed X-Real-IP does not create new buckets", func(t *testing.T) {
		h := middleware.RateLimit(0.001, 1)(ok)
		codes := make([]int, 0, 3)
		for i, spoof := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "10.0.0.1:" + []string{"1111", "2222", "3333"}[i]
			req.Header.Set("X-Real-IP", spoof)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			codes = append(codes, w.Code)
		}
		assert.Equal(t, []int{http.StatusOK, http.StatusTooManyRequests, http.StatusTooManyRequests}, codes,
			"same peer IP on different ports must share one bucket")
	})

	t.Run("with ClientIPFromHeader each client IP gets its own bucket", func(t *testing.T) {
		h := chiMiddleware.ClientIPFromHeader("X-Real-IP")(middleware.RateLimit(0.001, 1)(ok))
		send := func(ip string) int {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "10.0.0.1:1234" // the proxy
			req.Header.Set("X-Real-IP", ip)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			return w.Code
		}
		assert.Equal(t, http.StatusOK, send("203.0.113.1"))
		assert.Equal(t, http.StatusTooManyRequests, send("203.0.113.1"))
		assert.Equal(t, http.StatusOK, send("203.0.113.2"))
	})
}

func TestCacheClear_RequiresAuth(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.With(middleware.Auth(testAuthConfig(t))).Post("/api/cache/clear", handlers.ClearCache)

	req := httptest.NewRequest(http.MethodPost, "/api/cache/clear", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/cache/clear", nil)
	req.Header.Set("Authorization", "Bearer "+issueTestToken(t, 1))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_RejectsNonNumericSubject(t *testing.T) {
	setAuthEnv(t)
	h := middleware.Auth(testAuthConfig(t))(okHandler())
	jti := "jti-x"
	tok := issueTestTokenCustomSub(t, "not-a-number", jti)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
