package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
CREATE TABLE IF NOT EXISTS tokens (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT UNIQUE NOT NULL,
	name TEXT NOT NULL,
	is_admin INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS uploads (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slug TEXT UNIQUE NOT NULL,
	owner_token_id INTEGER NOT NULL REFERENCES tokens(id),
	filename TEXT NOT NULL,
	size INTEGER NOT NULL,
	password_hash TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_uploads_owner ON uploads(owner_token_id);

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
	if err := store.migrateTokenHashes(); err != nil {
		return nil, err
	}
	return store, nil
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

func (s *Store) CreateToken(token, name string, isAdmin bool) error {
	_, err := s.Exec(`INSERT INTO tokens(token,name,is_admin,created_at) VALUES(?,?,?,?)`,
		HashToken(token), name, boolToInt(isAdmin), time.Now().Unix())
	return err
}

func (s *Store) GetToken(token string) (*models.Token, error) {
	return s.getTokenWhere(`token=?`, HashToken(token))
}

func (s *Store) GetTokenByID(id int64) (*models.Token, error) {
	return s.getTokenWhere(`id=?`, id)
}

func (s *Store) getTokenWhere(where string, arg any) (*models.Token, error) {
	t := &models.Token{}
	var isAdmin, ts int64
	err := s.QueryRow(`SELECT id,token,name,is_admin,created_at FROM tokens WHERE `+where, arg).
		Scan(&t.ID, &t.Token, &t.Name, &isAdmin, &ts)
	if err != nil {
		return nil, err
	}
	t.IsAdmin = isAdmin == 1
	t.CreatedAt = time.Unix(ts, 0)
	return t, nil
}

func (s *Store) DeleteToken(id int64) error {
	_, err := s.Exec(`DELETE FROM tokens WHERE id=?`, id)
	return err
}

func (s *Store) CountAdminTokens() (int, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM tokens WHERE is_admin=1`).Scan(&n)
	return n, err
}

func (s *Store) ListTokens() ([]models.Token, error) {
	rows, err := s.Query(`SELECT id,token,name,is_admin,created_at FROM tokens ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Token
	for rows.Next() {
		var t models.Token
		var isAdmin, ts int64
		if err := rows.Scan(&t.ID, &t.Token, &t.Name, &isAdmin, &ts); err != nil {
			return nil, err
		}
		t.IsAdmin = isAdmin == 1
		t.CreatedAt = time.Unix(ts, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CountTokens() (int, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM tokens`).Scan(&n)
	return n, err
}

func (s *Store) CreateUpload(slug string, ownerID int64, filename string, size int64, passwordHash string) error {
	_, err := s.Exec(`INSERT INTO uploads(slug,owner_token_id,filename,size,password_hash,created_at) VALUES(?,?,?,?,?,?)`,
		slug, ownerID, filename, size, passwordHash, time.Now().Unix())
	return err
}

func (s *Store) GetUpload(slug string) (*models.Upload, error) {
	u := &models.Upload{}
	var ts int64
	err := s.QueryRow(`SELECT u.id,u.slug,u.owner_token_id,t.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN tokens t ON t.id=u.owner_token_id WHERE u.slug=?`, slug).
		Scan(&u.ID, &u.Slug, &u.OwnerTokenID, &u.OwnerName, &u.Filename, &u.Size, &u.PasswordHash, &ts)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = time.Unix(ts, 0)
	return u, nil
}

func (s *Store) GetUploadByID(id int64) (*models.Upload, error) {
	u := &models.Upload{}
	var ts int64
	err := s.QueryRow(`SELECT u.id,u.slug,u.owner_token_id,t.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN tokens t ON t.id=u.owner_token_id WHERE u.id=?`, id).
		Scan(&u.ID, &u.Slug, &u.OwnerTokenID, &u.OwnerName, &u.Filename, &u.Size, &u.PasswordHash, &ts)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = time.Unix(ts, 0)
	return u, nil
}

func (s *Store) ListUploadsByOwner(ownerID int64) ([]models.Upload, error) {
	rows, err := s.Query(`SELECT u.id,u.slug,u.owner_token_id,t.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN tokens t ON t.id=u.owner_token_id WHERE u.owner_token_id=? ORDER BY u.created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	return scanUploads(rows)
}

func (s *Store) ListAllUploads() ([]models.Upload, error) {
	rows, err := s.Query(`SELECT u.id,u.slug,u.owner_token_id,t.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN tokens t ON t.id=u.owner_token_id ORDER BY u.created_at DESC`)
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
		if err := rows.Scan(&u.ID, &u.Slug, &u.OwnerTokenID, &u.OwnerName, &u.Filename, &u.Size, &u.PasswordHash, &ts); err != nil {
			return nil, err
		}
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
	err := s.QueryRow(`SELECT COUNT(*) FROM uploads WHERE owner_token_id=?`, ownerID).Scan(&n)
	return n, err
}

func (s *Store) ListUploadsOlderThan(cutoff time.Time) ([]models.Upload, error) {
	rows, err := s.Query(`SELECT u.id,u.slug,u.owner_token_id,t.name,u.filename,u.size,u.password_hash,u.created_at
		FROM uploads u JOIN tokens t ON t.id=u.owner_token_id WHERE u.created_at < ? ORDER BY u.created_at ASC`,
		cutoff.Unix())
	if err != nil {
		return nil, err
	}
	return scanUploads(rows)
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
