package server

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/uploads"
	webui "github.com/puemos/peek/internal/web"
)

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
	if !s.setupRequired() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if r.Method == "GET" {
		code := strings.TrimSpace(r.URL.Query().Get("code"))
		data := setupData{}
		if s.validSetupCode(code) {
			data.Code = code
		} else {
			data.Error = "Use the setup URL printed by the server."
		}
		s.renderSetupForm(w, http.StatusOK, data)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderSetupForm(w, http.StatusOK, setupData{Error: "Invalid form."})
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	validCSRF, err := s.validateCSRF(r, w, r.FormValue("csrf"))
	if err != nil {
		s.renderCSRFError(w, err)
		return
	}
	if !validCSRF || !s.validSetupCode(code) {
		s.renderSetupForm(w, http.StatusOK, setupData{Error: "Invalid setup session."})
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	name := strings.TrimSpace(r.FormValue("name"))
	password := r.FormValue("password")
	if email == "" || !strings.Contains(email, "@") {
		s.renderSetupForm(w, http.StatusOK, setupData{Code: code, Error: "Admin email is required."})
		return
	}
	if password == "" {
		s.renderSetupForm(w, http.StatusOK, setupData{Code: code, Error: "Password is required."})
		return
	}
	if !uploads.ValidatePasswordLength(password) {
		s.renderSetupForm(w, http.StatusOK, setupData{Code: code, Error: "Password must be 72 characters or fewer."})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.renderSetupForm(w, http.StatusOK, setupData{Code: code, Error: "Could not create password."})
		return
	}
	account, err := s.store.CreateAccountWithPassword(email, name, string(hash), true)
	if err != nil {
		s.renderSetupForm(w, http.StatusOK, setupData{Code: code, Error: "Could not create admin account."})
		return
	}
	s.clearSetupCode()
	s.setWebSession(w, account.ID)
	s.auditRequest(r, account.Name, "setup.admin_created", "")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) renderSetupForm(w http.ResponseWriter, status int, data setupData) {
	csrf, ok := s.csrfToken(w)
	if !ok {
		return
	}
	data.CSRF = csrf
	s.renderHTML(w, status, webui.TemplateSetup, data)
}
