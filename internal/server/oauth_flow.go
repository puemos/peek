package server

import (
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	p, err := s.oauthProviderConfig(r.PathValue("provider"))
	if err != nil {
		http.Error(w, "OAuth provider is not configured", http.StatusNotFound)
		return
	}
	state, err := randID(24)
	if err != nil {
		http.Error(w, "state generation failed", http.StatusInternalServerError)
		return
	}
	verifier := oauth2.GenerateVerifier()
	s.setCookie(w, &http.Cookie{
		Name:     oauthCookieName(p.Key),
		Value:    s.makeOAuthFlowCookie(p.Key, state, verifier),
		Path:     "/oauth/" + p.Key,
		MaxAge:   10 * 60,
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
	url := s.oauth2Config(p).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	p, err := s.oauthProviderConfig(r.PathValue("provider"))
	if err != nil {
		http.Error(w, "OAuth provider is not configured", http.StatusNotFound)
		return
	}
	state, verifier, ok := s.parseOAuthFlowCookie(r, p.Key)
	if !ok || state == "" || state != r.URL.Query().Get("state") {
		http.Error(w, "OAuth state mismatch", http.StatusBadRequest)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		http.Error(w, "OAuth denied: "+e, http.StatusUnauthorized)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "OAuth callback missing code", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tok, err := s.oauth2Config(p).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		http.Error(w, "OAuth token exchange failed", http.StatusBadGateway)
		return
	}
	profile, err := s.fetchOAuthProfile(ctx, p, tok)
	if err != nil {
		http.Error(w, "OAuth profile lookup failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	account, err := s.resolveOAuthAccount(r, profile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := s.store.UpsertOAuthIdentity(account.ID, profile.Provider, profile.ProviderUserID, profile.Email, profile.Name); err != nil {
		http.Error(w, "OAuth account link failed", http.StatusInternalServerError)
		return
	}
	s.clearCookie(w, oauthCookieName(p.Key), "/oauth/"+p.Key)
	s.clearCookie(w, inviteCookie, "/")
	s.setWebSession(w, account.ID)
	s.audit("oauth login provider=%s account=%d email=%q", p.Key, account.ID, account.Email)
	http.Redirect(w, r, s.consumeNextPath(w, r), http.StatusSeeOther)
}
