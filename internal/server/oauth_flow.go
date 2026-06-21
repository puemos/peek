package server

import (
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/oauth2"

	webui "github.com/puemos/peek/internal/web"
)

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	p, err := s.oauthProviderConfig(r.PathValue("provider"))
	if err != nil {
		s.renderOAuthError(w, r, http.StatusNotFound, "OAuth provider is not configured.")
		return
	}
	state, err := randID(24)
	if err != nil {
		slog.Error("oauth state generation failed", "provider", p.Key, "err", err)
		s.renderOAuthError(w, r, http.StatusInternalServerError, "OAuth login could not start.")
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
		s.renderOAuthError(w, r, http.StatusNotFound, "OAuth provider is not configured.")
		return
	}
	state, verifier, ok := s.parseOAuthFlowCookie(r, p.Key)
	if !ok || state == "" || state != r.URL.Query().Get("state") {
		s.clearCookie(w, oauthCookieName(p.Key), "/oauth/"+p.Key)
		s.renderOAuthError(w, r, http.StatusBadRequest, "OAuth session expired. Try signing in again.")
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		s.clearCookie(w, oauthCookieName(p.Key), "/oauth/"+p.Key)
		s.renderOAuthError(w, r, http.StatusUnauthorized, "OAuth sign-in was denied.")
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		s.clearCookie(w, oauthCookieName(p.Key), "/oauth/"+p.Key)
		s.renderOAuthError(w, r, http.StatusBadRequest, "OAuth callback was missing a code.")
		return
	}
	ctx := r.Context()
	tok, err := s.oauth2Config(p).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		slog.Warn("oauth token exchange failed", "provider", p.Key, "err", err)
		s.clearCookie(w, oauthCookieName(p.Key), "/oauth/"+p.Key)
		s.renderOAuthError(w, r, http.StatusBadGateway, "OAuth token exchange failed.")
		return
	}
	profile, err := s.fetchOAuthProfile(ctx, p, tok)
	if err != nil {
		slog.Warn("oauth profile lookup failed", "provider", p.Key, "err", err)
		s.clearCookie(w, oauthCookieName(p.Key), "/oauth/"+p.Key)
		s.renderOAuthError(w, r, http.StatusBadGateway, "OAuth profile lookup failed.")
		return
	}
	account, err := s.resolveOAuthAccount(r, profile)
	if err != nil {
		slog.Warn("oauth account resolution failed", "provider", p.Key, "err", err)
		s.clearCookie(w, oauthCookieName(p.Key), "/oauth/"+p.Key)
		s.renderOAuthError(w, r, http.StatusForbidden, oauthAccountErrorMessage(err))
		return
	}
	if err := s.store.UpsertOAuthIdentity(account.ID, profile.Provider, profile.ProviderUserID, profile.Email, profile.Name); err != nil {
		slog.Error("oauth account link failed", "provider", p.Key, "account_id", account.ID, "err", err)
		s.clearCookie(w, oauthCookieName(p.Key), "/oauth/"+p.Key)
		s.renderOAuthError(w, r, http.StatusInternalServerError, "OAuth account link failed.")
		return
	}
	s.clearCookie(w, oauthCookieName(p.Key), "/oauth/"+p.Key)
	s.clearCookie(w, inviteCookie, "/")
	s.setWebSession(w, account.ID)
	s.audit("oauth login provider=%s account=%d email=%q", p.Key, account.ID, account.Email)
	http.Redirect(w, r, s.consumeNextPath(w, r), http.StatusSeeOther)
}

func (s *Server) renderOAuthError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
	s.renderLoginForm(w, status, msg, r)
}

func oauthAccountErrorMessage(err error) string {
	if err == nil {
		return "OAuth account could not be linked."
	}
	switch err.Error() {
	case "OAuth account must have a verified email":
		return "OAuth account must have a verified email."
	case "account disabled":
		return "This account is disabled."
	case "invite required":
		return "An invite is required for this account."
	case "invite not found", "invite does not match this account":
		return "This invite is invalid or expired."
	default:
		return "OAuth account could not be linked."
	}
}
