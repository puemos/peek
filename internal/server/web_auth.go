package server

import (
	"net/http"
	"strconv"

	"github.com/puemos/peek/internal/models"
)

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
	a, err := s.store.GetAccountByID(r.Context(), id)
	if err != nil {
		return nil, false
	}
	if a.Disabled {
		return nil, false
	}
	if s.oauthLoginRequired(r.Context()) && !a.IsAdmin {
		hasOAuth, err := s.store.AccountHasOAuthIdentity(r.Context(), a.ID)
		if err != nil || !hasOAuth {
			return nil, false
		}
	}
	return a, true
}
