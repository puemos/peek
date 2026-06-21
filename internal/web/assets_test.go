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
	if !strings.Contains(string(b), "if (e.source !== els.frame.contentWindow) return;") {
		t.Fatal("parent message handler must reject messages not sent by the sandbox iframe")
	}
}

func TestBridgeMessageHandlerChecksParentSource(t *testing.T) {
	b, err := assetsFS.ReadFile("assets/bridge.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "if (e.source !== parent) return;") {
		t.Fatal("bridge message handler must reject messages not sent by the parent page")
	}
}

func TestParentAppStoresReviewerNameLocally(t *testing.T) {
	b, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if strings.Contains(src, "document.cookie") {
		t.Fatal("parent app should not persist reviewer names in cookies")
	}
	if !strings.Contains(src, `localStorage.getItem("hn_name")`) || !strings.Contains(src, `localStorage.setItem("hn_name", n)`) {
		t.Fatal("parent app should persist reviewer names in localStorage")
	}
}

func TestDashboardClipboardHasFailureFeedback(t *testing.T) {
	b, err := assetsFS.ReadFile("assets/dashboard.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, `showCopyFeedback(button, "copy failed")`) {
		t.Fatal("dashboard copy actions should show failure feedback")
	}
	if !strings.Contains(src, `document.execCommand("copy")`) {
		t.Fatal("dashboard copy actions should keep the textarea copy fallback")
	}
}

func TestRuntimeJSAvoidsHTMLStringInsertion(t *testing.T) {
	entries, err := assetsFS.ReadDir("assets")
	if err != nil {
		t.Fatalf("read embedded assets: %v", err)
	}
	banned := []string{"innerHTML", "outerHTML", "insertAdjacentHTML"}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || path.Ext(name) != ".js" {
			continue
		}
		b, err := assetsFS.ReadFile("assets/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(b)
		for _, term := range banned {
			if strings.Contains(src, term) {
				t.Fatalf("%s should build dynamic UI with DOM APIs instead of %s", name, term)
			}
		}
	}
}

func TestBridgePositionsPinsInLayerCoordinateSpace(t *testing.T) {
	b, err := assetsFS.ReadFile("assets/bridge.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if !strings.Contains(js, "function layerMetrics(layer)") || !strings.Contains(js, "hn-pin-measure") {
		t.Fatal("bridge must measure the pin layer coordinate space before placing pins")
	}
	if !strings.Contains(js, "getClientRects()") {
		t.Fatal("bridge must place text pins from actual range line boxes")
	}
	if strings.Contains(js, "window.scrollX || window.pageXOffset") {
		t.Fatal("bridge must not place pins by adding viewport rects to scroll offsets")
	}
}
