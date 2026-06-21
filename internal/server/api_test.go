package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/server"
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
	srv, _, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("healthz: %d %s", resp.StatusCode, body)
	}

	resp, err = http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("readyz request: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ready" {
		t.Fatalf("readyz: %d %s", resp.StatusCode, body)
	}
}

func TestAuthRejectionWithoutToken(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/uploads")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
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
	if err := store.CreateUpload("owned-page", owner.AccountID, owner.ID, "page.html", 42, ""); err != nil {
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
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/uploads/owned-page/comments", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("comments request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected disabled account to be forbidden, got %d %s", resp.StatusCode, body)
	}
}

func TestUploadAndListAndDelete(t *testing.T) {
	srv, adminToken, dir := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	uploadReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/upload", bytes.NewReader([]byte("<!DOCTYPE html><html><body></body></html>")))
	uploadReq.Header.Set("Authorization", "Bearer "+adminToken)
	uploadReq.Header.Set("Content-Type", "text/html")
	resp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload failed: %d %s", resp.StatusCode, body)
	}
	var up struct {
		Slug string `json:"slug"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(body, &up); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if up.Slug == "" || !strings.Contains(up.URL, up.Slug) {
		t.Fatalf("unexpected upload response: %+v", up)
	}

	// Verify file exists on disk in the data dir uploads folder.
	path := filepath.Join(dir, "uploads", up.Slug+".html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be saved: %v", err)
	}
	if !strings.Contains(string(data), "<html") {
		t.Fatalf("unexpected file contents: %s", data)
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/uploads", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list failed: %d %s", resp.StatusCode, body)
	}
	var list []struct {
		Slug     string `json:"slug"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].Slug != up.Slug {
		t.Fatalf("expected one upload with slug %q, got %+v", up.Slug, list)
	}

	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/uploads/"+up.Slug, nil)
	delReq.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete failed: %d %s", resp.StatusCode, body)
	}
	var del map[string]any
	if err := json.Unmarshal(body, &del); err != nil {
		t.Fatalf("decode delete: %v", err)
	}
	if del["deleted"] != up.Slug {
		t.Fatalf("expected deleted slug, got %+v", del)
	}
}

func TestCreateTokenWithExpiryAndList(t *testing.T) {
	srv, adminToken, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := map[string]any{"name": "service", "expires_hours": 1}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/tokens", &buf)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create token failed: %d %s", resp.StatusCode, respBody)
	}
	var tok struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(respBody, &tok); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if tok.Token == "" || tok.Name != "service" {
		t.Fatalf("unexpected token response: %+v", tok)
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/tokens", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	respBody, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tokens failed: %d %s", resp.StatusCode, respBody)
	}
	var tokens []struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Admin     bool   `json:"admin"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(respBody, &tokens); err != nil {
		t.Fatalf("decode tokens: %v", err)
	}
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
	srv, adminToken, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Trigger an audited event by creating a token.
	body := map[string]any{"name": "audit-token"}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/tokens", &buf)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/audit", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("audit request: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit failed: %d %s", resp.StatusCode, respBody)
	}
	var entries []struct {
		Actor  string `json:"actor"`
		Action string `json:"action"`
		IP     string `json:"ip"`
	}
	if err := json.Unmarshal(respBody, &entries); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
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
