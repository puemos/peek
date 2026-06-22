package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Server) resolveOAuthAccount(r *http.Request, profile *oauthProfile) (*models.Account, error) {
	if profile.ProviderUserID == "" || profile.Email == "" || !profile.EmailVerified {
		return nil, errors.New("OAuth account must have a verified email")
	}
	if !s.accountAllowedByEmailDomain(r.Context(), profile.Email) {
		return nil, errAccountNotEligible
	}
	if oi, err := s.store.GetOAuthIdentity(r.Context(), profile.Provider, profile.ProviderUserID); err == nil {
		account, err := s.store.GetAccountByID(r.Context(), oi.AccountID)
		if err != nil {
			return nil, errors.New("linked account not found")
		}
		if account.Disabled {
			return nil, errors.New("account disabled")
		}
		if !s.accountAllowedByEmailDomain(r.Context(), account.Email) {
			return nil, errAccountNotEligible
		}
		return account, nil
	} else if err != sql.ErrNoRows {
		return nil, errors.New("OAuth lookup failed")
	}

	if account, err := s.store.GetAccountByEmail(r.Context(), profile.Email); err == nil {
		if account.Disabled {
			return nil, errors.New("account disabled")
		}
		if !s.accountAllowedByEmailDomain(r.Context(), account.Email) {
			return nil, errAccountNotEligible
		}
		return account, nil
	} else if err != sql.ErrNoRows {
		return nil, errors.New("account lookup failed")
	}

	inviteToken := ""
	if c, err := r.Cookie(inviteCookie); err == nil {
		inviteToken = c.Value
	}
	if inviteToken == "" {
		return nil, errors.New("invite required")
	}
	inv, err := s.store.GetInviteByToken(r.Context(), inviteToken)
	if err != nil {
		return nil, errors.New("invite not found")
	}
	if !invitePending(inv) || normalizeOAuthEmail(inv.Email) != profile.Email {
		return nil, errors.New("invite does not match this account")
	}
	account, err := s.store.CreateAccount(r.Context(), profile.Email, profile.Name, false)
	if err != nil {
		return nil, errors.New("account creation failed")
	}
	if err := s.store.ConsumeInvite(r.Context(), inv.ID); err != nil {
		return nil, errors.New("invite consumption failed")
	}
	return account, nil
}

func invitePending(inv *models.Invite) bool {
	return inv != nil && inv.UsedAt.IsZero() && inv.RevokedAt.IsZero() && time.Now().Before(inv.ExpiresAt)
}

func normalizeOAuthEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func displayName(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return "user"
}
