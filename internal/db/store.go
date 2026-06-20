package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/puemos/peek/internal/models"
)

// HashToken returns the hex SHA-256 of a bearer token. Tokens are stored only
// as this hash, so a database/backup leak does not expose usable credentials.
// SHA-256 (no salt) is appropriate here because tokens are 192-bit random
// values, not low-entropy passwords.
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

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT UNIQUE,
	name TEXT NOT NULL,
	is_admin INTEGER NOT NULL DEFAULT 0,
	disabled INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS tokens (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id INTEGER REFERENCES accounts(id) ON DELETE CASCADE,
	token TEXT UNIQUE NOT NULL,
	name TEXT NOT NULL,
	is_admin INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS uploads (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slug TEXT UNIQUE NOT NULL,
	owner_account_id INTEGER NOT NULL REFERENCES accounts(id),
	owner_token_id INTEGER REFERENCES tokens(id),
	filename TEXT NOT NULL,
	size INTEGER NOT NULL,
	password_hash TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	upload_id INTEGER NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
	element_selector TEXT NOT NULL,
	element_text TEXT NOT NULL DEFAULT '',
	author_name TEXT NOT NULL,
	author_cookie TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_comments_upload ON comments(upload_id);

CREATE TABLE IF NOT EXISTS visits (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	upload_id INTEGER NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
	visitor_cookie TEXT NOT NULL,
	visitor_name TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT '',
	visited_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_visits_upload ON visits(upload_id);

CREATE TABLE IF NOT EXISTS visitors (
	cookie TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	last_seen INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_identities (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	provider TEXT NOT NULL,
	provider_user_id TEXT NOT NULL,
	email TEXT NOT NULL,
	name TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	UNIQUE(provider, provider_user_id)
);

CREATE INDEX IF NOT EXISTS idx_oauth_account ON oauth_identities(account_id);

CREATE TABLE IF NOT EXISTS invites (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token_hash TEXT UNIQUE NOT NULL,
	token_cipher TEXT NOT NULL DEFAULT '',
	email TEXT NOT NULL,
	created_by_account_id INTEGER NOT NULL REFERENCES accounts(id),
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at INTEGER NOT NULL DEFAULT 0,
	revoked_at INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_invites_email ON invites(email);

CREATE TABLE IF NOT EXISTS cli_login_devices (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	device_hash TEXT UNIQUE NOT NULL,
	user_code TEXT UNIQUE NOT NULL,
	status TEXT NOT NULL,
	account_id INTEGER REFERENCES accounts(id),
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	consumed_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS audit_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	actor TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at DESC);
`

type Store struct {
	*sql.DB
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	store := &Store{db}
	if err := store.runMigrations(); err != nil {
		return nil, err
	}
	if err := store.migrateAccounts(); err != nil {
		return nil, err
	}
	if err := store.migrateUploadsOwner(); err != nil {
		return nil, err
	}
	if err := store.migrateInviteTokenCipher(); err != nil {
		return nil, err
	}
	if _, err := store.Exec(`CREATE INDEX IF NOT EXISTS idx_uploads_owner_account ON uploads(owner_account_id)`); err != nil {
		return nil, err
	}
	return store, nil
}

// migrations is an ordered list of schema migrations. Each migration has a
// unique integer ID. Migrations that have already been applied (recorded in
// the schema_migrations table) are skipped.
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
	s.markMigrationApplied(1)

	for _, m := range migrations {
		if s.isMigrationApplied(m.id) {
			continue
		}
		if m.id == 2 {
			if err := s.migrateTokenExpiry(); err != nil {
				return fmt.Errorf("migration %d (%s): %w", m.id, m.desc, err)
			}
		} else if m.sql != "" {
			if _, err := s.Exec(m.sql); err != nil {
				return fmt.Errorf("migration %d (%s): %w", m.id, m.desc, err)
			}
		}
		s.markMigrationApplied(m.id)
	}
	return nil
}

func (s *Store) isMigrationApplied(id int) bool {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE id=?`, id).Scan(&n)
	return err == nil && n > 0
}

func (s *Store) markMigrationApplied(id int) {
	_, _ = s.Exec(`INSERT OR IGNORE INTO schema_migrations(id, applied_at) VALUES(?, ?)`,
		id, time.Now().Unix())
}

// migrateTokenExpiry adds the expires_at column to the tokens table for
// existing installs that predate it.
func (s *Store) migrateTokenExpiry() error {
	_, err := s.Exec(`ALTER TABLE tokens ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		// Column already exists — ignore the error.
		return nil
	}
	return nil
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

func (s *Store) migrateUploadsOwner() error {
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
	if _, err := s.Exec(`PRAGMA foreign_keys=ON`); err != nil {
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

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Store) CreateAccount(email, name string, isAdmin bool) (*models.Account, error) {
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
	res, err := s.Exec(`INSERT INTO accounts(email,name,is_admin,disabled,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		emailArg, name, boolToInt(isAdmin), 0, now, now)
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
	err := s.QueryRow(`SELECT id,email,name,is_admin,disabled,created_at,updated_at FROM accounts WHERE `+where, arg).
		Scan(&a.ID, &email, &a.Name, &isAdmin, &disabled, &created, &updated)
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
	rows, err := s.Query(`SELECT id,email,name,is_admin,disabled,created_at,updated_at FROM accounts ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Account
	for rows.Next() {
		var a models.Account
		var email sql.NullString
		var isAdmin, disabled, created, updated int64
		if err := rows.Scan(&a.ID, &email, &a.Name, &isAdmin, &disabled, &created, &updated); err != nil {
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

func (s *Store) SetAccountDisabled(id int64, disabled bool) error {
	_, err := s.Exec(`UPDATE accounts SET disabled=?, updated_at=? WHERE id=?`, boolToInt(disabled), time.Now().Unix(), id)
	return err
}

func (s *Store) CountAdminAccounts() (int, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM accounts WHERE is_admin=1 AND disabled=0`).Scan(&n)
	return n, err
}

func (s *Store) CreateToken(token, name string, isAdmin bool, expiresAt int64) error {
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	res, err := tx.Exec(`INSERT INTO accounts(email,name,is_admin,disabled,created_at,updated_at) VALUES(NULL,?,?,?,?,?)`,
		strings.TrimSpace(name), boolToInt(isAdmin), 0, now, now)
	if err != nil {
		return err
	}
	accountID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO tokens(account_id,token,name,is_admin,created_at,expires_at) VALUES(?,?,?,?,?,?)`,
		accountID, HashToken(token), strings.TrimSpace(name), boolToInt(isAdmin), now, expiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateTokenForAccount(token string, accountID int64, name string) error {
	if strings.TrimSpace(name) == "" {
		name = "API token"
	}
	_, err := s.Exec(`INSERT INTO tokens(account_id,token,name,is_admin,created_at) VALUES(?,?,?,?,?)`,
		accountID, HashToken(token), strings.TrimSpace(name), 0, time.Now().Unix())
	return err
}

func (s *Store) GetToken(token string) (*models.Token, error) {
	return s.getTokenWhere(`tk.token=?`, HashToken(token))
}

func (s *Store) GetTokenByID(id int64) (*models.Token, error) {
	return s.getTokenWhere(`tk.id=?`, id)
}

func (s *Store) getTokenWhere(where string, arg any) (*models.Token, error) {
	t := &models.Token{}
	var isAdmin, disabled, ts, exp int64
	var email sql.NullString
	err := s.QueryRow(`SELECT tk.id,tk.account_id,tk.token,a.name,a.email,a.is_admin,a.disabled,tk.created_at,tk.expires_at
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

func (s *Store) DeleteToken(id int64) error {
	_, err := s.Exec(`DELETE FROM tokens WHERE id=?`, id)
	return err
}

func (s *Store) CountAdminTokens() (int, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM tokens tk JOIN accounts a ON a.id=tk.account_id WHERE a.is_admin=1 AND a.disabled=0`).Scan(&n)
	return n, err
}

func (s *Store) ListTokens() ([]models.Token, error) {
	rows, err := s.Query(`SELECT tk.id,tk.account_id,tk.token,a.name,a.email,a.is_admin,a.disabled,tk.created_at,tk.expires_at
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

func (s *Store) CountTokens() (int, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM tokens`).Scan(&n)
	return n, err
}

func (s *Store) CreateUpload(slug string, ownerAccountID, ownerTokenID int64, filename string, size int64, passwordHash string) error {
	var tokenArg any
	if ownerTokenID > 0 {
		tokenArg = ownerTokenID
	}
	_, err := s.Exec(`INSERT INTO uploads(slug,owner_account_id,owner_token_id,filename,size,password_hash,created_at) VALUES(?,?,?,?,?,?,?)`,
		slug, ownerAccountID, tokenArg, filename, size, passwordHash, time.Now().Unix())
	return err
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

func (s *Store) AddComment(uploadID int64, selector, text, author, cookie, body string) error {
	_, err := s.Exec(`INSERT INTO comments(upload_id,element_selector,element_text,author_name,author_cookie,body,created_at)
		VALUES(?,?,?,?,?,?,?)`, uploadID, selector, text, author, cookie, body, time.Now().Unix())
	return err
}

func (s *Store) ListComments(uploadID int64) ([]models.Comment, error) {
	rows, err := s.Query(`SELECT id,upload_id,element_selector,element_text,author_name,author_cookie,body,created_at
		FROM comments WHERE upload_id=? ORDER BY created_at ASC`, uploadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Comment
	for rows.Next() {
		var c models.Comment
		var ts int64
		if err := rows.Scan(&c.ID, &c.UploadID, &c.ElementSelector, &c.ElementText, &c.AuthorName, &c.AuthorCookie, &c.Body, &ts); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(ts, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) RecordVisit(uploadID int64, cookie, name, ip, ua string) error {
	_, err := s.Exec(`INSERT INTO visits(upload_id,visitor_cookie,visitor_name,ip,user_agent,visited_at)
		VALUES(?,?,?,?,?,?)`, uploadID, cookie, name, ip, ua, time.Now().Unix())
	return err
}

func (s *Store) CountVisits(uploadID int64) (total, unique int, err error) {
	err = s.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT visitor_cookie) FROM visits WHERE upload_id=?`, uploadID).Scan(&total, &unique)
	return
}

func (s *Store) RecentVisits(uploadID int64, limit int) ([]models.Visit, error) {
	rows, err := s.Query(`SELECT id,upload_id,visitor_cookie,visitor_name,ip,user_agent,visited_at
		FROM visits WHERE upload_id=? ORDER BY visited_at DESC LIMIT ?`, uploadID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Visit
	for rows.Next() {
		var v models.Visit
		var ts int64
		if err := rows.Scan(&v.ID, &v.UploadID, &v.VisitorCookie, &v.VisitorName, &v.IP, &v.UserAgent, &ts); err != nil {
			return nil, err
		}
		v.VisitedAt = time.Unix(ts, 0)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertVisitor(cookie, name string) error {
	now := time.Now().Unix()
	_, err := s.Exec(`INSERT INTO visitors(cookie,name,created_at,last_seen) VALUES(?,?,?,?)
		ON CONFLICT(cookie) DO UPDATE SET last_seen=excluded.last_seen, name=CASE WHEN excluded.name='' THEN visitors.name ELSE excluded.name END`,
		cookie, name, now, now)
	return err
}

func (s *Store) GetVisitor(cookie string) (*models.Visitor, error) {
	v := &models.Visitor{}
	var created, last int64
	err := s.QueryRow(`SELECT cookie,name,created_at,last_seen FROM visitors WHERE cookie=?`, cookie).
		Scan(&v.Cookie, &v.Name, &created, &last)
	if err != nil {
		return nil, err
	}
	v.CreatedAt = time.Unix(created, 0)
	v.LastSeen = time.Unix(last, 0)
	return v, nil
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

func (s *Store) GetOAuthIdentity(provider, providerUserID string) (*models.OAuthIdentity, error) {
	oi := &models.OAuthIdentity{}
	var created, updated int64
	err := s.QueryRow(`SELECT id,account_id,provider,provider_user_id,email,name,created_at,updated_at
		FROM oauth_identities WHERE provider=? AND provider_user_id=?`, provider, providerUserID).
		Scan(&oi.ID, &oi.AccountID, &oi.Provider, &oi.ProviderUserID, &oi.Email, &oi.Name, &created, &updated)
	if err != nil {
		return nil, err
	}
	oi.CreatedAt = time.Unix(created, 0)
	oi.UpdatedAt = time.Unix(updated, 0)
	return oi, nil
}

func (s *Store) UpsertOAuthIdentity(accountID int64, provider, providerUserID, email, name string) error {
	now := time.Now().Unix()
	_, err := s.Exec(`INSERT INTO oauth_identities(account_id,provider,provider_user_id,email,name,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(provider,provider_user_id) DO UPDATE SET
			account_id=excluded.account_id,
			email=excluded.email,
			name=excluded.name,
			updated_at=excluded.updated_at`,
		accountID, provider, providerUserID, normalizeEmail(email), strings.TrimSpace(name), now, now)
	return err
}

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
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) RevokeInvite(id int64) error {
	_, err := s.Exec(`UPDATE invites SET revoked_at=? WHERE id=? AND used_at=0`, time.Now().Unix(), id)
	return err
}

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
	_, err := s.Exec(`UPDATE cli_login_devices SET status='approved', account_id=? WHERE id=? AND status='pending'`, accountID, id)
	return err
}

func (s *Store) DenyCLILogin(id int64) error {
	_, err := s.Exec(`UPDATE cli_login_devices SET status='denied' WHERE id=? AND status='pending'`, id)
	return err
}

func (s *Store) ConsumeCLILogin(id int64) error {
	res, err := s.Exec(`UPDATE cli_login_devices SET consumed_at=? WHERE id=? AND consumed_at=0`, time.Now().Unix(), id)
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
	return nil
}

func (s *Store) ExpireCLILogin(id int64) error {
	_, err := s.Exec(`UPDATE cli_login_devices SET status='expired' WHERE id=? AND status='pending'`, id)
	return err
}

func (s *Store) GetSetting(key string) (string, error) {
	var value string
	err := s.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().Unix())
	return err
}

func (s *Store) GetAllSettings() (map[string]string, error) {
	rows, err := s.Query(`SELECT key,value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) AddAuditLog(actor, action, detail, ip string) error {
	_, err := s.Exec(`INSERT INTO audit_log(actor,action,detail,ip,created_at) VALUES(?,?,?,?,?)`,
		actor, action, detail, ip, time.Now().Unix())
	return err
}

type AuditEntry struct {
	ID        int64
	Actor     string
	Action    string
	Detail    string
	IP        string
	CreatedAt time.Time
}

func (s *Store) ListAuditLog(limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.Query(`SELECT id,actor,action,detail,ip,created_at FROM audit_log ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts int64
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Detail, &e.IP, &ts); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}
