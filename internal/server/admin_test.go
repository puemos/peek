package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/puemos/peek/internal/db"
)

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
	return &Server{store: store, secret: strings.Repeat("0", 64)}, store, account.ID
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
