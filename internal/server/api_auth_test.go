package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/puemos/peek/internal/db"
)

func TestAuthTokenStoresVerifiedAPIToken(t *testing.T) {
	store := newAuthTestStore(t, "user-token", "user", false)
	s := &Server{store: store}
	var actorName string

	handler := s.authToken(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := apiToken(r)
		if !ok {
			t.Fatal("api token missing from request context")
		}
		actorName = actor.Name
		jsonOK(w, map[string]string{"status": "ok"})
	})
	req := httptest.NewRequest(http.MethodGet, "/api/uploads", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if actorName != "user" {
		t.Fatalf("actor name = %q", actorName)
	}
}

func TestAuthAdminStoresVerifiedAPIToken(t *testing.T) {
	store := newAuthTestStore(t, "admin-token", "admin", true)
	s := &Server{store: store}
	var actorName string

	handler := s.authAdmin(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := apiToken(r)
		if !ok {
			t.Fatal("api token missing from request context")
		}
		actorName = actor.Name
		jsonOK(w, map[string]string{"status": "ok"})
	})
	req := httptest.NewRequest(http.MethodGet, "/api/tokens", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if actorName != "admin" {
		t.Fatalf("actor name = %q", actorName)
	}
}

func TestAPIHandlerWithoutAuthContextFailsClosed(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/uploads", nil)

	s.handleListUploads(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error != "auth context missing" {
		t.Fatalf("error = %q", body.Error)
	}
}

func newAuthTestStore(t *testing.T, token, name string, isAdmin bool) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateToken(context.Background(), token, name, isAdmin, 0); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return store
}
