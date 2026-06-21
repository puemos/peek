package server_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/server"
	"github.com/puemos/peek/internal/uploadquota"
)

func newTestServer(t *testing.T) (*server.Server, string, string) {
	t.Helper()
	dir := t.TempDir()
	adminToken := "dev-admin-token"
	store, err := db.Open(filepath.Join(dir, "peek.db"))
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	if err := store.CreateToken(adminToken, "admin", true, 0); err != nil {
		t.Fatalf("seed admin token: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close test store: %v", err)
	}
	srv, err := server.New(server.Config{
		DataDir:   dir,
		BaseURL:   "http://localhost:7700",
		Secret:    strings.Repeat("ab", 32), // 64 hex chars = 32 bytes
		MaxUpload: 10 << 20,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, adminToken, dir
}

func TestHealthEndpoints(t *testing.T) {
	app := newTestApp(t)

	resp := app.request(t, http.MethodGet, "/healthz", nil)
	if resp.StatusCode != http.StatusOK || string(resp.Body) != "ok" {
		t.Fatalf("healthz: %d %s", resp.StatusCode, resp.Body)
	}

	resp = app.request(t, http.MethodGet, "/readyz", nil)
	if resp.StatusCode != http.StatusOK || string(resp.Body) != "ready" {
		t.Fatalf("readyz: %d %s", resp.StatusCode, resp.Body)
	}
}

func TestAuthRejectionWithoutToken(t *testing.T) {
	app := newTestApp(t)

	resp := app.request(t, http.MethodGet, "/api/uploads", nil)
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestDisabledTokenCannotReadOwnedComments(t *testing.T) {
	dir := t.TempDir()
	userToken := "user-token"
	store, err := db.Open(filepath.Join(dir, "peek.db"))
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	if err := store.CreateToken(userToken, "user", false, 0); err != nil {
		t.Fatalf("seed user token: %v", err)
	}
	owner, err := store.GetToken(userToken)
	if err != nil {
		t.Fatalf("get user token: %v", err)
	}
	if err := store.CreateUploadChecked("owned-page", owner.AccountID, owner.ID, "page.html", 42, "", uploadquota.Limits{}); err != nil {
		t.Fatalf("seed upload: %v", err)
	}
	upload, err := store.GetUpload("owned-page")
	if err != nil {
		t.Fatalf("get upload: %v", err)
	}
	if err := store.AddComment(upload.ID, "", "", "Ada", "visitor", "Looks good"); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	if err := store.SetAccountDisabled(owner.AccountID, true); err != nil {
		t.Fatalf("disable account: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close test store: %v", err)
	}

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
	t.Cleanup(ts.Close)
	app := testApp{
		URL:    ts.URL,
		client: ts.Client(),
	}

	resp := app.request(t, http.MethodGet, "/api/uploads/owned-page/comments", nil, withAuth(userToken))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected disabled account to be forbidden, got %d %s", resp.StatusCode, resp.Body)
	}
}

func TestUploadRejectsUnsupportedContentType(t *testing.T) {
	app := newTestApp(t)

	resp := app.requestString(t, http.MethodPost, "/api/upload", "<!DOCTYPE html><html></html>", withAuth(app.AdminToken), withContentType("application/json"))
	assertStatus(t, resp, http.StatusUnsupportedMediaType)
	out := decodeResponseJSON[map[string]string](t, resp)
	if out["error"] != "unsupported content type" {
		t.Fatalf("response = %+v", out)
	}
}

func TestUploadAcceptsHTMLContentTypeWithParameters(t *testing.T) {
	app := newTestApp(t)

	resp := app.requestString(t, http.MethodPost, "/api/upload", "<!DOCTYPE html><html><body></body></html>", withAuth(app.AdminToken), withContentType("text/html; charset=utf-8"))
	assertStatus(t, resp, http.StatusOK)
}

func TestUploadAndListAndDelete(t *testing.T) {
	app := newTestApp(t)

	resp := app.requestString(t, http.MethodPost, "/api/upload", "<!DOCTYPE html><html><body></body></html>", withAuth(app.AdminToken), withContentType("text/html"))
	assertStatus(t, resp, http.StatusOK)
	up := decodeResponseJSON[struct {
		Slug string `json:"slug"`
		URL  string `json:"url"`
	}](t, resp)
	if up.Slug == "" || !strings.Contains(up.URL, up.Slug) {
		t.Fatalf("unexpected upload response: %+v", up)
	}

	// Verify file exists on disk in the data dir uploads folder.
	path := filepath.Join(app.DataDir, "uploads", up.Slug+".html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be saved: %v", err)
	}
	if !strings.Contains(string(data), "<html") {
		t.Fatalf("unexpected file contents: %s", data)
	}

	resp = app.request(t, http.MethodGet, "/api/uploads", nil, withAuth(app.AdminToken))
	assertStatus(t, resp, http.StatusOK)
	list := decodeResponseJSON[[]struct {
		Slug     string `json:"slug"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}](t, resp)
	if len(list) != 1 || list[0].Slug != up.Slug {
		t.Fatalf("expected one upload with slug %q, got %+v", up.Slug, list)
	}

	resp = app.request(t, http.MethodDelete, "/api/uploads/"+up.Slug, nil, withAuth(app.AdminToken))
	assertStatus(t, resp, http.StatusOK)
	del := decodeResponseJSON[map[string]any](t, resp)
	if del["deleted"] != up.Slug {
		t.Fatalf("expected deleted slug, got %+v", del)
	}
}

func TestInternalReviewWorkflow(t *testing.T) {
	app := newTestApp(t)

	resp := app.requestString(t, http.MethodPost, "/api/upload?filename=review.html", "<!DOCTYPE html><html><body><h1 id=\"hero\">Hello</h1></body></html>", withAuth(app.AdminToken), withContentType("text/html; charset=utf-8"))
	assertStatus(t, resp, http.StatusOK)
	up := decodeResponseJSON[struct {
		Slug string `json:"slug"`
		URL  string `json:"url"`
	}](t, resp)
	if up.Slug == "" {
		t.Fatalf("missing slug in upload response: %+v", up)
	}

	pageResp := app.request(t, http.MethodGet, "/p/"+up.Slug, nil)
	assertStatus(t, pageResp, http.StatusOK)
	if !strings.Contains(string(pageResp.Body), "/raw/"+up.Slug) {
		t.Fatalf("page did not reference raw iframe URL: %s", pageResp.Body)
	}

	resp = app.requestString(t, http.MethodPost, "/api/uploads/"+up.Slug+"/comments", `{"name":"Ada","body":"Looks good","selector":"#hero","element_text":"Hello"}`, withContentType("application/json"), withCookies(pageResp.Cookies...))
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(string(resp.Body), `"author":"Ada"`) || !strings.Contains(string(resp.Body), `"selector":"#hero"`) {
		t.Fatalf("comment response missing saved comment: %s", resp.Body)
	}

	resp = app.request(t, http.MethodGet, "/api/uploads/"+up.Slug+"/comments", nil, withAuth(app.AdminToken))
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(string(resp.Body), `"body":"Looks good"`) {
		t.Fatalf("owner comments response missing comment: %s", resp.Body)
	}

	app.flushVisits(t)
	resp = app.request(t, http.MethodGet, "/api/uploads/"+up.Slug+"/export", nil, withAuth(app.AdminToken))
	assertStatus(t, resp, http.StatusOK)
	export := decodeResponseJSON[workflowExport](t, resp)
	if export.Filename != "review.html" || export.TotalVisits < 1 || len(export.Comments) != 1 {
		t.Fatalf("unexpected export: %+v", export)
	}
	if export.Comments[0].Author != "Ada" || export.Comments[0].Body != "Looks good" {
		t.Fatalf("unexpected exported comment: %+v", export.Comments)
	}

	resp = app.request(t, http.MethodDelete, "/api/uploads/"+up.Slug, nil, withAuth(app.AdminToken))
	assertStatus(t, resp, http.StatusOK)
	if _, err := os.Stat(filepath.Join(app.DataDir, "uploads", up.Slug+".html")); !os.IsNotExist(err) {
		t.Fatalf("uploaded object should be removed, stat err=%v", err)
	}
}

type workflowExport struct {
	Filename    string `json:"filename"`
	TotalVisits int    `json:"total_visits"`
	Comments    []struct {
		Author string `json:"author"`
		Body   string `json:"body"`
	} `json:"comments"`
}

func TestCreateTokenWithExpiryAndList(t *testing.T) {
	app := newTestApp(t)

	resp := app.requestJSON(t, http.MethodPost, "/api/tokens", map[string]any{"name": "service", "expires_hours": 1}, withAuth(app.AdminToken))
	assertStatus(t, resp, http.StatusOK)
	tok := decodeResponseJSON[struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}](t, resp)
	if tok.Token == "" || tok.Name != "service" {
		t.Fatalf("unexpected token response: %+v", tok)
	}

	resp = app.request(t, http.MethodGet, "/api/tokens", nil, withAuth(app.AdminToken))
	assertStatus(t, resp, http.StatusOK)
	tokens := decodeResponseJSON[[]struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Admin     bool   `json:"admin"`
		ExpiresAt int64  `json:"expires_at"`
	}](t, resp)
	found := false
	for _, t := range tokens {
		if t.Name == "service" && !t.Admin && t.ExpiresAt > time.Now().Unix() {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected service token in list with future expiry, got %+v", tokens)
	}
}

func TestAuditLogRetrieval(t *testing.T) {
	app := newTestApp(t)

	// Trigger an audited event by creating a token.
	resp := app.requestJSON(t, http.MethodPost, "/api/tokens", map[string]any{"name": "audit-token"}, withAuth(app.AdminToken))
	assertStatus(t, resp, http.StatusOK)

	resp = app.request(t, http.MethodGet, "/api/audit", nil, withAuth(app.AdminToken))
	assertStatus(t, resp, http.StatusOK)
	entries := decodeResponseJSON[[]struct {
		Actor  string `json:"actor"`
		Action string `json:"action"`
		IP     string `json:"ip"`
	}](t, resp)
	if len(entries) == 0 {
		t.Fatalf("expected audit entries, got none")
	}
	found := false
	for _, e := range entries {
		if e.Action == "token.create" {
			found = true
		}
		if e.IP == "" {
			t.Fatalf("expected audit entry to have IP, got %+v", e)
		}
	}
	if !found {
		t.Fatalf("expected token.create audit entry, got %+v", entries)
	}
}
