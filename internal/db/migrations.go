package db

import (
	"fmt"
	"strings"
	"time"
)

var migrations = []struct {
	id   int
	desc string
	sql  string
}{
	{1, "token_hashes", ""}, // handled by migrateTokenHashes (legacy compat)
	{2, "token_expiry", `-- ALTER TABLE handled idempotently below`},
	{3, "audit_log", `
CREATE TABLE IF NOT EXISTS audit_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	actor TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at DESC);`},
	{4, "account_password_hash", `-- ALTER TABLE handled idempotently below`},
}

func (s *Store) runMigrations() error {
	// Create the migrations tracking table.
	if _, err := s.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		id INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return err
	}

	// Run legacy migration first (pre-migration-system).
	if err := s.migrateTokenHashes(); err != nil {
		return err
	}
	// Mark migration 1 as applied if not already.
	if err := s.markMigrationApplied(1); err != nil {
		return err
	}

	for _, m := range migrations {
		applied, err := s.isMigrationApplied(m.id)
		if err != nil {
			return fmt.Errorf("read migration %d (%s): %w", m.id, m.desc, err)
		}
		if applied {
			continue
		}
		if m.id == 2 {
			if err := s.migrateTokenExpiry(); err != nil {
				return fmt.Errorf("migration %d (%s): %w", m.id, m.desc, err)
			}
		} else if m.id == 4 {
			if err := s.migrateAccountPasswordHash(); err != nil {
				return fmt.Errorf("migration %d (%s): %w", m.id, m.desc, err)
			}
		} else if m.sql != "" {
			if _, err := s.Exec(m.sql); err != nil {
				return fmt.Errorf("migration %d (%s): %w", m.id, m.desc, err)
			}
		}
		if err := s.markMigrationApplied(m.id); err != nil {
			return fmt.Errorf("mark migration %d (%s): %w", m.id, m.desc, err)
		}
	}
	return nil
}

func (s *Store) isMigrationApplied(id int) (bool, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE id=?`, id).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Store) markMigrationApplied(id int) error {
	_, err := s.Exec(`INSERT OR IGNORE INTO schema_migrations(id, applied_at) VALUES(?, ?)`,
		id, time.Now().Unix())
	return err
}

// migrateTokenExpiry adds the expires_at column to the tokens table for
// existing installs that predate it.
func (s *Store) migrateTokenExpiry() error {
	_, err := s.Exec(`ALTER TABLE tokens ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		if isDuplicateColumnError(err) {
			return nil
		}
		return err
	}
	return nil
}

// migrateAccountPasswordHash adds optional local-login password hashes to
// accounts. Empty means the account cannot use password login.
func (s *Store) migrateAccountPasswordHash() error {
	_, err := s.Exec(`ALTER TABLE accounts ADD COLUMN password_hash TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		if isDuplicateColumnError(err) {
			return nil
		}
		return err
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}

// migrateTokenHashes upgrades any legacy plaintext token rows to hashes, so an
// existing install keeps working after the move to hashed-at-rest tokens.
func (s *Store) migrateTokenHashes() error {
	rows, err := s.Query(`SELECT id, token FROM tokens`)
	if err != nil {
		return err
	}
	type row struct {
		id  int64
		tok string
	}
	var legacy []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.tok); err != nil {
			rows.Close()
			return err
		}
		if !isHashed(r.tok) {
			legacy = append(legacy, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range legacy {
		if _, err := s.Exec(`UPDATE tokens SET token=? WHERE id=?`, HashToken(r.tok), r.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateAccounts() error {
	hasAccountID, _, err := s.columnInfo("tokens", "account_id")
	if err != nil {
		return err
	}
	if !hasAccountID {
		if _, err := s.Exec(`ALTER TABLE tokens ADD COLUMN account_id INTEGER REFERENCES accounts(id)`); err != nil {
			return err
		}
	}

	rows, err := s.Query(`SELECT id,name,is_admin FROM tokens WHERE account_id IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type tokenRow struct {
		id      int64
		name    string
		isAdmin int64
	}
	var tokens []tokenRow
	for rows.Next() {
		var tr tokenRow
		if err := rows.Scan(&tr.id, &tr.name, &tr.isAdmin); err != nil {
			return err
		}
		tokens = append(tokens, tr)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, tr := range tokens {
		now := time.Now().Unix()
		res, err := s.Exec(`INSERT INTO accounts(email,name,is_admin,disabled,created_at,updated_at) VALUES(NULL,?,?,?,?,?)`,
			tr.name, tr.isAdmin, 0, now, now)
		if err != nil {
			return err
		}
		accountID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := s.Exec(`UPDATE tokens SET account_id=? WHERE id=?`, accountID, tr.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateUploadsOwner() (err error) {
	hasOwnerAccount, _, err := s.columnInfo("uploads", "owner_account_id")
	if err != nil {
		return err
	}
	_, ownerTokenNotNull, err := s.columnInfo("uploads", "owner_token_id")
	if err != nil {
		return err
	}
	if hasOwnerAccount && !ownerTokenNotNull {
		return nil
	}

	if _, err := s.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer func() {
		if _, onErr := s.Exec(`PRAGMA foreign_keys=ON`); err == nil && onErr != nil {
			err = onErr
		}
	}()
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TABLE uploads_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		slug TEXT UNIQUE NOT NULL,
		owner_account_id INTEGER NOT NULL REFERENCES accounts(id),
		owner_token_id INTEGER REFERENCES tokens(id),
		filename TEXT NOT NULL,
		size INTEGER NOT NULL,
		password_hash TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	)`); err != nil {
		return err
	}

	copySQL := `INSERT INTO uploads_new(id,slug,owner_account_id,owner_token_id,filename,size,password_hash,created_at)
		SELECT u.id,u.slug,t.account_id,u.owner_token_id,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN tokens t ON t.id=u.owner_token_id`
	if hasOwnerAccount {
		copySQL = `INSERT INTO uploads_new(id,slug,owner_account_id,owner_token_id,filename,size,password_hash,created_at)
			SELECT u.id,u.slug,COALESCE(u.owner_account_id,t.account_id),u.owner_token_id,u.filename,u.size,u.password_hash,u.created_at
			FROM uploads u LEFT JOIN tokens t ON t.id=u.owner_token_id`
	}
	if _, err := tx.Exec(copySQL); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE uploads`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE uploads_new RENAME TO uploads`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) migrateInviteTokenCipher() error {
	hasCipher, _, err := s.columnInfo("invites", "token_cipher")
	if err != nil {
		return err
	}
	if hasCipher {
		return nil
	}
	_, err = s.Exec(`ALTER TABLE invites ADD COLUMN token_cipher TEXT NOT NULL DEFAULT ''`)
	return err
}

func (s *Store) columnInfo(table, column string) (exists, notNull bool, err error) {
	rows, err := s.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var nn, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &nn, &dflt, &pk); err != nil {
			return false, false, err
		}
		if name == column {
			return true, nn == 1, nil
		}
	}
	return false, false, rows.Err()
}
