package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/puemos/peek/internal/db"
	webui "github.com/puemos/peek/internal/web"
)

func TestInviteLinkMissingTokenRendersWebError(t *testing.T) {
	s, _, _ := newAdminTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/invite/missing", nil)
	req.SetPathValue("token", "missing")
	rec := httptest.NewRecorder()

	s.handleInviteLink(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "Invite not found") {
		t.Fatalf("invite error did not render web page: %s", rec.Body.String())
	}
}

func TestDashboardRevokeInviteRejectsMissingInvite(t *testing.T) {
	s, _, accountID := newAdminTestServer(t)

	rec := httptest.NewRecorder()
	s.handleDashboardRevokeInvite(rec, dashboardInviteRequest(s, accountID, "999"))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if got := u.Query().Get("err"); got != "bad invite" {
		t.Fatalf("err query = %q, location = %q", got, loc)
	}
}

func TestDashboardRevokeInviteReportsSuccessAfterUpdate(t *testing.T) {
	s, store, accountID := newAdminTestServer(t)
	inv, err := store.CreateInvite("raw", "ciphertext", "user@example.test", accountID, time.Now().Add(inviteTTL))
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleDashboardRevokeInvite(rec, dashboardInviteRequest(s, accountID, strconv.FormatInt(inv.ID, 10)))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Location") != "/dashboard?ok=invite+revoked" {
		t.Fatalf("location = %q", rec.Header().Get("Location"))
	}
}

func TestDashboardInviteRowsLogsDecryptFailure(t *testing.T) {
	s, store, accountID := newAdminTestServer(t)
	s.secret = "not-a-valid-secret"
	if _, err := store.CreateInvite("raw", "ciphertext", "user@example.test", accountID, time.Now().Add(inviteTTL)); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	rows := s.dashboardInviteRows()

	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Link != "" {
		t.Fatalf("link should be empty when decrypt fails: %+v", rows[0])
	}
	if !strings.Contains(logs.String(), "dashboard invite decrypt failed") {
		t.Fatalf("decrypt failure was not logged: %s", logs.String())
	}
}

func newAdminTestServer(t *testing.T) (*Server, *db.Store, int64) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	account, err := store.CreateAccount("admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := webui.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	return &Server{store: store, renderer: renderer, secret: strings.Repeat("0", 64)}, store, account.ID
}

func dashboardInviteRequest(s *Server, accountID int64, id string) *http.Request {
	form := url.Values{"csrf": {"csrf-token"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/invites/revoke/"+id, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", id)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(accountID, 10), sessionTTL)})
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-token"})
	return req
}
