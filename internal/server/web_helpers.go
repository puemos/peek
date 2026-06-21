package server

import (
	"net/http"
	"net/url"
)

const sessionCookie = "hn_session"

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
}

func dashboardError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/dashboard?err="+url.QueryEscape(msg), http.StatusSeeOther)
}
