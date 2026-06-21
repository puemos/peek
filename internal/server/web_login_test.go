package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/puemos/peek/internal/db"
	webui "github.com/puemos/peek/internal/web"
)

func TestLoginRejectsMalformedForm(t *testing.T) {
	s, _, _ := newWebLoginTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("csrf=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.handleLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Invalid session.") {
		t.Fatalf("login page did not report invalid session: %s", rec.Body.String())
	}
	if got := rec.Result().Cookies(); hasCookie(got, sessionCookie) {
		t.Fatalf("malformed login created session cookie: %+v", got)
	}
}

func TestLogoutRejectsInvalidCSRFWithoutClearingSession(t *testing.T) {
	s, _, accountID := newWebLoginTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader("csrf=bad"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(accountID, 10), sessionTTL)})
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-token"})
	rec := httptest.NewRecorder()

	s.handleLogout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "Your logout request could not be verified.") {
		t.Fatalf("logout did not render web error: %s", rec.Body.String())
	}
	if got := rec.Result().Cookies(); hasCookie(got, sessionCookie) {
		t.Fatalf("invalid logout cleared session cookie: %+v", got)
	}
}

func newWebLoginTestServer(t *testing.T) (*Server, *db.Store, int64) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	account, err := store.CreateAccount(context.Background(), "admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := webui.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	return &Server{store: store, renderer: renderer, secret: strings.Repeat("0", 64)}, store, account.ID
}

func hasCookie(cookies []*http.Cookie, name string) bool {
	for _, c := range cookies {
		if c.Name == name {
			return true
		}
	}
	return false
}
