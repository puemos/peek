package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/puemos/peek/internal/db"
)

type retentionCleanupStorage struct {
	deleted []string
	err     error
}

func (s *retentionCleanupStorage) Save(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (s *retentionCleanupStorage) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (s *retentionCleanupStorage) Delete(_ context.Context, slug string) error {
	s.deleted = append(s.deleted, slug)
	return s.err
}

func TestRetentionCleanupKeepsUploadWhenStorageDeleteFails(t *testing.T) {
	s, store, storage, accountID := newRetentionCleanupTestServer(t)
	storage.err = errors.New("delete blocked")
	seedExpiredRetentionUpload(t, store, accountID, "expired-page")

	s.cleanupExpired(context.Background())

	if len(storage.deleted) != 1 || storage.deleted[0] != "expired-page" {
		t.Fatalf("storage deletes = %+v", storage.deleted)
	}
	if _, err := store.GetUpload("expired-page"); err != nil {
		t.Fatalf("upload should remain after storage failure: %v", err)
	}
}

func TestRetentionCleanupRemovesUploadAfterStorageDelete(t *testing.T) {
	s, store, storage, accountID := newRetentionCleanupTestServer(t)
	seedExpiredRetentionUpload(t, store, accountID, "expired-page")

	s.cleanupExpired(context.Background())

	if len(storage.deleted) != 1 || storage.deleted[0] != "expired-page" {
		t.Fatalf("storage deletes = %+v", storage.deleted)
	}
	if _, err := store.GetUpload("expired-page"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected upload to be deleted, err=%v", err)
	}
}

func newRetentionCleanupTestServer(t *testing.T) (*Server, *db.Store, *retentionCleanupStorage, int64) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetSetting("retention_days", "1"); err != nil {
		t.Fatal(err)
	}
	account, err := store.CreateAccount("admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	storage := &retentionCleanupStorage{}
	return &Server{store: store, storage: storage, secret: strings.Repeat("0", 64)}, store, storage, account.ID
}

func seedExpiredRetentionUpload(t *testing.T, store *db.Store, ownerID int64, slug string) {
	t.Helper()
	if err := store.CreateUpload(slug, ownerID, 0, slug+".html", 42, ""); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().Add(-48 * time.Hour).Unix()
	if _, err := store.Exec(`UPDATE uploads SET created_at=? WHERE slug=?`, createdAt, slug); err != nil {
		t.Fatal(err)
	}
}
