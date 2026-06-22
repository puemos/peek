package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const oauthJSONBodyLimit = 1 << 20

type oauthProfile struct {
	Provider       string
	ProviderUserID string
	Email          string
	EmailVerified  bool
	Name           string
}

func (s *Server) fetchOAuthProfile(ctx context.Context, p *oauthProviderConfig, tok *oauth2.Token) (*oauthProfile, error) {
	switch p.Key {
	case "google":
		return s.fetchGoogleProfile(ctx, p, tok)
	case "github":
		return s.fetchGitHubProfile(ctx, p, tok)
	case "oidc":
		return s.fetchOIDCProfile(ctx, p, tok)
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
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := fetchOAuthJSON(ctx, client, githubUserURL, "github user", &user); err != nil {
		return nil, err
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := fetchOAuthJSON(ctx, client, githubEmailsURL, "github email", &emails); err != nil {
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

func (s *Server) fetchOIDCProfile(ctx context.Context, p *oauthProviderConfig, tok *oauth2.Token) (*oauthProfile, error) {
	provider := p.OIDCProvider
	if provider == nil {
		var err error
		provider, err = s.oidcProvider(ctx, p.IssuerURL)
		if err != nil {
			return nil, err
		}
	}
	rawIDToken, _ := tok.Extra("id_token").(string)
	if rawIDToken == "" {
		return nil, errors.New("missing id token")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: p.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		return nil, err
	}
	var claims oidcProfileClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}

	sub := strings.TrimSpace(claims.Sub)
	email := normalizeOAuthEmail(claims.Email)
	emailVerified := bool(claims.EmailVerified)
	name := displayName(claims.Name, claims.PreferredUsername, email)

	if email == "" || name == "user" || !emailVerified {
		userInfo, err := provider.UserInfo(ctx, s.oauth2Config(p).TokenSource(ctx, tok))
		if err == nil {
			var infoClaims oidcProfileClaims
			if claimErr := userInfo.Claims(&infoClaims); claimErr == nil {
				if sub == "" {
					sub = strings.TrimSpace(infoClaims.Sub)
				}
				if email == "" {
					email = normalizeOAuthEmail(infoClaims.Email)
				}
				if name == "user" {
					name = displayName(infoClaims.Name, infoClaims.PreferredUsername, email)
				}
				if !emailVerified {
					emailVerified = bool(infoClaims.EmailVerified)
				}
			}
		}
	}

	if sub == "" {
		return nil, errors.New("missing oidc subject")
	}
	return &oauthProfile{
		Provider:       "oidc",
		ProviderUserID: strings.TrimRight(p.IssuerURL, "/") + "#" + sub,
		Email:          email,
		EmailVerified:  emailVerified,
		Name:           name,
	}, nil
}

type oidcProfileClaims struct {
	Sub               string       `json:"sub"`
	Email             string       `json:"email"`
	EmailVerified     flexibleBool `json:"email_verified"`
	Name              string       `json:"name"`
	PreferredUsername string       `json:"preferred_username"`
}

type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	switch strings.Trim(strings.TrimSpace(string(data)), `"`) {
	case "true", "1":
		*b = true
	case "false", "0", "", "null":
		*b = false
	default:
		return fmt.Errorf("invalid boolean value %s", data)
	}
	return nil
}

func fetchOAuthJSON(ctx context.Context, client *http.Client, url, label string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, oauthJSONBodyLimit+1))
	if err != nil {
		return fmt.Errorf("read %s response: %w", label, err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s status %d", label, resp.StatusCode)
	}
	if len(body) > oauthJSONBodyLimit {
		return fmt.Errorf("%s response too large", label)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s response: %w", label, err)
	}
	return nil
}
