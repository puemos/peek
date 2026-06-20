package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/server"
)

func TestFreshInstallSetupCreatesAdminPassword(t *testing.T) {
	dir := t.TempDir()
	srv, err := server.New(server.Config{
		DataDir:   dir,
		BaseURL:   "http://localhost:7700",
		Secret:    strings.Repeat("ab", 32),
		MaxUpload: 10 << 20,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	client := noRedirectClient()

	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("index request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/setup" {
		t.Fatalf("expected setup redirect, got %d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}

	codeBytes, err := os.ReadFile(filepath.Join(dir, "setup.key"))
	if err != nil {
		t.Fatalf("read setup code: %v", err)
	}
	code := strings.TrimSpace(string(codeBytes))
	resp, err = client.Get(ts.URL + "/setup?code=" + url.QueryEscape(code))
	if err != nil {
		t.Fatalf("setup get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	csrf := hiddenValue(t, string(body), "csrf")
	csrfCookie := findCookie(resp.Cookies(), "hn_csrf")
	if csrfCookie == nil {
		t.Fatal("setup form did not set csrf cookie")
	}

	form := url.Values{
		"email":    {"admin@example.com"},
		"name":     {"Admin"},
		"password": {"correct horse battery staple"},
		"code":     {code},
		"csrf":     {csrf},
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("setup post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/dashboard" {
		t.Fatalf("expected dashboard redirect, got %d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if findCookie(resp.Cookies(), "hn_session") == nil {
		t.Fatal("setup did not create a web session")
	}
	if _, err := os.Stat(filepath.Join(dir, "setup.key")); !os.IsNotExist(err) {
		t.Fatalf("setup code should be removed, stat err=%v", err)
	}

	store, err := db.Open(filepath.Join(dir, "peek.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	account, err := store.GetAccountByEmail("admin@example.com")
	if err != nil {
		t.Fatalf("get admin account: %v", err)
	}
	if !account.IsAdmin || account.PasswordHash == "" {
		t.Fatalf("expected password-backed admin, got %+v", account)
	}
	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte("correct horse battery staple")) != nil {
		t.Fatal("stored password hash does not verify")
	}
}

func TestOAuthEnabledRejectsNonAdminTokenWebLogin(t *testing.T) {
	srv, adminToken, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	client := noRedirectClient()

	userToken := createAPIToken(t, ts.URL, adminToken, "user")
	updateSettings(t, ts.URL, adminToken, map[string]string{
		"oauth_github_enabled":       "true",
		"oauth_github_client_id":     "client-id",
		"oauth_github_client_secret": "client-secret",
	})

	resp, err := client.Get(ts.URL + "/login")
	if err != nil {
		t.Fatalf("login get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), "Access token") {
		t.Fatal("token login form should be hidden when OAuth is enabled")
	}
	csrf := hiddenValue(t, string(body), "csrf")
	csrfCookie := findCookie(resp.Cookies(), "hn_csrf")
	if csrfCookie == nil {
		t.Fatal("login form did not set csrf cookie")
	}

	form := url.Values{"method": {"token"}, "token": {userToken}, "csrf": {csrf}}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("login post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected failed login page, got %d", resp.StatusCode)
	}
	if findCookie(resp.Cookies(), "hn_session") != nil {
		t.Fatal("non-admin token login should not create a web session")
	}
}

func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func hiddenValue(t *testing.T, body, name string) string {
	t.Helper()
	re := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `" value="([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if len(m) != 2 {
		t.Fatalf("hidden field %q not found in body: %s", name, body)
	}
	return m[1]
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func createAPIToken(t *testing.T, host, adminToken, name string) string {
	t.Helper()
	body := strings.NewReader(`{"name":"` + name + `"}`)
	req, _ := http.NewRequest(http.MethodPost, host+"/api/tokens", body)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("create token failed: %d %s", resp.StatusCode, data)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if out.Token == "" {
		t.Fatal("empty token")
	}
	return out.Token
}

func updateSettings(t *testing.T, host, adminToken string, settings map[string]string) {
	t.Helper()
	var buf strings.Builder
	if err := json.NewEncoder(&buf).Encode(settings); err != nil {
		t.Fatalf("encode settings: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPut, host+"/api/settings", strings.NewReader(buf.String()))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("update settings failed: %d %s", resp.StatusCode, data)
	}
}
