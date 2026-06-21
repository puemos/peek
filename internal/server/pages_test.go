package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/uploadquota"
	webui "github.com/puemos/peek/internal/web"
	"golang.org/x/crypto/bcrypt"
)

type rawPageStorage struct {
	body      string
	openCount int
}

func (s *rawPageStorage) Save(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (s *rawPageStorage) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	s.openCount++
	return io.NopCloser(strings.NewReader(s.body)), nil
}

func (s *rawPageStorage) Delete(_ context.Context, _ string) error {
	return nil
}

func TestHandlePageMissingUploadRendersWebError(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	renderer, err := webui.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, renderer: renderer}
	req := httptest.NewRequest(http.MethodGet, "/p/missing", nil)
	req.SetPathValue("slug", "missing")
	rec := httptest.NewRecorder()

	s.handlePage(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "Page not found") {
		t.Fatalf("body did not render web error: %s", rec.Body.String())
	}
}

func TestPagePasswordCookieAuthorizesProtectedCommentAPI(t *testing.T) {
	s := newTestServer(t)
	account, err := s.store.CreateAccount(context.Background(), "owner@example.test", "Owner", false)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("lolololo"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateUploadChecked(context.Background(), "protected", account.ID, 0, "protected.html", 42, string(hash), uploadquota.Limits{}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads/protected/comments", strings.NewReader(`{"name":"Ada","body":"Looks good"}`))
	req.SetPathValue("slug", "protected")
	rec := httptest.NewRecorder()
	s.handleAddComment(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/p/protected", strings.NewReader("password=lolololo"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("slug", "protected")
	rec = httptest.NewRecorder()
	s.handlePagePassword(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("password status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var authCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == authCookieName("protected") {
			authCookie = c
			break
		}
	}
	if authCookie == nil {
		t.Fatalf("password response did not set %s cookie", authCookieName("protected"))
	}
	if authCookie.Path != "/" {
		t.Fatalf("auth cookie path = %q, want /", authCookie.Path)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/uploads/protected/comments", strings.NewReader(`{"name":"Ada","body":"Looks good"}`))
	req.SetPathValue("slug", "protected")
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	s.handleAddComment(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("comment status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"author":"Ada"`) {
		t.Fatalf("comment response missing saved author: %s", rec.Body.String())
	}
}

func TestHandleRawStreamsHTMLWithBridgeInjection(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	account, err := store.CreateAccount(context.Background(), "user@example.test", "User", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateUploadChecked(context.Background(), "page", account.ID, 0, "page.html", 42, "", uploadquota.Limits{}); err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("0", 64)
	vid := "visitor"
	storage := &rawPageStorage{body: "<HTML><BODY><h1>Hello</h1></BODY></HTML>"}
	s := &Server{store: store, storage: storage, secret: secret}
	req := httptest.NewRequest(http.MethodGet, "/raw/page?t="+makeViewToken(secret, "page", vid)+"&v="+vid, nil)
	req.SetPathValue("slug", "page")
	rec := httptest.NewRecorder()

	s.handleRaw(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if storage.openCount != 2 {
		t.Fatalf("storage open count = %d, want two-pass scan and stream", storage.openCount)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	assertBridgeBefore(t, rec.Body.String(), "</BODY>")
}

func TestInjectBridgeInsertsBeforeCaseInsensitiveBodyClose(t *testing.T) {
	got := string(injectBridge([]byte("<HTML><BODY><h1>Hello</h1></BODY></HTML>")))
	assertBridgeBefore(t, got, "</BODY>")
}

func TestInjectBridgeFallsBackToCaseInsensitiveHTMLClose(t *testing.T) {
	got := string(injectBridge([]byte("<HTML><main>Hello</main></HTML>")))
	assertBridgeBefore(t, got, "</HTML>")
}

func TestInjectBridgeAppendsToFragment(t *testing.T) {
	got := string(injectBridge([]byte("<main>Hello</main>")))
	if !strings.HasPrefix(got, "<main>Hello</main>") {
		t.Fatalf("fragment prefix changed: %s", got)
	}
	if !strings.Contains(got, `src="/bridge.js?v=`) {
		t.Fatalf("bridge script missing: %s", got)
	}
}

func TestBridgeInsertOffsetFindsMarkerAcrossReadBoundary(t *testing.T) {
	html := "<HTML><BODY><h1>Hello</h1></BODY></HTML>"
	offset, err := bridgeInsertOffset(&chunkedReader{chunks: []string{
		"<HTML><BODY><h1>Hello</h1></BO",
		"DY></HTML>",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := int64(strings.Index(strings.ToLower(html), "</body>"))
	if offset != want {
		t.Fatalf("offset = %d, want %d", offset, want)
	}
}

func TestBridgeInsertOffsetUsesLastBodyMarker(t *testing.T) {
	html := "<body></body><template></body></template></html>"
	offset, err := bridgeInsertOffset(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	want := int64(strings.LastIndex(strings.ToLower(html), "</body>"))
	if offset != want {
		t.Fatalf("offset = %d, want %d", offset, want)
	}
}

func TestWriteRawHTMLLogsWriteFailure(t *testing.T) {
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	w := &failingJSONWriter{}

	writeRawHTML(w, "page", strings.NewReader("<html></html>"), 0)

	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if !strings.Contains(logs.String(), "write raw html response") {
		t.Fatalf("write failure was not logged: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "slug=page") {
		t.Fatalf("slug was not logged: %s", logs.String())
	}
}

type chunkedReader struct {
	chunks []string
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	chunk := r.chunks[0]
	r.chunks = r.chunks[1:]
	return copy(p, chunk), nil
}

func assertBridgeBefore(t *testing.T, html, marker string) {
	t.Helper()
	bridge := strings.Index(html, `src="/bridge.js?v=`)
	if bridge < 0 {
		t.Fatalf("bridge script missing: %s", html)
	}
	closeTag := strings.Index(html, marker)
	if closeTag < 0 {
		t.Fatalf("marker %q missing: %s", marker, html)
	}
	if bridge > closeTag {
		t.Fatalf("bridge script appears after %s: %s", marker, html)
	}
}
