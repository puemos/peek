package db

import (
	"database/sql"
	"strings"
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Store) CreateCLILoginDevice(deviceCode, userCode string, expiresAt time.Time) error {
	_, err := s.Exec(`INSERT INTO cli_login_devices(device_hash,user_code,status,created_at,expires_at) VALUES(?,?,?,?,?)`,
		HashToken(deviceCode), userCode, "pending", time.Now().Unix(), expiresAt.Unix())
	return err
}

func (s *Store) GetCLILoginByDevice(deviceCode string) (*models.CLILoginDevice, error) {
	return s.getCLILoginWhere(`device_hash=?`, HashToken(deviceCode))
}

func (s *Store) GetCLILoginByUserCode(userCode string) (*models.CLILoginDevice, error) {
	return s.getCLILoginWhere(`user_code=?`, strings.ToUpper(strings.TrimSpace(userCode)))
}

func (s *Store) getCLILoginWhere(where string, arg any) (*models.CLILoginDevice, error) {
	d := &models.CLILoginDevice{}
	var accountID sql.NullInt64
	var created, expires, consumed int64
	err := s.QueryRow(`SELECT id,user_code,status,account_id,created_at,expires_at,consumed_at FROM cli_login_devices WHERE `+where, arg).
		Scan(&d.ID, &d.UserCode, &d.Status, &accountID, &created, &expires, &consumed)
	if err != nil {
		return nil, err
	}
	d.AccountID = accountID.Int64
	d.CreatedAt = time.Unix(created, 0)
	d.ExpiresAt = time.Unix(expires, 0)
	if consumed > 0 {
		d.ConsumedAt = time.Unix(consumed, 0)
	}
	return d, nil
}

func (s *Store) ApproveCLILogin(id, accountID int64) error {
	res, err := s.Exec(`UPDATE cli_login_devices SET status='approved', account_id=? WHERE id=? AND status='pending'`, accountID, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (s *Store) DenyCLILogin(id int64) error {
	res, err := s.Exec(`UPDATE cli_login_devices SET status='denied' WHERE id=? AND status='pending'`, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (s *Store) ConsumeCLILogin(id int64) error {
	res, err := s.Exec(`UPDATE cli_login_devices SET consumed_at=? WHERE id=? AND consumed_at=0`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (s *Store) ExpireCLILogin(id int64) error {
	res, err := s.Exec(`UPDATE cli_login_devices SET status='expired' WHERE id=? AND status='pending'`, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}
