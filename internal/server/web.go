package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/models"
	webui "github.com/puemos/peek/internal/web"
)

const (
	sessionCookie = "hn_session"
	csrfCookie    = "hn_csrf"
)

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
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
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
		s.renderHTML(w, http.StatusOK, webui.TemplateSetup, data)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderHTML(w, http.StatusOK, webui.TemplateSetup, setupData{CSRF: s.newCSRF(w), Error: "Invalid form."})
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if !s.validateCSRF(r, w, r.FormValue("csrf")) || !s.validSetupCode(code) {
		s.renderHTML(w, http.StatusOK, webui.TemplateSetup, setupData{CSRF: s.newCSRF(w), Error: "Invalid setup session."})
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	name := strings.TrimSpace(r.FormValue("name"))
	password := r.FormValue("password")
	if email == "" || !strings.Contains(email, "@") {
		s.renderHTML(w, http.StatusOK, webui.TemplateSetup, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Admin email is required."})
		return
	}
	if password == "" {
		s.renderHTML(w, http.StatusOK, webui.TemplateSetup, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Password is required."})
		return
	}
	if !validatePasswordLength(password) {
		s.renderHTML(w, http.StatusOK, webui.TemplateSetup, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Password must be 72 characters or fewer."})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.renderHTML(w, http.StatusOK, webui.TemplateSetup, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Could not create password."})
		return
	}
	account, err := s.store.CreateAccountWithPassword(email, name, string(hash), true)
	if err != nil {
		s.renderHTML(w, http.StatusOK, webui.TemplateSetup, setupData{CSRF: s.newCSRF(w), Code: code, Error: "Could not create admin account."})
		return
	}
	s.clearSetupCode()
	s.setWebSession(w, account.ID)
	s.auditRequest(r, account.Name, "setup.admin_created", "")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
	if s.setupRequired() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if r.Method == "GET" {
		csrf := s.newCSRF(w)
		s.renderHTML(w, http.StatusOK, webui.TemplateLogin, s.loginData(csrf, "", r))
		return
	}
	// POST
	r.ParseForm()
	if !s.validateCSRF(r, w, r.FormValue("csrf")) {
		csrf := s.newCSRF(w)
		s.renderHTML(w, http.StatusOK, webui.TemplateLogin, s.loginData(csrf, "Invalid session.", r))
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
		s.renderHTML(w, http.StatusOK, webui.TemplateLogin, s.loginData(csrf, "Invalid credentials.", r))
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
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
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
	s.renderHTML(w, http.StatusOK, webui.TemplateDashboard, dashData_)
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
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
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
	s.renderHTML(w, http.StatusOK, webui.TemplateStats, statsData{
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
