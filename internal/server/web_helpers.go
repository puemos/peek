package server

import (
	"net/http"
	"net/url"
)

const sessionCookie = "hn_session"
const dashboardPath = "/dashboard"

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
}

func dashboardError(w http.ResponseWriter, r *http.Request, msg string) {
	dashboardFlash(w, r, "err", msg)
}

func dashboardOK(w http.ResponseWriter, r *http.Request, msg string) {
	dashboardFlash(w, r, "ok", msg)
}

func dashboardUploaded(w http.ResponseWriter, r *http.Request, shareURL string) {
	dashboardFlash(w, r, "uploaded", shareURL)
}

func dashboardHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, dashboardPath, http.StatusSeeOther)
}

func dashboardFlash(w http.ResponseWriter, r *http.Request, key, msg string) {
	q := url.Values{key: {msg}}
	http.Redirect(w, r, dashboardPath+"?"+q.Encode(), http.StatusSeeOther)
}

func (s *Server) parseDashboardForm(w http.ResponseWriter, r *http.Request) bool {
	if err := r.ParseForm(); err != nil {
		dashboardError(w, r, "invalid session")
		return false
	}
	if !s.validateCSRF(r, w, r.FormValue("csrf")) {
		dashboardError(w, r, "invalid session")
		return false
	}
	return true
}
