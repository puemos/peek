package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	webui "github.com/puemos/peek/internal/web"
)

type authProvider = webui.AuthProvider

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
	providers := s.enabledOAuthProviders()
	jsonOK(w, map[string]any{
		"providers":      providers,
		"browser_login":  !s.setupRequired(),
		"oauth_required": len(providers) > 0,
	})
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
