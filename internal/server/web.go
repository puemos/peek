package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/models"
)

const (
	sessionCookie = "hn_session"
	csrfCookie    = "hn_csrf"
)

// dashboardCSP is the Content-Security-Policy for the management UI.
// It restricts all resources to same-origin, with no inline scripts/styles
// beyond what the templates use (none; all JS/CSS is served as static assets).
const dashboardCSP = "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; frame-ancestors 'none'"

// --- templates ---

var setupTmpl = template.Must(template.New("setup").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Set up Peek</title>
<link rel="stylesheet" href="/style.css">
<link rel="stylesheet" href="/dashboard.css">
</head>
<body>
<div class="hn-welcome">
  <div class="hn-gate-card">
  <form method="POST" action="/setup" class="hn-gate-form">
    <h2>Set up Peek</h2>
    <p>Create the first admin account.</p>
    <input type="email" name="email" placeholder="Admin email" required autofocus autocomplete="email">
    <input type="text" name="name" placeholder="Name" autocomplete="name">
    <input type="password" name="password" placeholder="Password" required autocomplete="new-password">
    <input type="hidden" name="code" value="{{.Code}}">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <button type="submit">Create admin</button>
    {{if .Error}}<p class="hn-err">{{.Error}}</p>{{end}}
  </form>
  </div>
</div>
</body>
</html>`))

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
  {{if .Invite}}<p class="hn-login-note">Choose a provider to accept your invite.</p>{{end}}
  {{if .Providers}}
  <div class="hn-oauth-list">
    {{range .Providers}}<a href="/oauth/{{.Key}}/start" class="hn-oauth-btn">Continue with {{.Name}}</a>{{end}}
  </div>
  {{if or .PasswordLogin .TokenLogin}}<div class="hn-login-sep"><span>or</span></div>{{end}}
  {{end}}
  {{if .PasswordLogin}}
  <form method="POST" action="/login" class="hn-gate-form">
    <h2>Peek</h2>
    <p>{{if .OAuthEnabled}}Admin sign in{{else}}Sign in with email and password{{end}}</p>
    <input type="hidden" name="method" value="password">
    <input type="email" name="email" placeholder="Email" required autofocus autocomplete="email">
    <input type="password" name="password" placeholder="Password" required autocomplete="current-password">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <button type="submit">Sign in &rarr;</button>
  </form>
  {{end}}
  {{if .TokenLogin}}
  {{if .PasswordLogin}}<div class="hn-login-sep"><span>token</span></div>{{end}}
  <form method="POST" action="/login" class="hn-gate-form">
    <h2>Peek</h2>
    <p>Enter your access token to manage uploads.</p>
    <input type="hidden" name="method" value="token">
    <input type="password" name="token" placeholder="Access token" required {{if not .PasswordLogin}}autofocus{{end}} autocomplete="off">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <button type="submit">Sign in &rarr;</button>
  </form>
  {{end}}
  {{if .Error}}<p class="hn-err">{{.Error}}</p>{{end}}
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
    <button type="button" data-url="{{.UploadSuccessURL}}" class="hn-flash-copy hn-copy-absolute">Copy</button>
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
        <label><input type="radio" name="mode" value="file" checked> Choose file</label>
        <label><input type="radio" name="mode" value="paste"> Paste HTML</label>
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
          <td><a href="/p/{{.Slug}}" target="_blank" class="hn-link">/p/{{.Slug}}</a> <button type="button" data-url="/p/{{.Slug}}" class="hn-copy-btn hn-copy-relative" title="Copy link">copy</button></td>
          <td class="hn-actions">
            <a href="/dashboard/stats/{{.Slug}}" class="hn-btn-sm">stats</a>
            <form method="POST" action="/dashboard/delete/{{.Slug}}" data-confirm="Delete {{.Filename}}? This cannot be undone.">
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
    <h2>Invitations <span class="hn-card-count">{{len .Invites}}</span></h2>
    <form method="POST" action="/dashboard/invites" class="hn-inline-form">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <input type="email" name="email" placeholder="person@example.com" required>
      <button type="submit">Create invite</button>
    </form>
    {{if .Invites}}
    <div class="hn-table-wrap">
    <table class="hn-table">
      <thead><tr><th>Email</th><th>Status</th><th>Expires</th><th>Link</th><th></th></tr></thead>
      <tbody>
      {{range .Invites}}
        <tr>
          <td>{{.Email}}</td>
          <td><span class="hn-tag {{if eq .Status "pending"}}hn-tag-on{{end}}">{{.Status}}</span></td>
          <td class="hn-muted-cell">{{.Expires}}</td>
          <td>{{if .Link}}<code>{{.Link}}</code> <button type="button" data-url="{{.Link}}" class="hn-copy-btn hn-copy-absolute">copy</button>{{else}}<span class="hn-muted-cell">{{.Status}}</span>{{end}}</td>
          <td class="hn-actions">
            {{if .CanRevoke}}
            <form method="POST" action="/dashboard/invites/revoke/{{.ID}}">
              <input type="hidden" name="csrf" value="{{$.CSRF}}">
              <button type="submit" class="hn-btn-sm hn-btn-danger">revoke</button>
            </form>
            {{end}}
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
    </div>
    {{end}}
  </section>

  <section class="hn-card">
    <h2>Users <span class="hn-card-count">{{len .Accounts}}</span></h2>
    <div class="hn-table-wrap">
    <table class="hn-table">
      <thead><tr><th>Name</th><th>Email</th><th>Admin</th><th>Status</th><th></th></tr></thead>
      <tbody>
      {{range .Accounts}}
        <tr>
          <td>{{.Name}}{{if .IsSelf}} <span class="hn-muted-cell">(you)</span>{{end}}</td>
          <td class="hn-muted-cell">{{if .Email}}{{.Email}}{{else}}token-only{{end}}</td>
          <td>{{if .Admin}}<span class="hn-tag hn-tag-on">admin</span>{{else}}<span class="hn-tag">user</span>{{end}}</td>
          <td>{{if .Disabled}}<span class="hn-tag hn-tag-danger">disabled</span>{{else}}<span class="hn-tag hn-tag-on">active</span>{{end}}</td>
          <td class="hn-actions">
            <form method="POST" action="/dashboard/users/{{.ID}}/admin">
              <input type="hidden" name="csrf" value="{{$.CSRF}}">
              <input type="hidden" name="admin" value="{{if .Admin}}false{{else}}true{{end}}">
              <button type="submit" class="hn-btn-sm">{{if .Admin}}remove admin{{else}}make admin{{end}}</button>
            </form>
            <form method="POST" action="/dashboard/users/{{.ID}}/disabled">
              <input type="hidden" name="csrf" value="{{$.CSRF}}">
              <input type="hidden" name="disabled" value="{{if .Disabled}}false{{else}}true{{end}}">
              <button type="submit" class="hn-btn-sm hn-btn-danger">{{if .Disabled}}enable{{else}}disable{{end}}</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
    </div>
  </section>

  <section class="hn-card">
    <h2>Settings</h2>
    <form method="POST" action="/dashboard/settings" class="hn-settings-form">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="hn-settings-grid">
        {{range .SettingsMeta}}
        <label>
          <span>{{.Label}}{{if .IsStartup}} <em>(restart to apply)</em>{{end}}</span>
          {{if .IsBool}}
          <input type="checkbox" name="{{.Key}}" value="true" {{if .Value}}checked{{end}}>
          {{else}}
          <input type="{{if .IsSecret}}password{{else}}text{{end}}" name="{{.Key}}" value="{{.Value}}" placeholder="{{.Description}}" autocomplete="off">
          {{end}}
          <span class="hn-muted">{{.Description}}</span>
        </label>
        {{end}}
      </div>
      <button type="submit">Save settings</button>
    </form>
  </section>
  {{end}}
</main>
<script src="/dashboard.js"></script>
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
    <p class="hn-muted">Share link: <a href="/p/{{.Slug}}" target="_blank" class="hn-link">/p/{{.Slug}}</a> <button type="button" data-url="/p/{{.Slug}}" class="hn-copy-btn hn-copy-relative">copy</button></p>
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
<script src="/dashboard.js"></script>
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

type setupData struct {
	CSRF  string
	Code  string
	Error string
}

type inviteDashRow struct {
	ID        int64
	Email     string
	Status    string
	Expires   string
	Link      string
	CanRevoke bool
}

type accountDashRow struct {
	ID       int64
	Name     string
	Email    string
	Admin    bool
	Disabled bool
	IsSelf   bool
}

type dashData struct {
	CSRF             string
	User             string
	IsAdmin          bool
	Settings         map[string]string
	SettingsMeta     []settingsRow
	Invites          []inviteDashRow
	Accounts         []accountDashRow
	Uploads          []dashUpload
	UploadError      string
	UploadSuccess    bool
	UploadSuccessURL string
}

type statsData struct {
	Slug           string
	Filename       string
	TotalVisits    int
	UniqueVisitors int
	Recent         []statsVisit
}

type loginData struct {
	CSRF          string
	Error         string
	Invite        bool
	Providers     []authProvider
	PasswordLogin bool
	TokenLogin    bool
	OAuthEnabled  bool
}

// --- helpers ---

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
}

func dashboardError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/dashboard?err="+url.QueryEscape(msg), http.StatusSeeOther)
}

// --- handlers ---

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", dashboardCSP)
	if !s.setupRequired() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if r.Method == "GET" {
		code := strings.TrimSpace(r.URL.Query().Get("code"))
		csrf := s.newCSRF(w)
		data := setupData{CSRF: csrf}
		if s.validSetupCode(code) {
			data.Code = code
		} else {
			data.Error = "Use the setup URL printed by the server."
		}
		setupTmpl.Execute(w, data)
		return
	}

	if err := r.ParseForm(); err != nil {
		setupTmpl.Execute(w, setupData{CSRF: s.newCSRF(w), Error: "Invalid form."})
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if !s.validateCSRF(r, w, r.FormValue("csrf")) || !s.validSetupCode(code) {
		setupTmpl.Execute(w, setupData{CSRF: s.newCSRF(w), Error: "Invalid setup session."})
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	name := strings.TrimSpace(r.FormValue("name"))
	password := r.FormValue("password")
	if email == "" || !strings.Contains(email, "@") {
		setupTmpl.Execute(w, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Admin email is required."})
		return
	}
	if password == "" {
		setupTmpl.Execute(w, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Password is required."})
		return
	}
	if !validatePasswordLength(password) {
		setupTmpl.Execute(w, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Password must be 72 characters or fewer."})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		setupTmpl.Execute(w, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Could not create password."})
		return
	}
	account, err := s.store.CreateAccountWithPassword(email, name, string(hash), true)
	if err != nil {
		setupTmpl.Execute(w, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Could not create admin account."})
		return
	}
	s.clearSetupCode()
	s.setWebSession(w, account.ID)
	s.auditRequest(r, account.Name, "setup.admin_created", "")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", dashboardCSP)
	if s.setupRequired() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if r.Method == "GET" {
		csrf := s.newCSRF(w)
		loginTmpl.Execute(w, s.loginData(csrf, "", r))
		return
	}
	// POST
	r.ParseForm()
	if !s.validateCSRF(r, w, r.FormValue("csrf")) {
		csrf := s.newCSRF(w)
		loginTmpl.Execute(w, s.loginData(csrf, "Invalid session.", r))
		return
	}

	var (
		accountID int64
		actorName string
		err       error
	)
	switch r.FormValue("method") {
	case "password":
		accountID, actorName, err = s.loginWithPassword(r)
	case "token":
		accountID, actorName, err = s.loginWithToken(r)
	default:
		err = fmt.Errorf("unsupported login method")
	}
	if err != nil {
		csrf := s.newCSRF(w)
		loginTmpl.Execute(w, s.loginData(csrf, "Invalid credentials.", r))
		return
	}
	s.setWebSession(w, accountID)
	s.auditRequest(r, actorName, "login.success", "")
	http.Redirect(w, r, s.consumeNextPath(w, r), http.StatusSeeOther)
}

func (s *Server) loginWithPassword(r *http.Request) (int64, string, error) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	if email == "" || password == "" {
		return 0, "", fmt.Errorf("missing credentials")
	}
	owner, err := s.store.GetAccountByEmail(email)
	if err != nil || owner.PasswordHash == "" {
		return 0, "", fmt.Errorf("invalid credentials")
	}
	if owner.Disabled {
		return 0, "", fmt.Errorf("account disabled")
	}
	if s.oauthLoginRequired() && !owner.IsAdmin {
		return 0, "", fmt.Errorf("oauth required")
	}
	if bcrypt.CompareHashAndPassword([]byte(owner.PasswordHash), []byte(password)) != nil {
		return 0, "", fmt.Errorf("invalid credentials")
	}
	return owner.ID, owner.Name, nil
}

func (s *Server) loginWithToken(r *http.Request) (int64, string, error) {
	tok := strings.TrimSpace(r.FormValue("token"))
	if tok == "" {
		return 0, "", fmt.Errorf("missing token")
	}
	owner, err := s.store.GetToken(tok)
	if err != nil {
		return 0, "", err
	}
	if owner.Disabled {
		return 0, "", fmt.Errorf("account disabled")
	}
	if (!s.tokenLoginEnabled() || s.oauthLoginRequired()) && !owner.IsAdmin {
		return 0, "", fmt.Errorf("token login disabled")
	}
	return owner.AccountID, owner.Name, nil
}

func (s *Server) oauthLoginRequired() bool {
	return len(s.enabledOAuthProviders()) > 0
}

func (s *Server) tokenLoginEnabled() bool {
	return s.settingBool("auth_token_login_enabled")
}

func (s *Server) loginData(csrf, errMsg string, r *http.Request) loginData {
	_, inviteErr := r.Cookie(inviteCookie)
	providers := s.enabledOAuthProviders()
	oauthEnabled := len(providers) > 0
	return loginData{
		CSRF:          csrf,
		Error:         errMsg,
		Invite:        inviteErr == nil,
		Providers:     providers,
		PasswordLogin: true,
		TokenLogin:    s.tokenLoginEnabled() && !oauthEnabled,
		OAuthEnabled:  oauthEnabled,
	}
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
	w.Header().Set("Content-Security-Policy", dashboardCSP)
	if s.setupRequired() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
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
	if owner.IsAdmin {
		dashData_.Invites = s.dashboardInviteRows()
		dashData_.Accounts = s.dashboardAccountRows(owner.ID)
	}
	// carry over flash messages from query params
	if e := r.URL.Query().Get("err"); e != "" {
		dashData_.UploadError = e
	}
	if url := r.URL.Query().Get("ok"); url != "" {
		dashData_.UploadSuccess = true
		dashData_.UploadSuccessURL = url
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

	up, err := s.uploadService().Create(r.Context(), uploadCreateInput{
		OwnerAccountID: owner.ID,
		Filename:       filename,
		Password:       password,
		Data:           data,
		Limits:         s.uploadLimits(),
	})
	if err != nil {
		if ue, ok := err.(*uploadError); ok {
			dashboardError(w, r, ue.Message)
		} else {
			dashboardError(w, r, "upload failed")
		}
		return
	}
	s.auditRequest(r, owner.Name, "upload.create", "slug="+up.Slug+" file="+up.Filename+" size="+strconv.Itoa(up.Size))
	shareURL := up.URL
	http.Redirect(w, r, "/dashboard?ok="+url.QueryEscape(shareURL), http.StatusSeeOther)
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
	if u.OwnerAccountID != owner.ID && !owner.IsAdmin {
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
	w.Header().Set("Content-Security-Policy", dashboardCSP)
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
	if u.OwnerAccountID != owner.ID && !owner.IsAdmin {
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

// webAuth validates the signed session cookie and returns the account.
// The cookie holds a signed reference to the account id, so it is revocable via
// account disablement and never carries an API credential.
func (s *Server) webAuth(r *http.Request) (*models.Account, bool) {
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
	a, err := s.store.GetAccountByID(id)
	if err != nil {
		return nil, false
	}
	if a.Disabled {
		return nil, false
	}
	if s.oauthLoginRequired() && !a.IsAdmin {
		hasOAuth, err := s.store.AccountHasOAuthIdentity(a.ID)
		if err != nil || !hasOAuth {
			return nil, false
		}
	}
	return a, true
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
