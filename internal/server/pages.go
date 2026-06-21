package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	webui "github.com/puemos/peek/internal/web"
)

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if u.PasswordHash != "" && !s.pageAuthorized(r, u) {
		s.renderHTML(w, http.StatusOK, webui.TemplateGate, webui.GateData{Slug: slug})
		return
	}
	vid := s.visitorID(w, r)
	s.recordVisit(r, u, vid)

	rawURL := "/raw/" + slug + "?t=" + makeViewToken(s.secret, slug, vid) + "&v=" + vid
	d := pageData{
		Filename: u.Filename, Slug: slug,
		RawURL: rawURL, Protected: u.PasswordHash != "",
	}
	w.Header().Set("Content-Security-Policy", webui.PageCSP)
	s.renderHTML(w, http.StatusOK, webui.TemplatePage, d)
}

func (s *Server) handlePagePassword(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if u.PasswordHash == "" {
		http.Redirect(w, r, "/p/"+slug, http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	pw := r.FormValue("password")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pw)) != nil {
		s.renderHTML(w, http.StatusUnauthorized, webui.TemplateGate, webui.GateData{Slug: slug, Error: true})
		return
	}
	s.setCookie(w, &http.Cookie{
		Name:     authCookieName(slug),
		Value:    makeSessionCookieValue(s.secret, slug, sessionTTL),
		Path:     "/p/" + slug,
		MaxAge:   int(sessionTTL.Seconds()),
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/p/"+slug, http.StatusSeeOther)
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if _, err := s.store.GetUpload(slug); err != nil {
		http.NotFound(w, r)
		return
	}
	// Require a valid view token (issued by /p) so /raw cannot be hot-linked
	// or used to bypass the password gate. The token is bound to the visitor
	// id so a shared raw URL won't work for a different viewer.
	vid := r.URL.Query().Get("v")
	if vid == "" {
		// Fall back to cookie-based vid for backward compat.
		if c, err := r.Cookie(visitorCookie); err == nil {
			vid = c.Value
		}
	}
	if !verifyViewToken(s.secret, r.URL.Query().Get("t"), slug, vid) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	rc, err := s.storage.Open(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data = injectBridge(data)
	writeRawHTML(w, slug, data)
}

func writeRawHTML(w http.ResponseWriter, slug string, data []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Allow the user's HTML to run inline scripts/styles & load resources, but
	// only permit embedding by our own origin. The iframe sandbox already
	// gives it an opaque origin so it cannot touch the server or parent.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; "+
			"script-src 'unsafe-inline' 'unsafe-eval' https: http: data: blob:; "+
			"style-src 'unsafe-inline' https: http:; "+
			"img-src https: http: data: blob:; "+
			"media-src https: http: data: blob:; "+
			"font-src https: http: data:; "+
			"connect-src https: http:; "+
			"frame-ancestors 'self'")
	if _, err := w.Write(data); err != nil {
		slog.Error("write raw html response", "slug", slug, "err", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if s.setupRequired() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	s.renderHTML(w, http.StatusOK, webui.TemplateIndex, webui.IndexData{BaseURL: s.baseURL})
}

// injectBridge appends the picker script so the parent can drive element
// selection over postMessage. Runs inside the sandboxed iframe.
func injectBridge(b []byte) []byte {
	marker := []byte("</body>")
	tag := []byte(`<script src="` + webui.AssetURL("bridge.js") + `"></script>`)
	lower := bytes.ToLower(b)
	if idx := bytes.LastIndex(lower, marker); idx >= 0 {
		return append(append(b[:idx:idx], tag...), b[idx:]...)
	}
	marker2 := []byte("</html>")
	if idx := bytes.LastIndex(lower, marker2); idx >= 0 {
		return append(append(b[:idx:idx], tag...), b[idx:]...)
	}
	return append(b, tag...)
}
