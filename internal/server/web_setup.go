package server

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

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
