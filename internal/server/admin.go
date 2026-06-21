package server

import (
	"database/sql"
	"errors"
	"log/slog"
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
		s.renderWebError(w, http.StatusNotFound, "Invite not found", "This invite link is invalid, expired, or already used.")
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
	if !s.parseDashboardForm(w, r) {
		return
	}
	email := normalizeOAuthEmail(r.FormValue("email"))
	if email == "" || !strings.Contains(email, "@") {
		dashboardError(w, r, "email required")
		return
	}
	raw, err := randID(24)
	if err != nil {
		dashboardError(w, r, "invite failed")
		return
	}
	ciphertext, err := encryptSecret(s.secret, raw)
	if err != nil {
		dashboardError(w, r, "invite failed")
		return
	}
	if _, err := s.store.CreateInvite(raw, ciphertext, email, owner.ID, time.Now().Add(inviteTTL)); err != nil {
		dashboardError(w, r, "invite failed")
		return
	}
	s.audit("invite created email=%q by=%s", email, owner.Name)
	dashboardOK(w, r, "invite created")
}

func (s *Server) handleDashboardRevokeInvite(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok || !owner.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.parseDashboardForm(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		dashboardError(w, r, "bad invite")
		return
	}
	if err := s.store.RevokeInvite(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			dashboardError(w, r, "bad invite")
			return
		}
		dashboardError(w, r, "invite revoke failed")
		return
	}
	s.audit("invite revoked id=%d by=%s", id, owner.Name)
	dashboardOK(w, r, "invite revoked")
}

func (s *Server) handleDashboardUserAdmin(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok || !owner.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.parseDashboardForm(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		dashboardError(w, r, "bad user")
		return
	}
	makeAdmin := r.FormValue("admin") == "true"
	if err := s.store.SetAccountAdminChecked(id, makeAdmin); err != nil {
		if errors.Is(err, db.ErrLastAdmin) {
			dashboardError(w, r, "cannot remove last admin")
			return
		}
		dashboardError(w, r, "user update failed")
		return
	}
	s.audit("account admin=%t id=%d by=%s", makeAdmin, id, owner.Name)
	dashboardOK(w, r, "user updated")
}

func (s *Server) handleDashboardUserDisabled(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok || !owner.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.parseDashboardForm(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		dashboardError(w, r, "bad user")
		return
	}
	disabled := r.FormValue("disabled") == "true"
	if err := s.store.SetAccountDisabledChecked(id, disabled); err != nil {
		if errors.Is(err, db.ErrLastAdmin) {
			dashboardError(w, r, "cannot disable last admin")
			return
		}
		dashboardError(w, r, "user update failed")
		return
	}
	s.audit("account disabled=%t id=%d by=%s", disabled, id, owner.Name)
	dashboardOK(w, r, "user updated")
}

func (s *Server) dashboardInviteRows() []inviteDashRow {
	invites, err := s.store.ListInvites()
	if err != nil {
		slog.Error("dashboard invite list failed", "err", err)
		return nil
	}
	out := make([]inviteDashRow, 0, len(invites))
	for _, inv := range invites {
		status := "pending"
		canRevoke := true
		raw, err := decryptSecret(s.secret, inv.Token)
		if err != nil {
			slog.Warn("dashboard invite decrypt failed", "invite_id", inv.ID, "err", err)
		}
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
		slog.Error("dashboard account list failed", "err", err)
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
