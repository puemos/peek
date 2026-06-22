package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const authAllowedEmailDomainSetting = "auth_allowed_email_domain"

var errAccountNotEligible = errors.New("account not eligible")

func normalizeAllowedEmailDomain(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "@") {
		value = strings.TrimPrefix(value, "@")
	}
	if value == "" || strings.Contains(value, "@") {
		return "", fmt.Errorf("%s must be a single email domain", authAllowedEmailDomainSetting)
	}
	if strings.ContainsAny(value, " \t\r\n,;:/\\*") {
		return "", fmt.Errorf("%s must be a single email domain", authAllowedEmailDomainSetting)
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("%s must be a valid email domain", authAllowedEmailDomainSetting)
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return "", fmt.Errorf("%s must be a valid email domain", authAllowedEmailDomainSetting)
		}
	}
	return value, nil
}

func emailMatchesAllowedDomain(email, domain string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	return email[at+1:] == domain
}

func (s *Server) allowedEmailDomain(ctx context.Context) string {
	return s.settingString(ctx, authAllowedEmailDomainSetting)
}

func (s *Server) accountAllowedByEmailDomain(ctx context.Context, email string) bool {
	return emailMatchesAllowedDomain(email, s.allowedEmailDomain(ctx))
}

func (s *Server) validateAllowedEmailDomainUpdate(ctx context.Context, updates map[string]string) error {
	domain, ok := updates[authAllowedEmailDomainSetting]
	if !ok || domain == "" {
		return nil
	}
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return fmt.Errorf("account lookup failed")
	}
	for _, account := range accounts {
		if account.IsAdmin && !account.Disabled && emailMatchesAllowedDomain(account.Email, domain) {
			return nil
		}
	}
	return fmt.Errorf("%s must match at least one active admin email", authAllowedEmailDomainSetting)
}
