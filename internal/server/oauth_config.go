package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	IssuerURL    string
	Endpoint     oauth2.Endpoint
	Scopes       []string
	OIDCProvider *oidc.Provider
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
	providers := s.enabledOAuthProviders(r.Context())
	jsonOK(w, map[string]any{
		"providers":      providers,
		"browser_login":  !s.setupRequired(r.Context()),
		"oauth_required": len(providers) > 0,
	})
}

func (s *Server) enabledOAuthProviders(ctx context.Context) []authProvider {
	out := []authProvider{}
	for _, key := range []string{"google", "github", "oidc"} {
		p, err := s.oauthProviderMetadata(ctx, key)
		if err == nil {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) oauthProviderMetadata(ctx context.Context, key string) (authProvider, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	enabled := s.settingBool(ctx, "oauth_"+key+"_enabled")
	clientID := strings.TrimSpace(s.settingString(ctx, "oauth_"+key+"_client_id"))
	clientSecret := strings.TrimSpace(s.settingString(ctx, "oauth_"+key+"_client_secret"))
	if !enabled || clientID == "" || clientSecret == "" {
		return authProvider{}, fmt.Errorf("%s oauth is not configured", key)
	}
	switch key {
	case "google":
		return authProvider{Key: "google", Name: "Google"}, nil
	case "github":
		return authProvider{Key: "github", Name: "GitHub"}, nil
	case "oidc":
		if _, err := s.normalizeOIDCIssuerURL(s.settingString(ctx, "oauth_oidc_issuer_url")); err != nil {
			return authProvider{}, err
		}
		return authProvider{Key: "oidc", Name: "SSO"}, nil
	default:
		return authProvider{}, fmt.Errorf("unsupported oauth provider")
	}
}

func (s *Server) oauthProviderConfig(ctx context.Context, key string) (*oauthProviderConfig, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	enabled := s.settingBool(ctx, "oauth_"+key+"_enabled")
	clientID := strings.TrimSpace(s.settingString(ctx, "oauth_"+key+"_client_id"))
	clientSecret := strings.TrimSpace(s.settingString(ctx, "oauth_"+key+"_client_secret"))
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
	case "oidc":
		issuerURL := strings.TrimSpace(s.settingString(ctx, "oauth_oidc_issuer_url"))
		if issuerURL == "" {
			return nil, fmt.Errorf("oidc oauth is not configured")
		}
		normalizedIssuer, err := s.normalizeOIDCIssuerURL(issuerURL)
		if err != nil {
			return nil, err
		}
		provider, err := s.oidcProvider(ctx, normalizedIssuer)
		if err != nil {
			return nil, err
		}
		return &oauthProviderConfig{
			authProvider: authProvider{Key: "oidc", Name: "SSO"},
			ClientID:     clientID,
			ClientSecret: clientSecret,
			IssuerURL:    normalizedIssuer,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
			OIDCProvider: provider,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported oauth provider")
	}
}

func (s *Server) oidcProvider(ctx context.Context, issuerURL string) (*oidc.Provider, error) {
	return oidc.NewProvider(ctx, issuerURL)
}

func (s *Server) normalizeOIDCIssuerURL(value string) (string, error) {
	return normalizeOIDCIssuerURL(value, s.oidcAllowPrivateIssuer)
}

func normalizeOIDCIssuerURL(value string, allowPrivateIssuer bool) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	u, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid OIDC issuer URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("oauth_oidc_issuer_url must use http or https")
	}
	if u.Scheme == "http" && !allowPrivateIssuer {
		return "", fmt.Errorf("oauth_oidc_issuer_url must use https unless private issuers are explicitly allowed")
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("oauth_oidc_issuer_url must include a host")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("oauth_oidc_issuer_url must not include user info, query, or fragment")
	}
	if allowPrivateIssuer {
		return value, nil
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrMetadataIP(ip) {
			return "", fmt.Errorf("oauth_oidc_issuer_url host %s is private or link-local", host)
		}
	} else {
		ips, err := net.LookupIP(host)
		if err == nil {
			for _, ip := range ips {
				if isPrivateOrMetadataIP(ip) {
					return "", fmt.Errorf("oauth_oidc_issuer_url host %s resolves to private IP %s", host, ip)
				}
			}
		}
	}
	return value, nil
}

func isPrivateOrMetadataIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, addr := range []string{"169.254.169.254", "fd00:ec2::254"} {
		if mdIP := net.ParseIP(addr); mdIP != nil && ip.Equal(mdIP) {
			return true
		}
	}
	return false
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
