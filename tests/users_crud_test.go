package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/middleware"
	"golang.org/x/crypto/bcrypt"
)

type mockUserStoreCRUD struct {
	userByID map[int64]*db.User

	updateEmailCalls    int
	updateEmailArgs     map[int64]string
	updatePasswordCalls int
	updatePasswordArgs  map[int64]string
	deleteCalls         int
}

func (m *mockUserStoreCRUD) CreateUser(ctx context.Context, email, pw string) (*db.User, error) {
	return nil, errors.New("not implemented")
}
func (m *mockUserStoreCRUD) GetUserByEmail(ctx context.Context, email string) (*db.User, error) {
	return nil, errors.New("not implemented")
}
func (m *mockUserStoreCRUD) GetUserByID(ctx context.Context, userID int64) (*db.User, error) {
	u, ok := m.userByID[userID]
	if !ok {
		return nil, db.ErrUserNotFound
	}
	return u, nil
}
func (m *mockUserStoreCRUD) UpdateLastLogin(ctx context.Context, userID int64) error {
	return nil
}
func (m *mockUserStoreCRUD) UpdateUserEmail(ctx context.Context, userID int64, newEmail string) error {
	m.updateEmailCalls++
	if m.updateEmailArgs == nil {
		m.updateEmailArgs = make(map[int64]string)
	}
	m.updateEmailArgs[userID] = newEmail

	if u, ok := m.userByID[userID]; ok {
		u.Email = newEmail
	}
	return nil
}
func (m *mockUserStoreCRUD) UpdateUserPassword(ctx context.Context, userID int64, newHashedPW string) error {
	m.updatePasswordCalls++
	if m.updatePasswordArgs == nil {
		m.updatePasswordArgs = make(map[int64]string)
	}
	m.updatePasswordArgs[userID] = newHashedPW

	if u, ok := m.userByID[userID]; ok {
		u.PW = newHashedPW
	}
	return nil
}
func (m *mockUserStoreCRUD) DeleteUser(ctx context.Context, userID int64) error {
	m.deleteCalls++
	delete(m.userByID, userID)
	return nil
}

