package db

import (
	"path/filepath"
	"testing"
	"time"
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
	if got.Name != "test" || got.IsAdmin || got.Token == token {
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
	if err := s.CreateUpload("slug1", tok.ID, "page.html", 42, ""); err != nil {
		t.Fatalf("create upload: %v", err)
	}
	got, err := s.GetUpload("slug1")
	if err != nil {
		t.Fatalf("get upload: %v", err)
	}
	if got.Slug != "slug1" || got.Filename != "page.html" || got.Size != 42 || got.OwnerTokenID != tok.ID {
		t.Fatalf("upload mismatch: %+v", got)
	}
}

func TestAddListComments(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()
	if err := s.CreateToken("owner", "owner", false, 0); err != nil {
		t.Fatalf("create token: %v", err)
	}
	tok, _ := s.GetToken("owner")
	if err := s.CreateUpload("slug2", tok.ID, "page.html", 0, ""); err != nil {
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
	if err := s.CreateToken("b", "second", true, time.Now().Add(time.Hour).Unix()); err != nil {
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
}
