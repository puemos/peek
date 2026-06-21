package db

import (
	"database/sql"
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Store) CreateUpload(slug string, ownerAccountID, ownerTokenID int64, filename string, size int64, passwordHash string) error {
	var tokenArg any
	if ownerTokenID > 0 {
		tokenArg = ownerTokenID
	}
	_, err := s.Exec(`INSERT INTO uploads(slug,owner_account_id,owner_token_id,filename,size,password_hash,created_at) VALUES(?,?,?,?,?,?,?)`,
		slug, ownerAccountID, tokenArg, filename, size, passwordHash, time.Now().Unix())
	return err
}

func (s *Store) CreateUploadChecked(slug string, ownerAccountID, ownerTokenID int64, filename string, size int64, passwordHash string, limits UploadLimits) error {
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if limits.MaxTotalSize > 0 {
		var total int64
		if err := tx.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM uploads`).Scan(&total); err != nil {
			return err
		}
		if total+size > limits.MaxTotalSize {
			return ErrTotalQuotaExceeded
		}
	}
	if limits.MaxUploadsPerOwner > 0 {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM uploads WHERE owner_account_id=?`, ownerAccountID).Scan(&count); err != nil {
			return err
		}
		if count >= limits.MaxUploadsPerOwner {
			return ErrOwnerUploadCountQuotaExceeded
		}
	}
	if limits.MaxStoragePerOwner > 0 {
		var total int64
		if err := tx.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM uploads WHERE owner_account_id=?`, ownerAccountID).Scan(&total); err != nil {
			return err
		}
		if total+size > limits.MaxStoragePerOwner {
			return ErrOwnerStorageQuotaExceeded
		}
	}

	var tokenArg any
	if ownerTokenID > 0 {
		tokenArg = ownerTokenID
	}
	if _, err := tx.Exec(`INSERT INTO uploads(slug,owner_account_id,owner_token_id,filename,size,password_hash,created_at) VALUES(?,?,?,?,?,?,?)`,
		slug, ownerAccountID, tokenArg, filename, size, passwordHash, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetUpload(slug string) (*models.Upload, error) {
	u := &models.Upload{}
	var ts int64
	var tokenID sql.NullInt64
	err := s.QueryRow(`SELECT u.id,u.slug,u.owner_account_id,u.owner_token_id,a.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN accounts a ON a.id=u.owner_account_id WHERE u.slug=?`, slug).
		Scan(&u.ID, &u.Slug, &u.OwnerAccountID, &tokenID, &u.OwnerName, &u.Filename, &u.Size, &u.PasswordHash, &ts)
	if err != nil {
		return nil, err
	}
	u.OwnerTokenID = tokenID.Int64
	u.CreatedAt = time.Unix(ts, 0)
	return u, nil
}

func (s *Store) GetUploadByID(id int64) (*models.Upload, error) {
	u := &models.Upload{}
	var ts int64
	var tokenID sql.NullInt64
	err := s.QueryRow(`SELECT u.id,u.slug,u.owner_account_id,u.owner_token_id,a.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN accounts a ON a.id=u.owner_account_id WHERE u.id=?`, id).
		Scan(&u.ID, &u.Slug, &u.OwnerAccountID, &tokenID, &u.OwnerName, &u.Filename, &u.Size, &u.PasswordHash, &ts)
	if err != nil {
		return nil, err
	}
	u.OwnerTokenID = tokenID.Int64
	u.CreatedAt = time.Unix(ts, 0)
	return u, nil
}

func (s *Store) ListUploadsByOwner(ownerID int64) ([]models.Upload, error) {
	rows, err := s.Query(`SELECT u.id,u.slug,u.owner_account_id,u.owner_token_id,a.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN accounts a ON a.id=u.owner_account_id WHERE u.owner_account_id=? ORDER BY u.created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	return scanUploads(rows)
}

func (s *Store) ListAllUploads() ([]models.Upload, error) {
	rows, err := s.Query(`SELECT u.id,u.slug,u.owner_account_id,u.owner_token_id,a.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN accounts a ON a.id=u.owner_account_id ORDER BY u.created_at DESC`)
	if err != nil {
		return nil, err
	}
	return scanUploads(rows)
}

func (s *Store) SetUploadPassword(id int64, hash string) error {
	_, err := s.Exec(`UPDATE uploads SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (s *Store) DeleteUpload(id int64) error {
	_, err := s.Exec(`DELETE FROM uploads WHERE id=?`, id)
	return err
}

func scanUploads(rows *sql.Rows) ([]models.Upload, error) {
	defer rows.Close()
	var out []models.Upload
	for rows.Next() {
		var u models.Upload
		var ts int64
		var tokenID sql.NullInt64
		if err := rows.Scan(&u.ID, &u.Slug, &u.OwnerAccountID, &tokenID, &u.OwnerName, &u.Filename, &u.Size, &u.PasswordHash, &ts); err != nil {
			return nil, err
		}
		u.OwnerTokenID = tokenID.Int64
		u.CreatedAt = time.Unix(ts, 0)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CountUploads() (int, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM uploads`).Scan(&n)
	return n, err
}

func (s *Store) SumUploadSizes() (int64, error) {
	var total int64
	err := s.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM uploads`).Scan(&total)
	return total, err
}

func (s *Store) CountUploadsByOwner(ownerID int64) (int, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM uploads WHERE owner_account_id=?`, ownerID).Scan(&n)
	return n, err
}

func (s *Store) SumUploadSizesByOwner(ownerID int64) (int64, error) {
	var total int64
	err := s.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM uploads WHERE owner_account_id=?`, ownerID).Scan(&total)
	return total, err
}

func (s *Store) ListUploadsOlderThan(cutoff time.Time) ([]models.Upload, error) {
	rows, err := s.Query(`SELECT u.id,u.slug,u.owner_account_id,u.owner_token_id,a.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN accounts a ON a.id=u.owner_account_id WHERE u.created_at < ? ORDER BY u.created_at ASC`,
		cutoff.Unix())
	if err != nil {
		return nil, err
	}
	return scanUploads(rows)
}
