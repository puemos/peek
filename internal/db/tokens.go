package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/puemos/peek/internal/models"
)

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func isHashed(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func (s *Store) CreateToken(ctx context.Context, token, name string, isAdmin bool, expiresAt int64) error {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx, `INSERT INTO accounts(email,name,is_admin,disabled,created_at,updated_at) VALUES(NULL,?,?,?,?,?)`,
		strings.TrimSpace(name), boolToInt(isAdmin), 0, now, now)
	if err != nil {
		return err
	}
	accountID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tokens(account_id,token,name,is_admin,created_at,expires_at) VALUES(?,?,?,?,?,?)`,
		accountID, HashToken(token), strings.TrimSpace(name), boolToInt(isAdmin), now, expiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateTokenForAccount(ctx context.Context, token string, accountID int64, name string) error {
	if strings.TrimSpace(name) == "" {
		name = "API token"
	}
	_, err := s.ExecContext(ctx, `INSERT INTO tokens(account_id,token,name,is_admin,created_at) VALUES(?,?,?,?,?)`,
		accountID, HashToken(token), strings.TrimSpace(name), 0, time.Now().Unix())
	return err
}

func (s *Store) GetToken(ctx context.Context, token string) (*models.Token, error) {
	return s.getTokenWhere(ctx, `tk.token=?`, HashToken(token))
}

func (s *Store) GetTokenByID(ctx context.Context, id int64) (*models.Token, error) {
	return s.getTokenWhere(ctx, `tk.id=?`, id)
}

func (s *Store) getTokenWhere(ctx context.Context, where string, arg any) (*models.Token, error) {
	t := &models.Token{}
	var isAdmin, disabled, ts, exp int64
	var email sql.NullString
	err := s.QueryRowContext(ctx, `SELECT tk.id,tk.account_id,tk.token,a.name,a.email,a.is_admin,a.disabled,tk.created_at,tk.expires_at
		FROM tokens tk JOIN accounts a ON a.id=tk.account_id WHERE `+where, arg).
		Scan(&t.ID, &t.AccountID, &t.Token, &t.Name, &email, &isAdmin, &disabled, &ts, &exp)
	if err != nil {
		return nil, err
	}
	t.Email = email.String
	t.IsAdmin = isAdmin == 1
	t.Disabled = disabled == 1
	t.CreatedAt = time.Unix(ts, 0)
	t.ExpiresAt = exp
	// Check expiry: 0 means no expiry.
	if exp > 0 && time.Now().Unix() > exp {
		return nil, errors.New("token expired")
	}
	return t, nil
}

func (s *Store) DeleteToken(ctx context.Context, id int64) error {
	res, err := s.ExecContext(ctx, `DELETE FROM tokens WHERE id=?`, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (s *Store) DeleteTokenChecked(ctx context.Context, id int64) (*models.Token, error) {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	t := &models.Token{}
	var isAdmin, disabled, ts, exp int64
	var email sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT tk.id,tk.account_id,tk.token,a.name,a.email,a.is_admin,a.disabled,tk.created_at,tk.expires_at
		FROM tokens tk JOIN accounts a ON a.id=tk.account_id WHERE tk.id=?`, id).
		Scan(&t.ID, &t.AccountID, &t.Token, &t.Name, &email, &isAdmin, &disabled, &ts, &exp)
	if err != nil {
		return nil, err
	}
	t.Email = email.String
	t.IsAdmin = isAdmin == 1
	t.Disabled = disabled == 1
	t.CreatedAt = time.Unix(ts, 0)
	t.ExpiresAt = exp

	if t.IsAdmin && !t.Disabled {
		n, err := countActiveAdminTokens(ctx, tx)
		if err != nil {
			return nil, err
		}
		if n <= 1 {
			return nil, ErrLastAdmin
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM tokens WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	if err := requireRowsAffected(res); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) CountAdminTokens(ctx context.Context) (int, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM tokens tk JOIN accounts a ON a.id=tk.account_id WHERE a.is_admin=1 AND a.disabled=0`).Scan(&n)
	return n, err
}

func countActiveAdminTokens(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM tokens tk JOIN accounts a ON a.id=tk.account_id WHERE a.is_admin=1 AND a.disabled=0`).Scan(&n)
	return n, err
}

func (s *Store) ListTokens(ctx context.Context) ([]models.Token, error) {
	rows, err := s.QueryContext(ctx, `SELECT tk.id,tk.account_id,tk.token,a.name,a.email,a.is_admin,a.disabled,tk.created_at,tk.expires_at
		FROM tokens tk JOIN accounts a ON a.id=tk.account_id ORDER BY tk.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Token
	for rows.Next() {
		var t models.Token
		var isAdmin, disabled, ts, exp int64
		var email sql.NullString
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Token, &t.Name, &email, &isAdmin, &disabled, &ts, &exp); err != nil {
			return nil, err
		}
		t.Email = email.String
		t.IsAdmin = isAdmin == 1
		t.Disabled = disabled == 1
		t.CreatedAt = time.Unix(ts, 0)
		t.ExpiresAt = exp
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CountTokens(ctx context.Context) (int, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM tokens`).Scan(&n)
	return n, err
}
