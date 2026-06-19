package server

import (
	"embed"
	"net/http"
)

//go:embed assets/*.js assets/*.css
var assetsFS embed.FS

func (s *Server) handleBridge(w http.ResponseWriter, r *http.Request) {
	s.serveAsset(w, r, "assets/bridge.js", "text/javascript; charset=utf-8")
}

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	s.serveAsset(w, r, "assets/app.js", "text/javascript; charset=utf-8")
}

func (s *Server) handleStyle(w http.ResponseWriter, r *http.Request) {
	s.serveAsset(w, r, "assets/style.css", "text/css; charset=utf-8")
}

func (s *Server) handleDashboardCSS(w http.ResponseWriter, r *http.Request) {
	s.serveAsset(w, r, "assets/dashboard.css", "text/css; charset=utf-8")
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, name, ct string) {
	b, err := assetsFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(b)
}
