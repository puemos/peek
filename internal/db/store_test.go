package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/puemos/peek/internal/uploadquota"

	_ "modernc.org/sqlite"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "peek.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s
}

func TestCreateGetToken(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	token := "token-secret-abc"
	if err := s.CreateToken(token, "test", false, 0); err != nil {
		t.Fatalf("create token: %v", err)
	}
	got, err := s.GetToken(token)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if got.Name != "test" || got.IsAdmin || got.Token == token || got.AccountID == 0 {
		t.Fatalf("token mismatch: %+v", got)
	}
	if got.Token != HashToken(token) {
		t.Fatalf("expected token to be stored as hash")
	}
}

func TestTokenExpiry(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	expired := "expired-token"
	if err := s.CreateToken(expired, "expired", false, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := s.GetToken(expired); err == nil {
		t.Fatalf("expected expired token to be rejected")
	}
	fresh := "fresh-token"
	if err := s.CreateToken(fresh, "fresh", false, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := s.GetToken(fresh); err != nil {
		t.Fatalf("expected valid token to be returned: %v", err)
	}
}

func TestTokenNoExpiry(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.CreateToken("no-exp", "no-exp", false, 0); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := s.GetToken("no-exp"); err != nil {
		t.Fatalf("token with no expiry should be valid: %v", err)
	}
}

func TestCreateGetUpload(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.CreateToken("owner", "owner", false, 0); err != nil {
		t.Fatalf("create token: %v", err)
	}
	tok, err := s.GetToken("owner")
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if err := s.CreateUpload("slug1", tok.AccountID, tok.ID, "page.html", 42, ""); err != nil {
		t.Fatalf("create upload: %v", err)
	}
	got, err := s.GetUpload("slug1")
	if err != nil {
		t.Fatalf("get upload: %v", err)
	}
	if got.Slug != "slug1" || got.Filename != "page.html" || got.Size != 42 || got.OwnerTokenID != tok.ID || got.OwnerAccountID != tok.AccountID {
		t.Fatalf("upload mismatch: %+v", got)
	}
	total, err := s.SumUploadSizesByOwner(tok.AccountID)
	if err != nil {
		t.Fatalf("sum owner sizes: %v", err)
	}
	if total != 42 {
		t.Fatalf("expected owner size 42, got %d", total)
	}
}

func TestUploadMutationsReportMissingRows(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	if err := s.SetUploadPassword(999, "hash"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("set password on missing upload should fail with sql.ErrNoRows, got %v", err)
	}
	if err := s.DeleteUpload(999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("delete missing upload should fail with sql.ErrNoRows, got %v", err)
	}
}

func TestAddListComments(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.CreateToken("owner", "owner", false, 0); err != nil {
		t.Fatalf("create token: %v", err)
	}
	tok, _ := s.GetToken("owner")
	if err := s.CreateUpload("slug2", tok.AccountID, tok.ID, "page.html", 0, ""); err != nil {
		t.Fatalf("create upload: %v", err)
	}
	up, _ := s.GetUpload("slug2")
	if err := s.AddComment(up.ID, "#id", "text", "Alice", "cookie-a", "comment body"); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if err := s.AddComment(up.ID, "#id2", "text2", "Bob", "cookie-b", "second"); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	comments, err := s.ListComments(up.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].AuthorName != "Alice" || comments[1].AuthorName != "Bob" {
		t.Fatalf("comment order or content mismatch: %+v", comments)
	}
}

func TestAddListAuditLog(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.AddAuditLog("admin", "settings.update", "key=retention", "127.0.0.1"); err != nil {
		t.Fatalf("add audit log: %v", err)
	}
	if err := s.AddAuditLog("user", "upload.create", "slug=foo", "127.0.0.1"); err != nil {
		t.Fatalf("add audit log: %v", err)
	}
	entries, err := s.ListAuditLog(100)
	if err != nil {
		t.Fatalf("list audit log: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	actions := map[string]bool{}
	for _, e := range entries {
		actions[e.Action] = true
	}
	if !actions["settings.update"] || !actions["upload.create"] {
		t.Fatalf("expected both actions, got %+v", entries)
	}
}

func TestSettingsGetSet(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.SetSetting("foo", "bar"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	got, err := s.GetSetting("foo")
	if err != nil {
		t.Fatalf("get setting: %v", err)
	}
	if got != "bar" {
		t.Fatalf("expected setting value bar, got %q", got)
	}
	if err := s.SetSetting("foo", "baz"); err != nil {
		t.Fatalf("update setting: %v", err)
	}
	got, _ = s.GetSetting("foo")
	if got != "baz" {
		t.Fatalf("expected updated value baz, got %q", got)
	}
	all, err := s.GetAllSettings()
	if err != nil {
		t.Fatalf("get all settings: %v", err)
	}
	if all["foo"] != "baz" {
		t.Fatalf("expected all settings to include foo=baz, got %+v", all)
	}
}

func TestGetMissingSetting(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if _, err := s.GetSetting("missing"); err == nil {
		t.Fatalf("expected error for missing setting")
	}
}

func TestListTokens(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.CreateToken("a", "first", false, 0); err != nil {
		t.Fatalf("create token: %v", err)
	}
	exp := time.Now().Add(time.Hour).Unix()
	if err := s.CreateToken("b", "second", true, exp); err != nil {
		t.Fatalf("create token: %v", err)
	}
	tokens, err := s.ListTokens()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if !tokens[0].IsAdmin && !tokens[1].IsAdmin {
		t.Fatalf("expected one admin token")
	}
	if tokens[0].AccountID == 0 || tokens[1].AccountID == 0 {
		t.Fatalf("expected account-backed tokens: %+v", tokens)
	}
	if tokens[1].ExpiresAt != exp && tokens[0].ExpiresAt != exp {
		t.Fatalf("expected list to include expiry %d, got %+v", exp, tokens)
	}
}

func TestLegacyAuthMigrationPreservesTokenAndUploadOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peek.db")
	legacy, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
CREATE TABLE tokens (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT UNIQUE NOT NULL,
	name TEXT NOT NULL,
	is_admin INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE TABLE uploads (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slug TEXT UNIQUE NOT NULL,
	owner_token_id INTEGER NOT NULL REFERENCES tokens(id),
	filename TEXT NOT NULL,
	size INTEGER NOT NULL,
	password_hash TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE TABLE comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	upload_id INTEGER NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
	element_selector TEXT NOT NULL,
	element_text TEXT NOT NULL DEFAULT '',
	author_name TEXT NOT NULL,
	author_cookie TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE visits (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	upload_id INTEGER NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
	visitor_cookie TEXT NOT NULL,
	visitor_name TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT '',
	visited_at INTEGER NOT NULL
);
CREATE TABLE visitors (
	cookie TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	last_seen INTEGER NOT NULL
);
CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);
INSERT INTO tokens(token,name,is_admin,created_at) VALUES('legacy-admin','Admin',1,100);
INSERT INTO uploads(slug,owner_token_id,filename,size,created_at) VALUES('abc',1,'page.html',10,101);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tok, err := store.GetToken("legacy-admin")
	if err != nil {
		t.Fatalf("legacy token should still authenticate: %v", err)
	}
	if !tok.IsAdmin || tok.AccountID == 0 || tok.Token == "legacy-admin" || tok.ExpiresAt != 0 {
		t.Fatalf("unexpected migrated token: %+v", tok)
	}
	up, err := store.GetUpload("abc")
	if err != nil {
		t.Fatal(err)
	}
	if up.OwnerAccountID != tok.AccountID || up.OwnerTokenID != tok.ID {
		t.Fatalf("upload ownership not migrated: upload=%+v token=%+v", up, tok)
	}
	if err := store.CreateUpload("oauth", tok.AccountID, 0, "oauth.html", 5, ""); err != nil {
		t.Fatalf("account-owned upload without token should be allowed: %v", err)
	}
	n, err := store.CountUploadsByOwner(tok.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 account uploads, got %d", n)
	}
}

func TestInviteAndCLILoginConsumptionAreOneTime(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateToken("admin-token", "Admin", true, 0); err != nil {
		t.Fatal(err)
	}
	admin, err := store.GetToken("admin-token")
	if err != nil {
		t.Fatal(err)
	}

	inv, err := store.CreateInvite("raw-invite", "ciphertext", "USER@Example.COM", admin.AccountID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetInviteByToken("raw-invite")
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "user@example.com" || got.Token != "ciphertext" {
		t.Fatalf("unexpected invite: %+v", got)
	}
	if err := store.ConsumeInvite(inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeInvite(inv.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second consume should fail, got %v", err)
	}
	if err := store.RevokeInvite(inv.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("revoking consumed invite should fail, got %v", err)
	}

	revoked, err := store.CreateInvite("raw-revoke", "ciphertext", "revoke@example.com", admin.AccountID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeInvite(revoked.ID); err != nil {
		t.Fatalf("revoke invite: %v", err)
	}
	if err := store.RevokeInvite(revoked.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second revoke should fail, got %v", err)
	}

	if err := store.CreateCLILoginDevice("device", "ABCDEFGH", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	device, err := store.GetCLILoginByDevice("device")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApproveCLILogin(device.ID, admin.AccountID); err != nil {
		t.Fatal(err)
	}
	if err := store.ApproveCLILogin(device.ID, admin.AccountID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second CLI approve should fail, got %v", err)
	}
	if err := store.DenyCLILogin(device.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("denying approved CLI login should fail, got %v", err)
	}
	if err := store.ExpireCLILogin(device.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expiring approved CLI login should fail, got %v", err)
	}
	if err := store.ConsumeCLILogin(device.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeCLILogin(device.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second CLI consume should fail, got %v", err)
	}

	if err := store.CreateCLILoginDevice("deny-device", "HJKLMNPQ", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	denyDevice, err := store.GetCLILoginByDevice("deny-device")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DenyCLILogin(denyDevice.ID); err != nil {
		t.Fatalf("deny CLI login: %v", err)
	}
	if err := store.DenyCLILogin(denyDevice.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second CLI deny should fail, got %v", err)
	}
	if err := store.ApproveCLILogin(denyDevice.ID, admin.AccountID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("approving denied CLI login should fail, got %v", err)
	}

	if err := store.CreateCLILoginDevice("expire-device", "RSTUVWXY", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	expireDevice, err := store.GetCLILoginByDevice("expire-device")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ExpireCLILogin(expireDevice.ID); err != nil {
		t.Fatalf("expire CLI login: %v", err)
	}
	if err := store.ExpireCLILogin(expireDevice.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second CLI expire should fail, got %v", err)
	}
	if err := store.ApproveCLILogin(expireDevice.ID, admin.AccountID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("approving expired CLI login should fail, got %v", err)
	}
}

func TestCreateUploadCheckedEnforcesQuotas(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.CreateToken("owner", "owner", false, 0); err != nil {
		t.Fatal(err)
	}
	tok, err := s.GetToken("owner")
	if err != nil {
		t.Fatal(err)
	}
	limits := uploadquota.Limits{MaxTotalSize: 15, MaxUploadsPerOwner: 1, MaxStoragePerOwner: 12}
	if err := s.CreateUploadChecked("one", tok.AccountID, tok.ID, "one.html", 10, "", limits); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	if err := s.CreateUploadChecked("two", tok.AccountID, tok.ID, "two.html", 1, "", limits); !errors.Is(err, uploadquota.ErrOwnerCountExceeded) {
		t.Fatalf("expected owner count quota, got %v", err)
	}

	if err := s.CreateToken("second", "second", false, 0); err != nil {
		t.Fatal(err)
	}
	tok2, err := s.GetToken("second")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUploadChecked("three", tok2.AccountID, tok2.ID, "three.html", 10, "", limits); !errors.Is(err, uploadquota.ErrTotalExceeded) {
		t.Fatalf("expected total quota, got %v", err)
	}
}

func TestUploadSlugExists(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.CreateToken("owner", "owner", false, 0); err != nil {
		t.Fatal(err)
	}
	tok, err := s.GetToken("owner")
	if err != nil {
		t.Fatal(err)
	}
	exists, err := s.UploadSlugExists("page")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("slug should not exist before upload")
	}
	if err := s.CreateUpload("page", tok.AccountID, tok.ID, "page.html", 10, ""); err != nil {
		t.Fatal(err)
	}
	exists, err = s.UploadSlugExists("page")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("slug should exist after upload")
	}
}

func TestCheckedAdminUpdatesPreserveLastAdmin(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.CreateToken("admin", "admin", true, 0); err != nil {
		t.Fatal(err)
	}
	admin, err := s.GetToken("admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccountAdminChecked(admin.AccountID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected last admin error, got %v", err)
	}
	if err := s.SetAccountDisabledChecked(admin.AccountID, true); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected last admin disable error, got %v", err)
	}

	if err := s.CreateToken("admin2", "admin2", true, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccountAdminChecked(admin.AccountID, false); err != nil {
		t.Fatalf("demote with second admin: %v", err)
	}
}

func TestDeleteTokenCheckedPreservesLastAdminAndAllowsExpiredDelete(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.CreateToken("admin", "admin", true, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.ListTokens()
	if err != nil {
		t.Fatal(err)
	}
	admin := tokens[0]
	if _, err := s.DeleteTokenChecked(admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected last admin token error, got %v", err)
	}
	if err := s.CreateToken("admin2", "admin2", true, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteTokenChecked(admin.ID); err != nil {
		t.Fatalf("delete expired admin token with another admin present: %v", err)
	}
}
