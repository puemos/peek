package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/models"
)

const (
	sessionCookie = "hn_session"
	csrfCookie    = "hn_csrf"
)

// --- templates ---

var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in &mdash; Peek</title>
<link rel="stylesheet" href="/style.css">
<link rel="stylesheet" href="/dashboard.css">
</head>
<body>
<div class="hn-welcome">
  <div class="hn-gate-card">
  <form method="POST" action="/login" class="hn-gate-form">
    <h2>Peek</h2>
    <p>Enter your access token to manage uploads.</p>
    <input type="password" name="token" placeholder="Access token" required autofocus autocomplete="off">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <button type="submit">Sign in &rarr;</button>
    {{if .Error}}<p class="hn-err">Invalid token. Try again.</p>{{end}}
  </form>
  </div>
</div>
</body>
</html>`))

var dashboardTmpl = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Dashboard &mdash; Peek</title>
<link rel="stylesheet" href="/style.css">
<link rel="stylesheet" href="/dashboard.css">
</head>
<body class="hn-dash">
<header class="hn-dash-header">
  <div class="hn-dash-header-inner">
    <a href="/" class="hn-dash-logo">Peek</a>
    <nav class="hn-dash-nav">
      <span class="hn-dash-user">{{.User}}</span>
      <form method="POST" action="/logout" class="hn-dash-logout">
        <input type="hidden" name="csrf" value="{{.CSRF}}">
        <button type="submit">Sign out</button>
      </form>
    </nav>
  </div>
</header>

<main class="hn-dash-main">
  {{if .UploadSuccess}}
  <div class="hn-flash hn-flash-ok">
    <span>Uploaded! Share link:</span>
    <code>{{.UploadSuccessURL}}</code>
    <button type="button" onclick="hnCopyLink(this, {{.UploadSuccessURLJSON}})" class="hn-flash-copy">Copy</button>
  </div>
  {{end}}
  {{if .UploadError}}
  <div class="hn-flash hn-flash-err">{{.UploadError}}</div>
  {{end}}

  <section class="hn-card">
    <h2>Upload HTML</h2>
    <form method="POST" action="/dashboard/upload" enctype="multipart/form-data" class="hn-upload-form">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="hn-tabs">
        <label><input type="radio" name="mode" value="file" checked onchange="hnToggle(this)"> Choose file</label>
        <label><input type="radio" name="mode" value="paste" onchange="hnToggle(this)"> Paste HTML</label>
      </div>
      <div id="hn-file-input">
        <label class="hn-file-drop">
          <input type="file" name="file" accept=".html,.htm,text/html">
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
          <span>Click to choose an HTML file</span>
        </label>
      </div>
      <div id="hn-paste-input" hidden>
        <textarea name="html" placeholder="<!doctype html>&#10;<html>&#10;  <body>&#10;    <h1>Hello</h1>&#10;  </body>&#10;</html>" rows="8"></textarea>
      </div>
      <div class="hn-upload-opts">
        <input type="text" name="filename" placeholder="filename (optional)">
        <input type="password" name="password" placeholder="password (optional)">
      </div>
      <button type="submit">Upload &amp; get link</button>
    </form>
  </section>

  <section class="hn-card">
    <h2>Your uploads <span class="hn-card-count">{{len .Uploads}}</span></h2>
    {{if .Uploads}}
    <div class="hn-table-wrap">
    <table class="hn-table">
      <thead><tr><th>Filename</th><th>Size</th><th>Protected</th><th>Created</th><th>Link</th><th></th></tr></thead>
      <tbody>
      {{range .Uploads}}
        <tr>
          <td class="hn-filename">{{.Filename}}</td>
          <td class="hn-muted-cell">{{.SizeHuman}}</td>
          <td>{{if .Protected}}<span class="hn-tag hn-tag-on">protected</span>{{else}}<span class="hn-tag">public</span>{{end}}</td>
          <td class="hn-muted-cell">{{.CreatedHuman}}</td>
          <td><a href="/p/{{.Slug}}" target="_blank" class="hn-link">/p/{{.Slug}}</a> <button type="button" onclick="hnCopyLink(this, &quot;/p/{{.Slug}}&quot;)" class="hn-copy-btn" title="Copy link">copy</button></td>
          <td class="hn-actions">
            <a href="/dashboard/stats/{{.Slug}}" class="hn-btn-sm">stats</a>
            <form method="POST" action="/dashboard/delete/{{.Slug}}" onsubmit="return confirm('Delete {{.Filename}}? This cannot be undone.')">
              <input type="hidden" name="csrf" value="{{$.CSRF}}">
              <button type="submit" class="hn-btn-sm hn-btn-danger">delete</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
    </div>
    {{else}}
    <div class="hn-empty">
      <p>No uploads yet.</p>
      <p class="hn-muted">Upload an HTML file or paste HTML above to get a shareable link.</p>
    </div>
    {{end}}
  </section>

  {{if .IsAdmin}}
  <section class="hn-card">
    <h2>Settings</h2>
    <form method="POST" action="/dashboard/settings" class="hn-settings-form">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="hn-settings-grid">
        {{range .SettingsMeta}}
        <label>
          <span>{{.Label}}{{if .IsStartup}} <em>(restart to apply)</em>{{end}}</span>
          <input type="{{if .IsSecret}}password{{else}}text{{end}}" name="{{.Key}}" value="{{.Value}}" placeholder="{{.Description}}" autocomplete="off">
          <span class="hn-muted">{{.Description}}</span>
        </label>
        {{end}}
      </div>
      <button type="submit">Save settings</button>
    </form>
  </section>
  {{end}}
</main>
<script>
function hnToggle(el){var f=document.getElementById('hn-file-input'),p=document.getElementById('hn-paste-input');if(el.value==='file'){f.hidden=false;p.hidden=true;}else{f.hidden=true;p.hidden=false;}}
function hnCopyLink(btn,url){navigator.clipboard.writeText(location.origin+url).then(function(){btn.textContent='copied!';setTimeout(function(){btn.textContent='copy';},1500);});}
</script>
</body>
</html>`))

