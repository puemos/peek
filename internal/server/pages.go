package server

import (
	"bytes"
	"html/template"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{.Filename}} &mdash; Peek</title>
<link rel="stylesheet" href="/style.css">
</head>
<body data-slug="{{.Slug}}" data-protected="{{.Protected}}">
<iframe id="hn-frame" src="{{.RawURL}}" sandbox="allow-scripts allow-popups"
        referrerpolicy="no-referrer" title="shared page"></iframe>

<div id="hn-hint" hidden>
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 11l3 3L22 4"/><path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/></svg>
  Click any element to comment
  <span class="hn-hint-sep">or</span>
  <button type="button" id="hn-hint-general">comment on the page</button>
  <kbd>Esc</kbd>
</div>

<div id="hn-bar">
  <button id="hn-comment-btn" type="button" title="Add a comment">
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/></svg>
    Comment
  </button>
  <button id="hn-panel-btn" class="hn-icon-btn" type="button" title="View comments" aria-label="View comments">
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>
    <span id="hn-count" class="hn-badge">0</span>
  </button>
</div>

<aside id="hn-panel" class="hn-panel">
  <header>
    <h3>Comments</h3>
    <div class="hn-panel-actions">
      <button id="hn-export-md" type="button" title="Copy all comments as Markdown" hidden>Copy MD</button>
      <button id="hn-panel-close" type="button">&times;</button>
    </div>
  </header>
  <ul id="hn-comment-list"></ul>
</aside>

<div id="hn-composer" class="hn-composer" hidden>
  <div class="hn-target" id="hn-target"></div>
  <form id="hn-comment-form">
    <textarea id="hn-body" placeholder="Write a comment&hellip;" maxlength="4000" required></textarea>
    <p class="hn-error" id="hn-error" hidden></p>
    <div class="hn-composer-actions">
      <button type="submit">Post</button>
      <button type="button" id="hn-cancel">Cancel</button>
      <span class="hn-commenting-as" id="hn-commenting-as"></span>
    </div>
  </form>
</div>

<div id="hn-name-modal" class="hn-modal" hidden>
  <div class="hn-modal-card">
    <h2>Add your name</h2>
    <p>It shows next to comments you leave. You can skip this.</p>
    <form id="hn-name-form">
      <input id="hn-name-input" type="text" placeholder="Your name" maxlength="100" autocomplete="name">
      <div class="hn-modal-actions">
        <button type="submit">Continue</button>
        <button type="button" id="hn-name-skip">Skip</button>
      </div>
    </form>
  </div>
</div>

<script src="/app.js"></script>
</body>
</html>`))

var gateTmpl = template.Must(template.New("gate").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Password required &mdash; Peek</title>
<link rel="stylesheet" href="/style.css">
</head>
<body class="hn-gate">
<div class="hn-gate-card">
<form method="POST" action="/p/{{.Slug}}" class="hn-gate-form">
  <h2>This page is protected</h2>
  <p>Enter the password to view this page.</p>
  <input type="password" name="password" placeholder="Password" required autofocus>
  <button type="submit">Unlock</button>
  {{if .Error}}<p class="hn-err">Incorrect password.</p>{{end}}
</form>
</div>
</body>
</html>`))

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Peek</title>
<link rel="stylesheet" href="/style.css">
</head>
<body>
<div class="hn-welcome">
  <div class="hn-welcome-box">
    <h1>Peek</h1>
    <p>Share HTML files and get a live preview link.<br>Upload via the dashboard or the CLI.</p>
    <div class="hn-welcome-actions">
      <a href="/dashboard" class="hn-welcome-primary">Open dashboard &rarr;</a>
      <a href="/login" class="hn-welcome-secondary">Sign in</a>
    </div>
    <div class="hn-welcome-code">
      <strong>CLI quick start:</strong><br>
      <code>peek login --host {{.BaseURL}}</code><br>
      <code>peek upload page.html</code>
    </div>
  </div>
</div>
</body>
</html>`))

type pageData struct {
	Filename  string
	Slug      string
	RawURL    string
	Protected bool
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if u.PasswordHash != "" && !s.pageAuthorized(r, u) {
		gateTmpl.Execute(w, map[string]any{"Slug": slug, "Error": false})
		return
	}
	vid := s.visitorID(w, r)
	go s.recordVisit(r, u, vid)

	rawURL := "/raw/" + slug + "?t=" + makeViewToken(s.secret, slug)
	d := pageData{
		Filename: u.Filename, Slug: slug,
		RawURL: rawURL, Protected: u.PasswordHash != "",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; frame-src 'self'")
	pageTmpl.Execute(w, d)
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
		w.WriteHeader(http.StatusUnauthorized)
		gateTmpl.Execute(w, map[string]any{"Slug": slug, "Error": true})
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
	// or used to bypass the password gate.
	if !verifyToken(s.secret, r.URL.Query().Get("t"), slug) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	data, err := s.readUploadFile(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data = injectBridge(data)
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
	w.Write(data)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexTmpl.Execute(w, map[string]any{"BaseURL": s.baseURL})
}

// injectBridge appends the picker script so the parent can drive element
// selection over postMessage. Runs inside the sandboxed iframe.
func injectBridge(b []byte) []byte {
	marker := []byte("</body>")
	tag := []byte(`<script src="/bridge.js"></script>`)
	if idx := bytes.LastIndex(b, marker); idx >= 0 {
		return append(append(b[:idx:idx], tag...), b[idx:]...)
	}
	marker2 := []byte("</html>")
	if idx := bytes.LastIndex(b, marker2); idx >= 0 {
		return append(append(b[:idx:idx], tag...), b[idx:]...)
	}
	return append(b, tag...)
}
