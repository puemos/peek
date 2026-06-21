package db

import (
	"database/sql"
	"strings"
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Store) CreateAccount(email, name string, isAdmin bool) (*models.Account, error) {
	return s.CreateAccountWithPassword(email, name, "", isAdmin)
}

func (s *Store) CreateAccountWithPassword(email, name, passwordHash string, isAdmin bool) (*models.Account, error) {
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
	res, err := s.Exec(`INSERT INTO accounts(email,name,password_hash,is_admin,disabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		emailArg, name, strings.TrimSpace(passwordHash), boolToInt(isAdmin), 0, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetAccountByID(id)
}

func (s *Store) GetAccountByID(id int64) (*models.Account, error) {
	return s.getAccountWhere(`id=?`, id)
}

func (s *Store) GetAccountByEmail(email string) (*models.Account, error) {
	return s.getAccountWhere(`lower(email)=?`, normalizeEmail(email))
}

func (s *Store) getAccountWhere(where string, arg any) (*models.Account, error) {
	a := &models.Account{}
	var isAdmin, disabled, created, updated int64
	var email sql.NullString
	err := s.QueryRow(`SELECT id,email,name,password_hash,is_admin,disabled,created_at,updated_at FROM accounts WHERE `+where, arg).
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

func (s *Store) ListAccounts() ([]models.Account, error) {
	rows, err := s.Query(`SELECT id,email,name,password_hash,is_admin,disabled,created_at,updated_at FROM accounts ORDER BY created_at,id`)
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

func (s *Store) SetAccountAdmin(id int64, isAdmin bool) error {
	_, err := s.Exec(`UPDATE accounts SET is_admin=?, updated_at=? WHERE id=?`, boolToInt(isAdmin), time.Now().Unix(), id)
	return err
}

func (s *Store) SetAccountAdminChecked(id int64, isAdmin bool) error {
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if !isAdmin {
		targetAdmin, targetDisabled, err := accountAdminState(tx, id)
		if err != nil {
			return err
		}
		if targetAdmin && !targetDisabled {
			n, err := countActiveAdmins(tx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
	}
	res, err := tx.Exec(`UPDATE accounts SET is_admin=?, updated_at=? WHERE id=?`, boolToInt(isAdmin), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) SetAccountDisabled(id int64, disabled bool) error {
	_, err := s.Exec(`UPDATE accounts SET disabled=?, updated_at=? WHERE id=?`, boolToInt(disabled), time.Now().Unix(), id)
	return err
}

func (s *Store) SetAccountDisabledChecked(id int64, disabled bool) error {
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if disabled {
		targetAdmin, targetDisabled, err := accountAdminState(tx, id)
		if err != nil {
			return err
		}
		if targetAdmin && !targetDisabled {
			n, err := countActiveAdmins(tx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
	}
	res, err := tx.Exec(`UPDATE accounts SET disabled=?, updated_at=? WHERE id=?`, boolToInt(disabled), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) CountAdminAccounts() (int, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM accounts WHERE is_admin=1 AND disabled=0`).Scan(&n)
	return n, err
}

func accountAdminState(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, id int64) (isAdmin, disabled bool, err error) {
	var adminInt, disabledInt int64
	err = q.QueryRow(`SELECT is_admin, disabled FROM accounts WHERE id=?`, id).Scan(&adminInt, &disabledInt)
	return adminInt == 1, disabledInt == 1, err
}

func countActiveAdmins(q interface {
	QueryRow(query string, args ...any) *sql.Row
}) (int, error) {
	var n int
	err := q.QueryRow(`SELECT COUNT(*) FROM accounts WHERE is_admin=1 AND disabled=0`).Scan(&n)
	return n, err
}

func (s *Store) CountAccounts() (int, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n)
	return n, err
}
