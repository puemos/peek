package server

import (
	"net/http"

	webui "github.com/puemos/peek/internal/web"
)

func (s *Server) handleBridge(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "bridge.js")
}

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "app.js")
}

func (s *Server) handleDashboardJS(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "dashboard.js")
}

func (s *Server) handleStyle(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "style.css")
}

func (s *Server) handleDashboardCSS(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "dashboard.css")
}