func TestUpdateProfileEmail_Success(t *testing.T) {
	setAuthEnv(t)

	const userID int64 = 7
	currentPW := "CurrentPass1!xY" // >= 12 chars
	newEmail := "new@example.com"

	hashedPW, err := bcrypt.GenerateFromPassword([]byte(currentPW), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	origUsers := db.Users
	store := &mockUserStoreCRUD{
		userByID: map[int64]*db.User{
			userID: {ID: userID, Email: "old@example.com", PW: string(hashedPW)},
		},
	}
	db.Users = store
	t.Cleanup(func() { db.Users = origUsers })

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Put("/profile/email", middleware.ValidateBody(handlers.UpdateProfileEmail))

	tokenStr := issueTestToken(t, userID)

	body, _ := json.Marshal(map[string]string{
		"email":    newEmail,
		"password": currentPW,
	})
	req := httptest.NewRequest(http.MethodPut, "/profile/email", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["user_id"] != strconv.FormatInt(userID, 10) {
		t.Fatalf("unexpected user_id: %q", resp["user_id"])
	}
	if resp["email"] != newEmail {
		t.Fatalf("unexpected email: %q", resp["email"])
	}
	if store.updateEmailCalls != 1 {
		t.Fatalf("expected UpdateUserEmail calls=1, got %d", store.updateEmailCalls)
	}
	if store.updateEmailArgs[userID] != newEmail {
		t.Fatalf("unexpected UpdateUserEmail arg: %q", store.updateEmailArgs[userID])
	}
}

func TestUpdateProfileEmail_WrongPassword(t *testing.T) {
	setAuthEnv(t)

	const userID int64 = 7
	currentPW := "CurrentPass1!xY"
	wrongPW := "WrongWrongPass1!xY"
	newEmail := "new@example.com"

	hashedPW, err := bcrypt.GenerateFromPassword([]byte(currentPW), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	origUsers := db.Users
	store := &mockUserStoreCRUD{
		userByID: map[int64]*db.User{
			userID: {ID: userID, Email: "old@example.com", PW: string(hashedPW)},
		},
	}
	db.Users = store
	t.Cleanup(func() { db.Users = origUsers })

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Put("/profile/email", middleware.ValidateBody(handlers.UpdateProfileEmail))

	tokenStr := issueTestToken(t, userID)
	body, _ := json.Marshal(map[string]string{
		"email":    newEmail,
		"password": wrongPW,
	})
	req := httptest.NewRequest(http.MethodPut, "/profile/email", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d, body=%s", w.Code, w.Body.String())
	}
	if store.updateEmailCalls != 0 {
		t.Fatalf("expected UpdateUserEmail calls=0, got %d", store.updateEmailCalls)
	}
}

func TestUpdateProfileEmail_InvalidJSON(t *testing.T) {
	setAuthEnv(t)

	const userID int64 = 7
	currentPW := "CurrentPass1!xY" // >= 12 chars
	newEmail := "new@example.com"

	hashedPW, err := bcrypt.GenerateFromPassword([]byte(currentPW), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	origUsers := db.Users
	store := &mockUserStoreCRUD{
		userByID: map[int64]*db.User{
			userID: {ID: userID, Email: "old@example.com", PW: string(hashedPW)},
		},
	}
	db.Users = store
	t.Cleanup(func() { db.Users = origUsers })

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Put("/profile/email", middleware.ValidateBody(handlers.UpdateProfileEmail))

	tokenStr := issueTestToken(t, userID)

	// Malformed JSON: triggers json.Decoder.Decode error before any update calls.
	reqBody := []byte(`{"email":"` + newEmail + `","password":`)
	req := httptest.NewRequest(http.MethodPut, "/profile/email", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
	if store.updateEmailCalls != 0 {
		t.Fatalf("expected UpdateUserEmail calls=0, got %d", store.updateEmailCalls)
	}
}

func TestUpdateProfilePassword_Success(t *testing.T) {
	setAuthEnv(t)

	const userID int64 = 7
	currentPW := "CurrentPass1!xY"
	newPW := "StrongNewPw1!XyZ" // meets validatePassword complexity

	hashedPW, err := bcrypt.GenerateFromPassword([]byte(currentPW), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	origUsers := db.Users
	store := &mockUserStoreCRUD{
		userByID: map[int64]*db.User{
			userID: {ID: userID, Email: "old@example.com", PW: string(hashedPW)},
		},
	}
	db.Users = store
	t.Cleanup(func() { db.Users = origUsers })

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Put("/profile/password", middleware.ValidateBody(handlers.UpdateProfilePassword))

	tokenStr := issueTestToken(t, userID)
	body, _ := json.Marshal(map[string]string{
		"current_password": currentPW,
		"password":          newPW,
	})
	req := httptest.NewRequest(http.MethodPut, "/profile/password", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d, body=%s", w.Code, w.Body.String())
	}
	if store.updatePasswordCalls != 1 {
		t.Fatalf("expected UpdateUserPassword calls=1, got %d", store.updatePasswordCalls)
	}
	if store.updatePasswordArgs[userID] == "" {
		t.Fatalf("expected hashed password to be passed to UpdateUserPassword")
	}
}

func TestUpdateProfilePassword_WeakNewPassword(t *testing.T) {
	setAuthEnv(t)

	const userID int64 = 7
	currentPW := "CurrentPass1!xY"
	weakNewPW := "weakpassword1" // missing uppercase and special chars

	hashedPW, err := bcrypt.GenerateFromPassword([]byte(currentPW), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	origUsers := db.Users
	store := &mockUserStoreCRUD{
		userByID: map[int64]*db.User{
			userID: {ID: userID, Email: "old@example.com", PW: string(hashedPW)},
		},
	}
	db.Users = store
	t.Cleanup(func() { db.Users = origUsers })

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Put("/profile/password", middleware.ValidateBody(handlers.UpdateProfilePassword))

	tokenStr := issueTestToken(t, userID)
	body, _ := json.Marshal(map[string]string{
		"current_password": currentPW,
		"password":          weakNewPW,
	})
	req := httptest.NewRequest(http.MethodPut, "/profile/password", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
	if store.updatePasswordCalls != 0 {
		t.Fatalf("expected UpdateUserPassword calls=0, got %d", store.updatePasswordCalls)
	}
}

func TestUpdateProfilePassword_InvalidJSON(t *testing.T) {
	setAuthEnv(t)

	const userID int64 = 7
	currentPW := "CurrentPass1!xY"
	weakNewPW := "weakpassword1"

	hashedPW, err := bcrypt.GenerateFromPassword([]byte(currentPW), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	origUsers := db.Users
	store := &mockUserStoreCRUD{
		userByID: map[int64]*db.User{
			userID: {ID: userID, Email: "old@example.com", PW: string(hashedPW)},
		},
	}
	db.Users = store
	t.Cleanup(func() { db.Users = origUsers })

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Put("/profile/password", middleware.ValidateBody(handlers.UpdateProfilePassword))

	tokenStr := issueTestToken(t, userID)

	// Malformed JSON: triggers json.Decoder.Decode error before any password compare/update calls.
	reqBody := []byte(`{"current_password":"` + currentPW + `","password":"` + weakNewPW + `"`)
	req := httptest.NewRequest(http.MethodPut, "/profile/password", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
	if store.updatePasswordCalls != 0 {
		t.Fatalf("expected UpdateUserPassword calls=0, got %d", store.updatePasswordCalls)
	}
}

func TestDeleteAccount_Success(t *testing.T) {
	setAuthEnv(t)

	const userID int64 = 7
	currentPW := "CurrentPass1!xY"

	hashedPW, err := bcrypt.GenerateFromPassword([]byte(currentPW), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	origUsers := db.Users
	store := &mockUserStoreCRUD{
		userByID: map[int64]*db.User{
			userID: {ID: userID, Email: "old@example.com", PW: string(hashedPW)},
		},
	}
	db.Users = store
	t.Cleanup(func() { db.Users = origUsers })

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Delete("/profile", middleware.ValidateBody(handlers.DeleteAccount))

	tokenStr := issueTestToken(t, userID)
	body, _ := json.Marshal(map[string]string{
		"password": currentPW,
	})
	req := httptest.NewRequest(http.MethodDelete, "/profile", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d, body=%s", w.Code, w.Body.String())
	}
	if store.deleteCalls != 1 {
		t.Fatalf("expected DeleteUser calls=1, got %d", store.deleteCalls)
	}
	if _, ok := store.userByID[userID]; ok {
		t.Fatalf("expected user to be deleted from mock store")
	}
}

func TestDeleteAccount_WrongPassword(t *testing.T) {
	setAuthEnv(t)

	const userID int64 = 7
	currentPW := "CurrentPass1!xY"
	wrongPW := "WrongWrongPass1!xY"

	hashedPW, err := bcrypt.GenerateFromPassword([]byte(currentPW), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	origUsers := db.Users
	store := &mockUserStoreCRUD{
		userByID: map[int64]*db.User{
			userID: {ID: userID, Email: "old@example.com", PW: string(hashedPW)},
		},
	}
	db.Users = store
	t.Cleanup(func() { db.Users = origUsers })

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Delete("/profile", middleware.ValidateBody(handlers.DeleteAccount))

	tokenStr := issueTestToken(t, userID)
	body, _ := json.Marshal(map[string]string{
		"password": wrongPW,
	})
	req := httptest.NewRequest(http.MethodDelete, "/profile", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d, body=%s", w.Code, w.Body.String())
	}
	if store.deleteCalls != 0 {
		t.Fatalf("expected DeleteUser calls=0, got %d", store.deleteCalls)
	}
}

func TestDeleteAccount_InvalidJSON(t *testing.T) {
	setAuthEnv(t)

	const userID int64 = 7
	currentPW := "CurrentPass1!xY"

	hashedPW, err := bcrypt.GenerateFromPassword([]byte(currentPW), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	origUsers := db.Users
	store := &mockUserStoreCRUD{
		userByID: map[int64]*db.User{
			userID: {ID: userID, Email: "old@example.com", PW: string(hashedPW)},
		},
	}
	db.Users = store
	t.Cleanup(func() { db.Users = origUsers })

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Delete("/profile", middleware.ValidateBody(handlers.DeleteAccount))

	tokenStr := issueTestToken(t, userID)

	// Malformed JSON: triggers json.Decoder.Decode error before any delete calls.
	reqBody := []byte(`{"password":`)
	req := httptest.NewRequest(http.MethodDelete, "/profile", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
	if store.deleteCalls != 0 {
		t.Fatalf("expected DeleteUser calls=0, got %d", store.deleteCalls)
	}
}

