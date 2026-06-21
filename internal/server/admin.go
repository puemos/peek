package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/puemos/peek/internal/db"
)

const inviteTTL = 7 * 24 * time.Hour

func (s *Server) handleInviteLink(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.PathValue("token"))
	inv, err := s.store.GetInviteByToken(raw)
	if err != nil || !invitePending(inv) {
		http.NotFound(w, r)
		return
	}
	s.setCookie(w, &http.Cookie{
		Name:     inviteCookie,
		Value:    raw,
		Path:     "/",
		MaxAge:   int(inviteTTL.Seconds()),
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleDashboardCreateInvite(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok || !owner.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil || !s.validateCSRF(r, w, r.FormValue("csrf")) {
		http.Redirect(w, r, "/dashboard?err=invalid+session", http.StatusSeeOther)
		return
	}
	email := normalizeOAuthEmail(r.FormValue("email"))
	if email == "" || !strings.Contains(email, "@") {
		http.Redirect(w, r, "/dashboard?err=email+required", http.StatusSeeOther)
		return
	}
	raw, err := randID(24)
	if err != nil {
		http.Redirect(w, r, "/dashboard?err=invite+failed", http.StatusSeeOther)
		return
	}
	ciphertext, err := encryptSecret(s.secret, raw)
	if err != nil {
		http.Redirect(w, r, "/dashboard?err=invite+failed", http.StatusSeeOther)
		return
	}
	if _, err := s.store.CreateInvite(raw, ciphertext, email, owner.ID, time.Now().Add(inviteTTL)); err != nil {
		http.Redirect(w, r, "/dashboard?err=invite+failed", http.StatusSeeOther)
		return
	}
	s.audit("invite created email=%q by=%s", email, owner.Name)
	http.Redirect(w, r, "/dashboard?ok=invite+created", http.StatusSeeOther)
}

func (s *Server) handleDashboardRevokeInvite(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok || !owner.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil || !s.validateCSRF(r, w, r.FormValue("csrf")) {
		http.Redirect(w, r, "/dashboard?err=invalid+session", http.StatusSeeOther)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/dashboard?err=bad+invite", http.StatusSeeOther)
		return
	}
	if err := s.store.RevokeInvite(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Redirect(w, r, "/dashboard?err=bad+invite", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/dashboard?err=invite+revoke+failed", http.StatusSeeOther)
		return
	}
	s.audit("invite revoked id=%d by=%s", id, owner.Name)
	http.Redirect(w, r, "/dashboard?ok=invite+revoked", http.StatusSeeOther)
}

func (s *Server) handleDashboardUserAdmin(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok || !owner.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil || !s.validateCSRF(r, w, r.FormValue("csrf")) {
		http.Redirect(w, r, "/dashboard?err=invalid+session", http.StatusSeeOther)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/dashboard?err=bad+user", http.StatusSeeOther)
		return
	}
	makeAdmin := r.FormValue("admin") == "true"
	if err := s.store.SetAccountAdminChecked(id, makeAdmin); err != nil {
		if errors.Is(err, db.ErrLastAdmin) {
			http.Redirect(w, r, "/dashboard?err=cannot+remove+last+admin", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/dashboard?err=user+update+failed", http.StatusSeeOther)
		return
	}
	s.audit("account admin=%t id=%d by=%s", makeAdmin, id, owner.Name)
	http.Redirect(w, r, "/dashboard?ok=user+updated", http.StatusSeeOther)
}

func (s *Server) handleDashboardUserDisabled(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok || !owner.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil || !s.validateCSRF(r, w, r.FormValue("csrf")) {
		http.Redirect(w, r, "/dashboard?err=invalid+session", http.StatusSeeOther)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/dashboard?err=bad+user", http.StatusSeeOther)
		return
	}
	disabled := r.FormValue("disabled") == "true"
	if err := s.store.SetAccountDisabledChecked(id, disabled); err != nil {
		if errors.Is(err, db.ErrLastAdmin) {
			http.Redirect(w, r, "/dashboard?err=cannot+disable+last+admin", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/dashboard?err=user+update+failed", http.StatusSeeOther)
		return
	}
	s.audit("account disabled=%t id=%d by=%s", disabled, id, owner.Name)
	http.Redirect(w, r, "/dashboard?ok=user+updated", http.StatusSeeOther)
}

func (s *Server) dashboardInviteRows() []inviteDashRow {
	invites, err := s.store.ListInvites()
	if err != nil {
		return nil
	}
	out := make([]inviteDashRow, 0, len(invites))
	for _, inv := range invites {
		status := "pending"
		canRevoke := true
		raw, _ := decryptSecret(s.secret, inv.Token)
		link := ""
		if raw != "" {
			link = s.baseURL + "/invite/" + raw
		}
		if !inv.UsedAt.IsZero() {
			status = "used"
			canRevoke = false
			link = ""
		} else if !inv.RevokedAt.IsZero() {
			status = "revoked"
			canRevoke = false
			link = ""
		} else if time.Now().After(inv.ExpiresAt) {
			status = "expired"
			canRevoke = false
			link = ""
		}
		out = append(out, inviteDashRow{
			ID:        inv.ID,
			Email:     inv.Email,
			Status:    status,
			Expires:   inv.ExpiresAt.Format("2006-01-02 15:04"),
			Link:      link,
			CanRevoke: canRevoke,
		})
	}
	return out
}

func (s *Server) dashboardAccountRows(selfID int64) []accountDashRow {
	accounts, err := s.store.ListAccounts()
	if err != nil {
		return nil
	}
	out := make([]accountDashRow, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, accountDashRow{
			ID:       a.ID,
			Name:     a.Name,
			Email:    a.Email,
			Admin:    a.IsAdmin,
			Disabled: a.Disabled,
			IsSelf:   a.ID == selfID,
		})
	}
	return out
}
