package server

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/puemos/peek/internal/models"
)

const (
	inviteCookie = "hn_invite"
	nextCookie   = "hn_next"
)

type authProvider struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type oauthProfile struct {
	Provider       string
	ProviderUserID string
	Email          string
	EmailVerified  bool
	Name           string
}

type oauthProviderConfig struct {
	authProvider
	ClientID     string
	ClientSecret string
	Endpoint     oauth2.Endpoint
	Scopes       []string
}

var (
	githubOAuthEndpoint = oauth2.Endpoint{
		AuthURL:  "https://github.com/login/oauth/authorize",
		TokenURL: "https://github.com/login/oauth/access_token",
	}
	githubUserURL   = "https://api.github.com/user"
	githubEmailsURL = "https://api.github.com/user/emails"
)

func (s *Server) handleAuthProviders(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"providers": s.enabledOAuthProviders()})
}

func (s *Server) enabledOAuthProviders() []authProvider {
	out := []authProvider{}
	for _, key := range []string{"google", "github"} {
		p, err := s.oauthProviderConfig(key)
		if err == nil {
			out = append(out, p.authProvider)
		}
	}
	return out
}

func (s *Server) oauthProviderConfig(key string) (*oauthProviderConfig, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	enabled := s.settingBool("oauth_" + key + "_enabled")
	clientID := strings.TrimSpace(s.settingString("oauth_" + key + "_client_id"))
	clientSecret := strings.TrimSpace(s.settingString("oauth_" + key + "_client_secret"))
	if !enabled || clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("%s oauth is not configured", key)
	}
	switch key {
	case "google":
		return &oauthProviderConfig{
			authProvider: authProvider{Key: "google", Name: "Google"},
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     google.Endpoint,
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		}, nil
	case "github":
		return &oauthProviderConfig{
			authProvider: authProvider{Key: "github", Name: "GitHub"},
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     githubOAuthEndpoint,
			Scopes:       []string{"read:user", "user:email"},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported oauth provider")
	}
}

func (s *Server) oauth2Config(p *oauthProviderConfig) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		RedirectURL:  s.baseURL + "/oauth/" + p.Key + "/callback",
		Scopes:       p.Scopes,
		Endpoint:     p.Endpoint,
	}
}

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

func (s *Server) fetchOAuthProfile(ctx context.Context, p *oauthProviderConfig, tok *oauth2.Token) (*oauthProfile, error) {
	switch p.Key {
	case "google":
		return s.fetchGoogleProfile(ctx, p, tok)
	case "github":
		return s.fetchGitHubProfile(ctx, p, tok)
	default:
		return nil, errors.New("unsupported provider")
	}
}

func (s *Server) fetchGoogleProfile(ctx context.Context, p *oauthProviderConfig, tok *oauth2.Token) (*oauthProfile, error) {
	rawIDToken, _ := tok.Extra("id_token").(string)
	if rawIDToken == "" {
		return nil, errors.New("missing id token")
	}
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, err
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: p.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		return nil, err
	}
	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}
	return &oauthProfile{
		Provider:       "google",
		ProviderUserID: claims.Sub,
		Email:          normalizeOAuthEmail(claims.Email),
		EmailVerified:  claims.EmailVerified,
		Name:           displayName(claims.Name, claims.Email),
	}, nil
}

func (s *Server) fetchGitHubProfile(ctx context.Context, p *oauthProviderConfig, tok *oauth2.Token) (*oauthProfile, error) {
	client := s.oauth2Config(p).Client(ctx, tok)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github user status %d", resp.StatusCode)
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, githubEmailsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github email status %d", resp.StatusCode)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return nil, err
	}
	chosen := ""
	for _, e := range emails {
		if e.Primary && e.Verified {
			chosen = e.Email
			break
		}
	}
	if chosen == "" {
		for _, e := range emails {
			if e.Verified {
				chosen = e.Email
				break
			}
		}
	}
	if chosen == "" {
		return nil, errors.New("no verified GitHub email")
	}
	return &oauthProfile{
		Provider:       "github",
		ProviderUserID: strconv.FormatInt(user.ID, 10),
		Email:          normalizeOAuthEmail(chosen),
		EmailVerified:  true,
		Name:           displayName(user.Name, user.Login, chosen),
	}, nil
}

func (s *Server) resolveOAuthAccount(r *http.Request, profile *oauthProfile) (*models.Account, error) {
	if profile.ProviderUserID == "" || profile.Email == "" || !profile.EmailVerified {
		return nil, errors.New("OAuth account must have a verified email")
	}
	if oi, err := s.store.GetOAuthIdentity(profile.Provider, profile.ProviderUserID); err == nil {
		account, err := s.store.GetAccountByID(oi.AccountID)
		if err != nil {
			return nil, errors.New("linked account not found")
		}
		if account.Disabled {
			return nil, errors.New("account disabled")
		}
		return account, nil
	} else if err != sql.ErrNoRows {
		return nil, errors.New("OAuth lookup failed")
	}

	if account, err := s.store.GetAccountByEmail(profile.Email); err == nil {
		if account.Disabled {
			return nil, errors.New("account disabled")
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
	inv, err := s.store.GetInviteByToken(inviteToken)
	if err != nil {
		return nil, errors.New("invite not found")
	}
	if !invitePending(inv) || normalizeOAuthEmail(inv.Email) != profile.Email {
		return nil, errors.New("invite does not match this account")
	}
	account, err := s.store.CreateAccount(profile.Email, profile.Name, false)
	if err != nil {
		return nil, errors.New("account creation failed")
	}
	if err := s.store.ConsumeInvite(inv.ID); err != nil {
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

func oauthCookieName(provider string) string {
	return "hn_oauth_" + provider
}

func (s *Server) makeOAuthFlowCookie(provider, state, verifier string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(provider + "|" + state + "|" + verifier))
	return makeWebSession(s.secret, payload, 10*time.Minute)
}

func (s *Server) parseOAuthFlowCookie(r *http.Request, provider string) (state, verifier string, ok bool) {
	c, err := r.Cookie(oauthCookieName(provider))
	if err != nil || c.Value == "" {
		return "", "", false
	}
	payload, ok := parseSignedSubject(s.secret, c.Value)
	if !ok {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(raw), "|", 3)
	if len(parts) != 3 || parts[0] != provider {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func (s *Server) setWebSession(w http.ResponseWriter, accountID int64) {
	s.setCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    makeWebSession(s.secret, strconv.FormatInt(accountID, 10), sessionTTL),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
	})
}

func (s *Server) setNextPath(w http.ResponseWriter, next string) {
	if !safeNextPath(next) {
		return
	}
	s.setCookie(w, &http.Cookie{
		Name:     nextCookie,
		Value:    next,
		Path:     "/",
		MaxAge:   10 * 60,
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
}

func (s *Server) consumeNextPath(w http.ResponseWriter, r *http.Request) string {
	next := "/dashboard"
	if c, err := r.Cookie(nextCookie); err == nil && safeNextPath(c.Value) {
		next = c.Value
	}
	s.clearCookie(w, nextCookie, "/")
	return next
}

func safeNextPath(next string) bool {
	return strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") &&
		!strings.ContainsAny(next, "\r\n")
}

func (s *Server) clearCookie(w http.ResponseWriter, name, path string) {
	s.setCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
}
