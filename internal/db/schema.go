package db

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT UNIQUE,
	name TEXT NOT NULL,
	password_hash TEXT NOT NULL DEFAULT '',
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
	visibility TEXT NOT NULL DEFAULT 'password',
	password_hash TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	upload_id INTEGER NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
	element_selector TEXT NOT NULL,
	element_text TEXT NOT NULL DEFAULT '',
	anchor_kind TEXT NOT NULL DEFAULT '',
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
