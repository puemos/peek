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

func (s *Server) handleDashboardAlpine(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "dashboard-alpine.js")
}

func (s *Server) handleToast(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "toast.js")
}

func (s *Server) handleAlpine(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "alpine.min.js")
}

func (s *Server) handlePines(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "pines.css")
}

func (s *Server) handleFaviconSVG(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "favicon.svg")
}

func (s *Server) handleFaviconPNG(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "favicon.png")
}

func (s *Server) handleFaviconICO(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "favicon.ico")
}

func (s *Server) handleLogoSVG(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "logo.svg")
}

func (s *Server) handleLogoPNG(w http.ResponseWriter, r *http.Request) {
	webui.ServeAsset(w, r, "logo.png")
}
