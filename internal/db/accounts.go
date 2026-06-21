package db

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Store) CreateAccount(ctx context.Context, email, name string, isAdmin bool) (*models.Account, error) {
	return s.CreateAccountWithPassword(ctx, email, name, "", isAdmin)
}

func (s *Store) CreateAccountWithPassword(ctx context.Context, email, name, passwordHash string, isAdmin bool) (*models.Account, error) {
	email = normalizeEmail(email)
	name = strings.TrimSpace(name)
	if name == "" {
		name = email
	}
	now := time.Now().Unix()
	var emailArg any
	if email != "" {
		emailArg = email
	}
	res, err := s.ExecContext(ctx, `INSERT INTO accounts(email,name,password_hash,is_admin,disabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		emailArg, name, strings.TrimSpace(passwordHash), boolToInt(isAdmin), 0, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetAccountByID(ctx, id)
}

func (s *Store) GetAccountByID(ctx context.Context, id int64) (*models.Account, error) {
	return s.getAccountWhere(ctx, `id=?`, id)
}

func (s *Store) GetAccountByEmail(ctx context.Context, email string) (*models.Account, error) {
	return s.getAccountWhere(ctx, `lower(email)=?`, normalizeEmail(email))
}

func (s *Store) getAccountWhere(ctx context.Context, where string, arg any) (*models.Account, error) {
	a := &models.Account{}
	var isAdmin, disabled, created, updated int64
	var email sql.NullString
	err := s.QueryRowContext(ctx, `SELECT id,email,name,password_hash,is_admin,disabled,created_at,updated_at FROM accounts WHERE `+where, arg).
		Scan(&a.ID, &email, &a.Name, &a.PasswordHash, &isAdmin, &disabled, &created, &updated)
	if err != nil {
		return nil, err
	}
	a.Email = email.String
	a.IsAdmin = isAdmin == 1
	a.Disabled = disabled == 1
	a.CreatedAt = time.Unix(created, 0)
	a.UpdatedAt = time.Unix(updated, 0)
	return a, nil
}

func (s *Store) ListAccounts(ctx context.Context) ([]models.Account, error) {
	rows, err := s.QueryContext(ctx, `SELECT id,email,name,password_hash,is_admin,disabled,created_at,updated_at FROM accounts ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Account
	for rows.Next() {
		var a models.Account
		var email sql.NullString
		var isAdmin, disabled, created, updated int64
		if err := rows.Scan(&a.ID, &email, &a.Name, &a.PasswordHash, &isAdmin, &disabled, &created, &updated); err != nil {
			return nil, err
		}
		a.Email = email.String
		a.IsAdmin = isAdmin == 1
		a.Disabled = disabled == 1
		a.CreatedAt = time.Unix(created, 0)
		a.UpdatedAt = time.Unix(updated, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SetAccountAdmin(ctx context.Context, id int64, isAdmin bool) error {
	res, err := s.ExecContext(ctx, `UPDATE accounts SET is_admin=?, updated_at=? WHERE id=?`, boolToInt(isAdmin), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (s *Store) SetAccountAdminChecked(ctx context.Context, id int64, isAdmin bool) error {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if !isAdmin {
		targetAdmin, targetDisabled, err := accountAdminState(ctx, tx, id)
		if err != nil {
			return err
		}
		if targetAdmin && !targetDisabled {
			n, err := countActiveAdmins(ctx, tx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE accounts SET is_admin=?, updated_at=? WHERE id=?`, boolToInt(isAdmin), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if err := requireRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetAccountDisabled(ctx context.Context, id int64, disabled bool) error {
	res, err := s.ExecContext(ctx, `UPDATE accounts SET disabled=?, updated_at=? WHERE id=?`, boolToInt(disabled), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (s *Store) SetAccountDisabledChecked(ctx context.Context, id int64, disabled bool) error {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if disabled {
		targetAdmin, targetDisabled, err := accountAdminState(ctx, tx, id)
		if err != nil {
			return err
		}
		if targetAdmin && !targetDisabled {
			n, err := countActiveAdmins(ctx, tx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE accounts SET disabled=?, updated_at=? WHERE id=?`, boolToInt(disabled), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if err := requireRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CountAdminAccounts(ctx context.Context) (int, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE is_admin=1 AND disabled=0`).Scan(&n)
	return n, err
}

func accountAdminState(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int64) (isAdmin, disabled bool, err error) {
	var adminInt, disabledInt int64
	err = q.QueryRowContext(ctx, `SELECT is_admin, disabled FROM accounts WHERE id=?`, id).Scan(&adminInt, &disabledInt)
	return adminInt == 1, disabledInt == 1, err
}

func countActiveAdmins(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE is_admin=1 AND disabled=0`).Scan(&n)
	return n, err
}

func (s *Store) CountAccounts(ctx context.Context) (int, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&n)
	return n, err
}
