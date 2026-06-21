package web

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
)

type failingAssetWriter struct {
	header http.Header
}

func (w *failingAssetWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *failingAssetWriter) WriteHeader(int) {}

func (w *failingAssetWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestAssetURLIncludesContentHash(t *testing.T) {
	got := AssetURL("dashboard.js")
	if !strings.HasPrefix(got, "/dashboard.js?v=") {
		t.Fatalf("AssetURL() = %q", got)
	}
	if got == AssetURL("missing.js") {
		t.Fatalf("known and missing asset URLs should differ")
	}
}

func TestAssetManifestCoversEmbeddedAssets(t *testing.T) {
	entries, err := assetsFS.ReadDir("assets")
	if err != nil {
		t.Fatalf("read embedded assets: %v", err)
	}
	embedded := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		embedded[entry.Name()] = true
	}

	for name, asset := range assetManifest {
		if !embedded[name] {
			t.Fatalf("manifest includes %q, but no embedded asset with that name exists", name)
		}
		if path.Base(asset.path) != name {
			t.Fatalf("manifest key %q points at %q", name, asset.path)
		}
		if asset.contentType == "" {
			t.Fatalf("manifest asset %q has no content type", name)
		}
		if asset.hash == "" {
			t.Fatalf("manifest asset %q has no content hash", name)
		}
		if _, err := assetsFS.ReadFile(asset.path); err != nil {
			t.Fatalf("manifest asset %q cannot be read from %q: %v", name, asset.path, err)
		}
		delete(embedded, name)
	}

	for name := range embedded {
		t.Fatalf("embedded asset %q is not exposed through assetManifest", name)
	}
}

func TestServeAssetCacheHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, AssetURL("dashboard.js"), nil)
	rec := httptest.NewRecorder()

	ServeAsset(rec, req, "dashboard.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("cache-control = %q", got)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected asset body")
	}
}

func TestServeAssetNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing.js", nil)

	ServeAsset(rec, req, "missing.js")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestServeAssetLogsWriteFailure(t *testing.T) {
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	w := &failingAssetWriter{}
	req := httptest.NewRequest(http.MethodGet, AssetURL("dashboard.js"), nil)

	ServeAsset(w, req, "dashboard.js")

	if got := w.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if !strings.Contains(logs.String(), "write asset response failed") {
		t.Fatalf("write failure was not logged: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "asset=dashboard.js") {
		t.Fatalf("asset name was not logged: %s", logs.String())
	}
}

func TestParentAppMessageHandlerChecksFrameSource(t *testing.T) {
	b, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "if (e.source !== frame.contentWindow) return;") {
		t.Fatal("parent message handler must reject messages not sent by the sandbox iframe")
	}
}
