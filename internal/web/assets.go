package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"log/slog"
	"net/http"
)

//go:embed assets/*.js assets/*.css assets/*.svg assets/*.png assets/*.ico
var assetsFS embed.FS

type embeddedAsset struct {
	path        string
	contentType string
	hash        string
}

var assetManifest = map[string]embeddedAsset{
	"bridge.js":           {path: "assets/bridge.js", contentType: "text/javascript; charset=utf-8"},
	"app.js":              {path: "assets/app.js", contentType: "text/javascript; charset=utf-8"},
	"dashboard-alpine.js": {path: "assets/dashboard-alpine.js", contentType: "text/javascript; charset=utf-8"},
	"toast.js":            {path: "assets/toast.js", contentType: "text/javascript; charset=utf-8"},
	"alpine.min.js":       {path: "assets/alpine.min.js", contentType: "text/javascript; charset=utf-8"},
	"pines.css":           {path: "assets/pines.css", contentType: "text/css; charset=utf-8"},
	"favicon.svg":         {path: "assets/favicon.svg", contentType: "image/svg+xml"},
	"favicon.png":         {path: "assets/favicon.png", contentType: "image/png"},
	"favicon.ico":         {path: "assets/favicon.ico", contentType: "image/x-icon"},
	"logo.svg":            {path: "assets/logo.svg", contentType: "image/svg+xml"},
	"logo.png":            {path: "assets/logo.png", contentType: "image/png"},
}

func init() {
	for name, asset := range assetManifest {
		b, err := assetsFS.ReadFile(asset.path)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(b)
		asset.hash = hex.EncodeToString(sum[:])[:12]
		assetManifest[name] = asset
	}
}

func AssetURL(name string) string {
	asset, ok := assetManifest[name]
	if !ok || asset.hash == "" {
		return "/" + name
	}
	return "/" + name + "?v=" + asset.hash
}

func ServeAsset(w http.ResponseWriter, r *http.Request, name string) {
	asset, ok := assetManifest[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	b, err := assetsFS.ReadFile(asset.path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", asset.contentType)
	if r.URL.Query().Get("v") == asset.hash && asset.hash != "" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	if _, err := w.Write(b); err != nil {
		slog.Warn("write asset response failed", "asset", name, "err", err)
	}
}
