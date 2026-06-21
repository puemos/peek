package server

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"strings"
	"time"

	webui "github.com/puemos/peek/internal/web"
)

const (
	cliLoginTTL      = 15 * time.Minute
	cliLoginInterval = 2
)

func (s *Server) handleCLILoginStart(w http.ResponseWriter, r *http.Request) {
	if s.setupRequired() {
		jsonError(w, http.StatusBadRequest, "server setup is not complete")
		return
	}
	deviceCode, err := randID(32)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "device code failed")
		return
	}
	var userCode string
	for range 5 {
		userCode, err = randomUserCode()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "user code failed")
			return
		}
		err = s.store.CreateCLILoginDevice(deviceCode, userCode, time.Now().Add(cliLoginTTL))
		if err == nil {
			break
		}
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "login start failed")
		return
	}
	jsonOK(w, map[string]any{
		"device_code":      deviceCode,
		"user_code":        userCode,
		"verification_url": s.baseURL + "/cli-login/" + userCode,
		"interval":         cliLoginInterval,
		"expires_in":       int(cliLoginTTL.Seconds()),
	})
}

func (s *Server) handleCLILoginPoll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceCode string `json:"device_code"`
	}
	if err := decodeJSON(w, r, &body, smallJSONBodyLimit); err != nil {
		jsonError(w, http.StatusBadRequest, "bad json")
		return
	}
	body.DeviceCode = strings.TrimSpace(body.DeviceCode)
	if body.DeviceCode == "" {
		jsonError(w, http.StatusBadRequest, "device_code required")
		return
	}
	d, err := s.store.GetCLILoginByDevice(body.DeviceCode)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid device code")
		return
	}
	if time.Now().After(d.ExpiresAt) {
		if d.Status == "pending" {
			if err := s.store.ExpireCLILogin(d.ID); err != nil {
				slog.Warn("cli login expire failed", "device_id", d.ID, "err", err)
			}
		}
		jsonOK(w, map[string]any{"status": "expired"})
		return
	}
	switch d.Status {
	case "pending":
		jsonOK(w, map[string]any{"status": "pending", "interval": cliLoginInterval})
	case "denied", "expired":
		jsonOK(w, map[string]any{"status": d.Status})
	case "approved":
		if !d.ConsumedAt.IsZero() {
			jsonOK(w, map[string]any{"status": "consumed"})
			return
		}
		if err := s.store.ConsumeCLILogin(d.ID); err != nil {
			jsonOK(w, map[string]any{"status": "consumed"})
			return
		}
		account, err := s.store.GetAccountByID(d.AccountID)
		if err != nil || account.Disabled {
			jsonOK(w, map[string]any{"status": "denied"})
			return
		}
		token, err := randID(24)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "token generation failed")
			return
		}
		if err := s.store.CreateTokenForAccount(token, account.ID, "CLI login"); err != nil {
			jsonError(w, http.StatusInternalServerError, "token creation failed")
			return
		}
		s.audit("cli token issued account=%d email=%q", account.ID, account.Email)
		jsonOK(w, map[string]any{"status": "approved", "token": token})
	default:
		jsonOK(w, map[string]any{"status": "expired"})
	}
}

func (s *Server) handleCLILoginPage(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	d, err := s.store.GetCLILoginByUserCode(code)
	if err != nil || d.Status != "pending" || time.Now().After(d.ExpiresAt) {
		s.renderHTML(w, http.StatusOK, webui.TemplateCLILoginDone, webui.CLILoginDoneData{Title: "CLI login expired", Message: "Start a new login from the CLI."})
		return
	}
	owner, ok := s.webAuth(r)
	if !ok {
		s.setNextPath(w, "/cli-login/"+code)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	csrf := s.newCSRF(w)
	s.renderHTML(w, http.StatusOK, webui.TemplateCLILogin, webui.CLILoginData{Code: code, CSRF: csrf, User: owner.Name})
}

func (s *Server) handleCLILoginApprove(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	owner, ok := s.webAuth(r)
	if !ok {
		s.setNextPath(w, "/cli-login/"+code)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil || !s.validateCSRF(r, w, r.FormValue("csrf")) {
		s.renderHTML(w, http.StatusOK, webui.TemplateCLILogin, webui.CLILoginData{Code: code, CSRF: s.newCSRF(w), User: owner.Name, Error: "Invalid session."})
		return
	}
	d, err := s.store.GetCLILoginByUserCode(code)
	if err != nil || d.Status != "pending" || time.Now().After(d.ExpiresAt) {
		s.renderHTML(w, http.StatusOK, webui.TemplateCLILoginDone, webui.CLILoginDoneData{Title: "CLI login expired", Message: "Start a new login from the CLI."})
		return
	}
	if r.FormValue("decision") == "deny" {
		if err := s.store.DenyCLILogin(d.ID); err != nil {
			slog.Warn("cli login deny failed", "device_id", d.ID, "err", err)
			s.renderHTML(w, http.StatusOK, webui.TemplateCLILogin, webui.CLILoginData{Code: code, CSRF: s.newCSRF(w), User: owner.Name, Error: "Denial failed."})
			return
		}
		s.renderHTML(w, http.StatusOK, webui.TemplateCLILoginDone, webui.CLILoginDoneData{Title: "CLI login denied", Message: "Return to your terminal to continue."})
		return
	}
	if err := s.store.ApproveCLILogin(d.ID, owner.ID); err != nil {
		slog.Warn("cli login approve failed", "device_id", d.ID, "account_id", owner.ID, "err", err)
		s.renderHTML(w, http.StatusOK, webui.TemplateCLILogin, webui.CLILoginData{Code: code, CSRF: s.newCSRF(w), User: owner.Name, Error: "Approval failed."})
		return
	}
	s.audit("cli login approved account=%d code=%s", owner.ID, code)
	s.renderHTML(w, http.StatusOK, webui.TemplateCLILoginDone, webui.CLILoginDoneData{Title: "CLI login approved", Message: "Return to your terminal to continue."})
}

func randomUserCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}
