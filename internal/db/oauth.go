package db

import (
	"context"
	"strings"
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Store) GetOAuthIdentity(ctx context.Context, provider, providerUserID string) (*models.OAuthIdentity, error) {
	oi := &models.OAuthIdentity{}
	var created, updated int64
	err := s.QueryRowContext(ctx, `SELECT id,account_id,provider,provider_user_id,email,name,created_at,updated_at
		FROM oauth_identities WHERE provider=? AND provider_user_id=?`, provider, providerUserID).
		Scan(&oi.ID, &oi.AccountID, &oi.Provider, &oi.ProviderUserID, &oi.Email, &oi.Name, &created, &updated)
	if err != nil {
		return nil, err
	}
	oi.CreatedAt = time.Unix(created, 0)
	oi.UpdatedAt = time.Unix(updated, 0)
	return oi, nil
}

func (s *Store) UpsertOAuthIdentity(ctx context.Context, accountID int64, provider, providerUserID, email, name string) error {
	now := time.Now().Unix()
	_, err := s.ExecContext(ctx, `INSERT INTO oauth_identities(account_id,provider,provider_user_id,email,name,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(provider,provider_user_id) DO UPDATE SET
			account_id=excluded.account_id,
			email=excluded.email,
			name=excluded.name,
			updated_at=excluded.updated_at`,
		accountID, provider, providerUserID, normalizeEmail(email), strings.TrimSpace(name), now, now)
	return err
}

func (s *Store) AccountHasOAuthIdentity(ctx context.Context, accountID int64) (bool, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_identities WHERE account_id=?`, accountID).Scan(&n)
	return n > 0, err
}
