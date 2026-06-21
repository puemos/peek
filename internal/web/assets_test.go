package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetURLIncludesContentHash(t *testing.T) {
	got := AssetURL("dashboard.js")
	if !strings.HasPrefix(got, "/dashboard.js?v=") {
		t.Fatalf("AssetURL() = %q", got)
	}
	if got == AssetURL("missing.js") {
		t.Fatalf("known and missing asset URLs should differ")
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

func TestParentAppMessageHandlerChecksFrameSource(t *testing.T) {
	b, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "if (e.source !== frame.contentWindow) return;") {
		t.Fatal("parent message handler must reject messages not sent by the sandbox iframe")
	}
}
