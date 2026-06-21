package db

import (
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Store) CreateInvite(rawToken, tokenCipher, email string, createdByAccountID int64, expiresAt time.Time) (*models.Invite, error) {
	email = normalizeEmail(email)
	now := time.Now().Unix()
	res, err := s.Exec(`INSERT INTO invites(token_hash,token_cipher,email,created_by_account_id,created_at,expires_at) VALUES(?,?,?,?,?,?)`,
		HashToken(rawToken), tokenCipher, email, createdByAccountID, now, expiresAt.Unix())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetInviteByID(id)
}

func (s *Store) GetInviteByID(id int64) (*models.Invite, error) {
	return s.getInviteWhere(`id=?`, id)
}

func (s *Store) GetInviteByToken(rawToken string) (*models.Invite, error) {
	return s.getInviteWhere(`token_hash=?`, HashToken(rawToken))
}

func (s *Store) getInviteWhere(where string, arg any) (*models.Invite, error) {
	inv := &models.Invite{}
	var created, expires, used, revoked int64
	err := s.QueryRow(`SELECT id,token_cipher,email,created_by_account_id,created_at,expires_at,used_at,revoked_at FROM invites WHERE `+where, arg).
		Scan(&inv.ID, &inv.Token, &inv.Email, &inv.CreatedByAccountID, &created, &expires, &used, &revoked)
	if err != nil {
		return nil, err
	}
	inv.CreatedAt = time.Unix(created, 0)
	inv.ExpiresAt = time.Unix(expires, 0)
	if used > 0 {
		inv.UsedAt = time.Unix(used, 0)
	}
	if revoked > 0 {
		inv.RevokedAt = time.Unix(revoked, 0)
	}
	return inv, nil
}

func (s *Store) ListInvites() ([]models.Invite, error) {
	rows, err := s.Query(`SELECT id,token_cipher,email,created_by_account_id,created_at,expires_at,used_at,revoked_at FROM invites ORDER BY created_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Invite
	for rows.Next() {
		var inv models.Invite
		var created, expires, used, revoked int64
		if err := rows.Scan(&inv.ID, &inv.Token, &inv.Email, &inv.CreatedByAccountID, &created, &expires, &used, &revoked); err != nil {
			return nil, err
		}
		inv.CreatedAt = time.Unix(created, 0)
		inv.ExpiresAt = time.Unix(expires, 0)
		if used > 0 {
			inv.UsedAt = time.Unix(used, 0)
		}
		if revoked > 0 {
			inv.RevokedAt = time.Unix(revoked, 0)
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *Store) ConsumeInvite(id int64) error {
	res, err := s.Exec(`UPDATE invites SET used_at=? WHERE id=? AND used_at=0 AND revoked_at=0`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (s *Store) RevokeInvite(id int64) error {
	res, err := s.Exec(`UPDATE invites SET revoked_at=? WHERE id=? AND used_at=0 AND revoked_at=0`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}