var statsTmpl = template.Must(template.New("stats").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Stats &mdash; {{.Filename}}</title>
<link rel="stylesheet" href="/style.css">
<link rel="stylesheet" href="/dashboard.css">
</head>
<body class="hn-dash">
<header class="hn-dash-header">
  <div class="hn-dash-header-inner">
    <a href="/dashboard" class="hn-dash-logo">&larr; Dashboard</a>
  </div>
</header>
<main class="hn-dash-main">
  <section class="hn-card">
    <h2>{{.Filename}}</h2>
    <div class="hn-stats-grid">
      <div class="hn-stat"><span class="hn-stat-num">{{.TotalVisits}}</span><span class="hn-stat-label">total visits</span></div>
      <div class="hn-stat"><span class="hn-stat-num">{{.UniqueVisitors}}</span><span class="hn-stat-label">unique visitors</span></div>
    </div>
    <p class="hn-muted">Share link: <a href="/p/{{.Slug}}" target="_blank" class="hn-link">/p/{{.Slug}}</a> <button type="button" onclick="hnCopyLink(this, &quot;/p/{{.Slug}}&quot;)" class="hn-copy-btn">copy</button></p>
  </section>
  <section class="hn-card">
    <h3>Recent visits</h3>
    {{if .Recent}}
    <div class="hn-table-wrap">
    <table class="hn-table">
      <thead><tr><th>When</th><th>Visitor</th><th>IP (hashed)</th><th>User agent</th></tr></thead>
      <tbody>
      {{range .Recent}}
        <tr>
          <td class="hn-muted-cell">{{.WhenHuman}}</td>
          <td>{{if .Name}}{{.Name}}{{else}}<span class="hn-muted-cell">(anonymous)</span>{{end}}</td>
          <td class="hn-muted-cell">{{.IP}}</td>
          <td class="hn-ua">{{.UA}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
    </div>
    {{else}}
    <div class="hn-empty"><p>No visits yet.</p><p class="hn-muted">Share the link to start collecting analytics.</p></div>
    {{end}}
  </section>
</main>
<script>
function hnCopyLink(btn,url){navigator.clipboard.writeText(location.origin+url).then(function(){btn.textContent='copied!';setTimeout(function(){btn.textContent='copy';},1500);});}
</script>
</body>
</html>`))

// --- types for templates ---

type dashUpload struct {
	Slug         string
	Filename     string
	SizeHuman    string
	Protected    bool
	CreatedHuman string
}

type statsVisit struct {
	Name      string
	IP        string
	UA        string
	WhenHuman string
}

type dashData struct {
	CSRF                 string
	User                 string
	IsAdmin              bool
	Settings             map[string]string
	SettingsMeta         []settingsRow
	Uploads              []dashUpload
	UploadError          string
	UploadSuccess        bool
	UploadSuccessURL     string
	UploadSuccessURLJSON template.JS
}

type statsData struct {
	Slug           string
	Filename       string
	TotalVisits    int
	UniqueVisitors int
	Recent         []statsVisit
}

// --- helpers ---

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
}

// --- handlers ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	if r.Method == "GET" {
		csrf := s.newCSRF(w)
		loginTmpl.Execute(w, map[string]any{"CSRF": csrf, "Error": false})
		return
	}
	// POST
	r.ParseForm()
	tok := strings.TrimSpace(r.FormValue("token"))
	if tok == "" || !s.validateCSRF(r, w, r.FormValue("csrf")) {
		csrf := s.newCSRF(w)
		loginTmpl.Execute(w, map[string]any{"CSRF": csrf, "Error": true})
		return
	}
	owner, err := s.store.GetToken(tok)
	if err != nil {
		csrf := s.newCSRF(w)
		loginTmpl.Execute(w, map[string]any{"CSRF": csrf, "Error": true})
		return
	}
	s.setCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    makeWebSession(s.secret, strconv.FormatInt(owner.ID, 10), sessionTTL),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
	})
	s.auditRequest(r, owner.Name, "login.success", "")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	_ = s.validateCSRF(r, w, r.FormValue("csrf"))
	s.setCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	owner, ok := s.webAuth(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	var list []models.Upload
	if owner.IsAdmin {
		list, _ = s.store.ListAllUploads()
	} else {
		list, _ = s.store.ListUploadsByOwner(owner.ID)
	}
	uploads := make([]dashUpload, 0, len(list))
	for _, u := range list {
		uploads = append(uploads, dashUpload{
			Slug: u.Slug, Filename: u.Filename,
			SizeHuman: humanSize(u.Size), Protected: u.PasswordHash != "",
			CreatedHuman: u.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	csrf := s.newCSRF(w)
	allSettings := s.dashboardSettingsMap()
	sortedMeta := dashboardSettingsRows(allSettings)
	dashData_ := dashData{
		CSRF:         csrf,
		User:         owner.Name,
		IsAdmin:      owner.IsAdmin,
		Settings:     allSettings,
		SettingsMeta: sortedMeta,
		Uploads:      uploads,
	}
	// carry over flash messages from query params
	if e := r.URL.Query().Get("err"); e != "" {
		dashData_.UploadError = e
	}
	if url := r.URL.Query().Get("ok"); url != "" {
		dashData_.UploadSuccess = true
		dashData_.UploadSuccessURL = url
		dashData_.UploadSuccessURLJSON = template.JS("\"" + url + "\"")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	dashboardTmpl.Execute(w, dashData_)
}

func (s *Server) handleDashboardUpload(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	maxUpload := s.settingInt64("max_upload", 2<<20)

	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1024)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		http.Redirect(w, r, "/dashboard?err=file+too+large+or+invalid+form", http.StatusSeeOther)
		return
	}
	if !s.validateCSRF(r, w, r.FormValue("csrf")) {
		http.Redirect(w, r, "/dashboard?err=invalid+session", http.StatusSeeOther)
		return
	}

	mode := r.FormValue("mode")
	password := strings.TrimSpace(r.FormValue("password"))
	filename := strings.TrimSpace(r.FormValue("filename"))

	var data []byte
	if mode == "paste" {
		html := strings.TrimSpace(r.FormValue("html"))
		if html == "" {
			http.Redirect(w, r, "/dashboard?err=no+html+pasted", http.StatusSeeOther)
			return
		}
		data = []byte(html)
		if filename == "" {
			filename = "pasted.html"
		}
	} else {
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Redirect(w, r, "/dashboard?err=no+file+selected", http.StatusSeeOther)
			return
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, maxUpload+1))
		if err != nil || int64(len(data)) > maxUpload {
			http.Redirect(w, r, "/dashboard?err=file+too+large", http.StatusSeeOther)
			return
		}
		if filename == "" {
			filename = header.Filename
		}
	}

	if len(data) == 0 {
		http.Redirect(w, r, "/dashboard?err=empty+content", http.StatusSeeOther)
		return
	}
	if !looksLikeHTML(data) {
		http.Redirect(w, r, "/dashboard?err=file+does+not+look+like+HTML", http.StatusSeeOther)
		return
	}

	maxTotalSize := s.settingInt64("max_total_size", 0)
	if maxTotalSize > 0 {
		currentTotal, err := s.store.SumUploadSizes()
		if err != nil {
			http.Redirect(w, r, "/dashboard?err=db+error", http.StatusSeeOther)
			return
		}
		if currentTotal+int64(len(data)) > maxTotalSize {
			http.Redirect(w, r, "/dashboard?err=total+storage+quota+exceeded", http.StatusSeeOther)
			return
		}
	}

	slug, err := generateSlug(s.store)
	if err != nil {
		http.Redirect(w, r, "/dashboard?err=internal+error", http.StatusSeeOther)
		return
	}
	if err := s.storage.Save(r.Context(), slug, data); err != nil {
		http.Redirect(w, r, "/dashboard?err=storage+failed", http.StatusSeeOther)
		return
	}
	pwHash := ""
	if password != "" {
		if !validatePasswordLength(password) {
			http.Redirect(w, r, "/dashboard?err=password+must+be+72+characters+or+fewer", http.StatusSeeOther)
			return
		}
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			http.Redirect(w, r, "/dashboard?err=hash+failed", http.StatusSeeOther)
			return
		}
		pwHash = string(h)
	}
	if err := s.store.CreateUpload(slug, owner.ID, filename, int64(len(data)), pwHash); err != nil {
		_ = s.storage.Delete(r.Context(), slug)
		http.Redirect(w, r, "/dashboard?err=db+failed", http.StatusSeeOther)
		return
	}
	s.auditRequest(r, owner.Name, "upload.create", "slug="+slug+" file="+filename+" size="+strconv.Itoa(len(data)))
	url := s.baseURL + "/p/" + slug
	http.Redirect(w, r, "/dashboard?ok="+url, http.StatusSeeOther)
}

func (s *Server) handleDashboardDelete(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	slug := r.PathValue("slug")
	r.ParseForm()
	if !s.validateCSRF(r, w, r.FormValue("csrf")) {
		http.Redirect(w, r, "/dashboard?err=invalid+session", http.StatusSeeOther)
		return
	}
	u, err := s.store.GetUpload(slug)
	if err != nil {
		http.Redirect(w, r, "/dashboard?err=not+found", http.StatusSeeOther)
		return
	}
	if u.OwnerTokenID != owner.ID && !owner.IsAdmin {
		http.Redirect(w, r, "/dashboard?err=not+owner", http.StatusSeeOther)
		return
	}
	_ = s.store.DeleteUpload(u.ID)
	_ = s.storage.Delete(r.Context(), slug)
	s.auditRequest(r, owner.Name, "upload.delete", "slug="+slug+" file="+u.Filename)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	owner, ok := s.webAuth(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if u.OwnerTokenID != owner.ID && !owner.IsAdmin {
		http.NotFound(w, r)
		return
	}
	total, unique, _ := s.store.CountVisits(u.ID)
	recent, _ := s.store.RecentVisits(u.ID, 100)
	visits := make([]statsVisit, 0, len(recent))
	for _, v := range recent {
		name := v.VisitorName
		if name == "" {
			name = ""
		}
		visits = append(visits, statsVisit{
			Name: name, IP: v.IP, UA: v.UserAgent,
			WhenHuman: v.VisitedAt.Format("2006-01-02 15:04"),
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	statsTmpl.Execute(w, statsData{
		Slug: slug, Filename: u.Filename,
		TotalVisits: total, UniqueVisitors: unique, Recent: visits,
	})
}

// --- auth helpers ---

// webAuth validates the signed session cookie and returns the owning token.
// The cookie holds a signed reference to the token's id (not the bearer token),
// so it is revocable and never carries the credential itself.
func (s *Server) webAuth(r *http.Request) (*models.Token, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, false
	}
	sub, ok := parseSignedSubject(s.secret, c.Value)
	if !ok {
		return nil, false
	}
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return nil, false
	}
	t, err := s.store.GetTokenByID(id)
	if err != nil {
		return nil, false
	}
	return t, true
}

// --- CSRF ---

func (s *Server) newCSRF(w http.ResponseWriter) string {
	b := make([]byte, 16)
	rand.Read(b)
	val := hex.EncodeToString(b)
	s.setCookie(w, &http.Cookie{
		Name: csrfCookie, Value: val, Path: "/",
		MaxAge:   0,
		SameSite: http.SameSiteStrictMode, HttpOnly: false,
	})
	return val
}

func (s *Server) validateCSRF(r *http.Request, w http.ResponseWriter, val string) bool {
	if val == "" {
		return false
	}
	c, err := r.Cookie(csrfCookie)
	if err != nil || c.Value != val {
		return false
	}
	s.newCSRF(w)
	return true
}

// --- size helper (shared with CLI formatting) ---

func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
