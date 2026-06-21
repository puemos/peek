package server

import (
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	webui "github.com/puemos/peek/internal/web"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
	if s.setupRequired() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if r.Method == "GET" {
		s.renderLoginForm(w, http.StatusOK, "", r)
		return
	}
	// POST
	if err := r.ParseForm(); err != nil {
		s.renderLoginForm(w, http.StatusOK, "Invalid session.", r)
		return
	}
	validCSRF, csrfErr := s.validateCSRF(r, w, r.FormValue("csrf"))
	if csrfErr != nil {
		s.renderCSRFError(w, csrfErr)
		return
	}
	if !validCSRF {
		s.renderLoginForm(w, http.StatusOK, "Invalid session.", r)
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
		s.renderLoginForm(w, http.StatusOK, "Invalid credentials.", r)
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

func (s *Server) renderLoginForm(w http.ResponseWriter, status int, errMsg string, r *http.Request) {
	csrf, ok := s.csrfToken(w)
	if !ok {
		return
	}
	s.renderHTML(w, status, webui.TemplateLogin, s.loginData(csrf, errMsg, r))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderWebError(w, http.StatusBadRequest, "Invalid session", "Your logout request could not be verified.")
		return
	}
	validCSRF, err := s.validateCSRF(r, w, r.FormValue("csrf"))
	if err != nil {
		s.renderCSRFError(w, err)
		return
	}
	if !validCSRF {
		s.renderWebError(w, http.StatusBadRequest, "Invalid session", "Your logout request could not be verified.")
		return
	}
	s.setCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
