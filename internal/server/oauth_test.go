package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/puemos/peek/internal/db"
	webui "github.com/puemos/peek/internal/web"
)

const testSecret = "0000000000000000000000000000000000000000000000000000000000000000"

func newTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := webui.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &Server{
		store:           store,
		secret:          testSecret,
		baseURL:         "http://peek.test",
		renderer:        renderer,
		loginLimiter:    newLimiter(100, time.Minute),
		commentLimiter:  newLimiter(100, time.Minute),
		cliLoginLimiter: newLimiter(100, time.Minute),
	}
}

func TestResolveOAuthAccountConsumesMatchingInvite(t *testing.T) {
	s := newTestServer(t)
	raw := "invite-token"
	ciphertext, err := encryptSecret(s.secret, raw)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.store.CreateAccount("", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := s.store.CreateInvite(raw, ciphertext, "User@Example.COM", admin.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: inviteCookie, Value: raw})

	account, err := s.resolveOAuthAccount(req, &oauthProfile{
		Provider:       "google",
		ProviderUserID: "sub-1",
		Email:          "user@example.com",
		EmailVerified:  true,
		Name:           "User",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Email != "user@example.com" || account.IsAdmin {
		t.Fatalf("unexpected account: %+v", account)
	}
	used, err := s.store.GetInviteByID(inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if used.UsedAt.IsZero() {
		t.Fatal("invite was not consumed")
	}
}

func TestResolveOAuthAccountLinksExistingVerifiedEmailWithoutInvite(t *testing.T) {
	s := newTestServer(t)
	account, err := s.store.CreateAccount("user@example.com", "Existing", false)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	got, err := s.resolveOAuthAccount(req, &oauthProfile{
		Provider:       "github",
		ProviderUserID: "123",
		Email:          "USER@example.com",
		EmailVerified:  true,
		Name:           "Other",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != account.ID {
		t.Fatalf("expected account %d, got %d", account.ID, got.ID)
	}
}

func TestResolveOAuthAccountRejectsUnverifiedEmail(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := s.resolveOAuthAccount(req, &oauthProfile{
		Provider:       "google",
		ProviderUserID: "sub-1",
		Email:          "user@example.com",
		EmailVerified:  false,
		Name:           "User",
	})
	if err == nil || !strings.Contains(err.Error(), "verified") {
		t.Fatalf("expected verified email error, got %v", err)
	}
}

func TestOAuthStartErrorRendersLoginPage(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/oauth/google/start", nil)
	req.SetPathValue("provider", "google")
	rec := httptest.NewRecorder()

	s.handleOAuthStart(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != webui.DashboardCSP {
		t.Fatalf("csp = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("cache-control = %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "OAuth provider is not configured.") {
		t.Fatalf("rendered page did not include oauth error: %s", body)
	}
	if !strings.Contains(body, "Sign in") {
		t.Fatalf("expected login template, got: %s", body)
	}
}

func TestOAuthAccountErrorMessageHidesInternalFailures(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{err: errors.New("OAuth account must have a verified email"), want: "OAuth account must have a verified email."},
		{err: errors.New("account disabled"), want: "This account is disabled."},
		{err: errors.New("invite required"), want: "An invite is required for this account."},
		{err: errors.New("invite not found"), want: "This invite is invalid or expired."},
		{err: errors.New("account lookup failed: driver timeout"), want: "OAuth account could not be linked."},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			if got := oauthAccountErrorMessage(tc.err); got != tc.want {
				t.Fatalf("message = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCLILoginPollIssuesTokenOnce(t *testing.T) {
	s := newTestServer(t)
	account, err := s.store.CreateAccount("user@example.com", "User", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateCLILoginDevice("device-code", "ABCDEFGH", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	device, err := s.store.GetCLILoginByDevice("device-code")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.ApproveCLILogin(device.ID, account.ID); err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"device_code":"device-code"}`)
	rec := httptest.NewRecorder()
	s.handleCLILoginPoll(rec, httptest.NewRequest(http.MethodPost, "/api/cli/login/poll", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "approved" || out.Token == "" {
		t.Fatalf("unexpected poll response: %+v", out)
	}
	if _, err := s.store.GetToken(out.Token); err != nil {
		t.Fatalf("issued token should authenticate: %v", err)
	}

	rec = httptest.NewRecorder()
	s.handleCLILoginPoll(rec, httptest.NewRequest(http.MethodPost, "/api/cli/login/poll", strings.NewReader(`{"device_code":"device-code"}`)))
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "consumed" {
		t.Fatalf("second poll should be consumed, got %+v", out)
	}
}

func TestCLILoginStartDoesNotRequireOAuth(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.store.CreateAccountWithPassword("admin@example.com", "Admin", "hash", true); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleCLILoginStart(rec, httptest.NewRequest(http.MethodPost, "/api/cli/login/start", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.DeviceCode == "" || out.UserCode == "" || out.VerificationURL == "" {
		t.Fatalf("unexpected start response: %+v", out)
	}
}
