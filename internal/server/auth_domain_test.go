package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func TestNormalizeAllowedEmailDomain(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "blank disables", value: " ", want: ""},
		{name: "lowercase", value: "Example.COM", want: "example.com"},
		{name: "leading at", value: "@Example.COM", want: "example.com"},
		{name: "subdomain", value: "team.example.com", want: "team.example.com"},
		{name: "email rejected", value: "user@example.com", wantErr: true},
		{name: "wildcard rejected", value: "*.example.com", wantErr: true},
		{name: "comma rejected", value: "example.com,other.com", wantErr: true},
		{name: "url rejected", value: "https://example.com", wantErr: true},
		{name: "empty label rejected", value: "example..com", wantErr: true},
		{name: "hyphen prefix rejected", value: "-example.com", wantErr: true},
		{name: "hyphen suffix rejected", value: "example-.com", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeAllowedEmailDomain(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeAllowedEmailDomain(%q): %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeAllowedEmailDomain(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestEmailMatchesAllowedDomain(t *testing.T) {
	tests := []struct {
		email  string
		domain string
		want   bool
	}{
		{email: "user@example.com", domain: "", want: true},
		{email: "USER@Example.COM", domain: "example.com", want: true},
		{email: "user@team.example.com", domain: "example.com", want: false},
		{email: "user@example.com", domain: "team.example.com", want: false},
		{email: "not-an-email", domain: "example.com", want: false},
		{email: "", domain: "example.com", want: false},
	}
	for _, tt := range tests {
		if got := emailMatchesAllowedDomain(tt.email, tt.domain); got != tt.want {
			t.Fatalf("emailMatchesAllowedDomain(%q, %q) = %t, want %t", tt.email, tt.domain, got, tt.want)
		}
	}
}

func TestValidateAllowedEmailDomainUpdateRequiresActiveMatchingAdmin(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.store.CreateAccount(context.Background(), "admin@example.com", "Admin", true); err != nil {
		t.Fatal(err)
	}
	disabled, err := s.store.CreateAccount(context.Background(), "disabled@other.com", "Disabled", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetAccountDisabled(context.Background(), disabled.ID, true); err != nil {
		t.Fatal(err)
	}

	if err := s.validateAllowedEmailDomainUpdate(context.Background(), map[string]string{authAllowedEmailDomainSetting: "example.com"}); err != nil {
		t.Fatalf("matching active admin rejected: %v", err)
	}
	if err := s.validateAllowedEmailDomainUpdate(context.Background(), map[string]string{authAllowedEmailDomainSetting: "other.com"}); err == nil {
		t.Fatal("expected non-matching domain to be rejected")
	}
	if err := s.validateAllowedEmailDomainUpdate(context.Background(), map[string]string{authAllowedEmailDomainSetting: ""}); err != nil {
		t.Fatalf("clearing domain rejected: %v", err)
	}
}

func TestUpdateSettingsRejectsDomainThatWouldLockOutAdminsBeforeWriting(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.CreateAccount(context.Background(), "admin@example.com", "Admin", true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "max_upload", "1024"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}

	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"auth_allowed_email_domain":"other.com","max_upload":"2048"}`))
	req = withAPIToken(req, &models.Token{Name: "admin", IsAdmin: true})
	rec := httptest.NewRecorder()

	s.handleUpdateSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := mustGetSetting(t, store, "max_upload"); got != "1024" {
		t.Fatalf("max_upload was partially updated to %q", got)
	}
}

func TestDashboardSettingsRejectsDomainThatWouldLockOutAdminsBeforeWriting(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.CreateAccount(context.Background(), "admin@example.com", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "max_upload", "1024"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}
	form := url.Values{
		"csrf":                        {"csrf-token"},
		authAllowedEmailDomainSetting: {"other.com"},
		"max_upload":                  {"2048"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(account.ID, 10), sessionTTL)})
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-token"})
	rec := httptest.NewRecorder()

	s.handleDashboardSettings(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := mustGetSetting(t, store, "max_upload"); got != "1024" {
		t.Fatalf("max_upload was partially updated to %q", got)
	}
}

func TestLoginWithPasswordRejectsDisallowedDomain(t *testing.T) {
	s := newTestServer(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateAccountWithPassword(context.Background(), "user@other.com", "User", string(hash), false); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetSetting(context.Background(), authAllowedEmailDomainSetting, "example.com"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{
		"email":    {"user@other.com"},
		"password": {"password"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, err = s.loginWithPassword(req)
	if !errors.Is(err, errAccountNotEligible) {
		t.Fatalf("error = %v", err)
	}
}

func TestLoginWithTokenRejectsDisallowedDomain(t *testing.T) {
	s := newTestServer(t)
	account, err := s.store.CreateAccount(context.Background(), "user@other.com", "User", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateTokenForAccount(context.Background(), "raw-token", account.ID, "web token"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetSetting(context.Background(), authAllowedEmailDomainSetting, "example.com"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"token": {"raw-token"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, err = s.loginWithToken(req)
	if !errors.Is(err, errAccountNotEligible) {
		t.Fatalf("error = %v", err)
	}
}

func TestDirectAPITokenAuthIgnoresAllowedEmailDomain(t *testing.T) {
	store := newAuthTestStore(t, "automation-token", "automation", false)
	if err := store.SetSetting(context.Background(), authAllowedEmailDomainSetting, "example.com"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}
	called := false
	handler := s.authToken(func(w http.ResponseWriter, r *http.Request) {
		called = true
		jsonOK(w, map[string]string{"status": "ok"})
	})
	req := httptest.NewRequest(http.MethodGet, "/api/uploads", nil)
	req.Header.Set("Authorization", "Bearer automation-token")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestResolveOAuthAccountRejectsDisallowedDomain(t *testing.T) {
	s := newTestServer(t)
	if err := s.store.SetSetting(context.Background(), authAllowedEmailDomainSetting, "example.com"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	_, err := s.resolveOAuthAccount(req, &oauthProfile{
		Provider:       "google",
		ProviderUserID: "sub-1",
		Email:          "user@other.com",
		EmailVerified:  true,
		Name:           "User",
	})
	if !errors.Is(err, errAccountNotEligible) {
		t.Fatalf("error = %v", err)
	}
}

func TestWebAuthRejectsExistingSessionAfterDomainChange(t *testing.T) {
	s, store, accountID := newAdminTestServer(t)
	if err := store.SetSetting(context.Background(), authAllowedEmailDomainSetting, "other.com"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(accountID, 10), sessionTTL)})

	if _, ok := s.webAuth(req); ok {
		t.Fatal("webAuth accepted an off-domain existing session")
	}
}

func TestDashboardCreateInviteRejectsDisallowedDomain(t *testing.T) {
	s, store, accountID := newAdminTestServer(t)
	if err := store.SetSetting(context.Background(), authAllowedEmailDomainSetting, "example.test"); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf": {"csrf-token"}, "email": {"user@other.test"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/invites", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(accountID, 10), sessionTTL)})
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-token"})
	rec := httptest.NewRecorder()

	s.handleDashboardCreateInvite(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "email+must+match+allowed+domain") {
		t.Fatalf("location = %q", loc)
	}
}

func TestCLILoginPollRejectsDisallowedDomain(t *testing.T) {
	s := newTestServer(t)
	account, err := s.store.CreateAccount(context.Background(), "user@other.com", "User", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetSetting(context.Background(), authAllowedEmailDomainSetting, "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateCLILoginDevice(context.Background(), "device-code", "ABCDEFGH", time.Now().Add(cliLoginTTL)); err != nil {
		t.Fatal(err)
	}
	device, err := s.store.GetCLILoginByDevice(context.Background(), "device-code")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.ApproveCLILogin(context.Background(), device.ID, account.ID); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleCLILoginPoll(rec, httptest.NewRequest(http.MethodPost, "/api/cli/login/poll", strings.NewReader(`{"device_code":"device-code"}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"denied"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
