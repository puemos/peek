package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/puemos/peek/internal/db"
	webui "github.com/puemos/peek/internal/web"
)

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

func TestWriteRawHTMLLogsWriteFailure(t *testing.T) {
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	w := &failingJSONWriter{}

	writeRawHTML(w, "page", []byte("<html></html>"))

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
