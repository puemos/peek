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
	u, err := s.store.GetUpload(r.Context(), slug)
	if err != nil {
		s.renderWebError(w, http.StatusNotFound, "Page not found", "This shared page does not exist or is no longer available.")
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
		Name: u.Name, Slug: slug,
		RawURL: rawURL, Protected: u.PasswordHash != "",
	}
	w.Header().Set("Content-Security-Policy", webui.PageCSP)
	s.renderHTML(w, http.StatusOK, webui.TemplatePage, d)
}

func (s *Server) handlePagePassword(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(r.Context(), slug)
	if err != nil {
		s.renderWebError(w, http.StatusNotFound, "Page not found", "This shared page does not exist or is no longer available.")
		return
	}
	if u.PasswordHash == "" {
		http.Redirect(w, r, "/p/"+slug, http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderWebError(w, http.StatusBadRequest, "Bad request", "The password form could not be read.")
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
	if _, err := s.store.GetUpload(r.Context(), slug); err != nil {
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
	insertOffset, err := bridgeInsertOffset(rc)
	closeErr := rc.Close()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if closeErr != nil {
		slog.Warn("close raw html scan", "slug", slug, "err", closeErr)
	}
	rc, err = s.storage.Open(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer rc.Close()
	writeRawHTML(w, slug, rc, insertOffset)
}

func writeRawHTML(w http.ResponseWriter, slug string, body io.Reader, insertOffset int64) {
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
	if insertOffset > 0 {
		if _, err := io.CopyN(w, body, insertOffset); err != nil {
			slog.Error("write raw html response", "slug", slug, "err", err)
			return
		}
	}
	if _, err := w.Write(bridgeScriptTag()); err != nil {
		slog.Error("write raw html response", "slug", slug, "err", err)
		return
	}
	if _, err := io.Copy(w, body); err != nil {
		slog.Error("write raw html response", "slug", slug, "err", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.renderWebError(w, http.StatusNotFound, "Page not found", "The requested page does not exist.")
		return
	}
	if s.setupRequired(r.Context()) {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	s.renderHTML(w, http.StatusOK, webui.TemplateIndex, webui.IndexData{BaseURL: s.baseURL})
}

// injectBridge appends the picker script so the parent can drive element
// selection over postMessage. Runs inside the sandboxed iframe.
func injectBridge(b []byte) []byte {
	offset, err := bridgeInsertOffset(bytes.NewReader(b))
	if err != nil {
		return b
	}
	tag := bridgeScriptTag()
	return append(append(append([]byte{}, b[:offset]...), tag...), b[offset:]...)
}

func bridgeScriptTag() []byte {
	return []byte(`<script src="` + webui.AssetURL("bridge.js") + `"></script>`)
}

func bridgeInsertOffset(r io.Reader) (int64, error) {
	marker := []byte("</body>")
	marker2 := []byte("</html>")
	const maxTail = len("</body>") - 1
	buf := make([]byte, 32*1024)
	tail := make([]byte, 0, maxTail)
	var (
		total    int64
		lastBody int64 = -1
		lastHTML int64 = -1
	)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			prevTotal := total
			combined := make([]byte, 0, len(tail)+n)
			combined = append(combined, tail...)
			combined = append(combined, buf[:n]...)
			combinedStart := total - int64(len(tail))
			lower := bytes.ToLower(combined)
			lastBody = lastMarkerOffset(lower, marker, combinedStart, prevTotal, lastBody)
			lastHTML = lastMarkerOffset(lower, marker2, combinedStart, prevTotal, lastHTML)
			total += int64(n)
			if len(combined) > maxTail {
				tail = append(tail[:0], combined[len(combined)-maxTail:]...)
			} else {
				tail = append(tail[:0], combined...)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	if lastBody >= 0 {
		return lastBody, nil
	}
	if lastHTML >= 0 {
		return lastHTML, nil
	}
	return total, nil
}

func lastMarkerOffset(lower, marker []byte, combinedStart, previousTotal, current int64) int64 {
	for searchFrom := 0; searchFrom < len(lower); {
		idx := bytes.Index(lower[searchFrom:], marker)
		if idx < 0 {
			return current
		}
		idx += searchFrom
		absolute := combinedStart + int64(idx)
		if absolute+int64(len(marker)) > previousTotal {
			current = absolute
		}
		searchFrom = idx + 1
	}
	return current
}
