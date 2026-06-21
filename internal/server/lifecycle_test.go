package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/puemos/peek/internal/db"
)

type retentionCleanupStorage struct {
	mu      sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, slug)
	return s.err
}

func (s *retentionCleanupStorage) deletedSlugs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deleted...)
}

func TestRetentionCleanupKeepsUploadWhenStorageDeleteFails(t *testing.T) {
	s, store, storage, accountID := newRetentionCleanupTestServer(t)
	storage.err = errors.New("delete blocked")
	seedExpiredRetentionUpload(t, store, accountID, "expired-page")

	s.cleanupExpired(context.Background())

	deleted := storage.deletedSlugs()
	if len(deleted) != 1 || deleted[0] != "expired-page" {
		t.Fatalf("storage deletes = %+v", deleted)
	}
	if _, err := store.GetUpload("expired-page"); err != nil {
		t.Fatalf("upload should remain after storage failure: %v", err)
	}
}

func TestRetentionCleanupRemovesUploadAfterStorageDelete(t *testing.T) {
	s, store, storage, accountID := newRetentionCleanupTestServer(t)
	seedExpiredRetentionUpload(t, store, accountID, "expired-page")

	s.cleanupExpired(context.Background())

	deleted := storage.deletedSlugs()
	if len(deleted) != 1 || deleted[0] != "expired-page" {
		t.Fatalf("storage deletes = %+v", deleted)
	}
	if _, err := store.GetUpload("expired-page"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected upload to be deleted, err=%v", err)
	}
}

func TestRetentionCleanupWorkerHonorsRuntimeSettingChanges(t *testing.T) {
	s, store, storage, accountID := newRetentionCleanupTestServer(t)
	if err := store.SetSetting("retention_days", "0"); err != nil {
		t.Fatal(err)
	}
	seedExpiredRetentionUpload(t, store, accountID, "runtime-page")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runRetentionCleanup(ctx, 5*time.Millisecond)
	}()

	time.Sleep(30 * time.Millisecond)
	if deleted := storage.deletedSlugs(); len(deleted) != 0 {
		t.Fatalf("storage deletes while retention disabled = %+v", deleted)
	}
	if _, err := store.GetUpload("runtime-page"); err != nil {
		t.Fatalf("upload should remain while retention disabled: %v", err)
	}

	if err := store.SetSetting("retention_days", "1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if _, err := store.GetUpload("runtime-page"); errors.Is(err, sql.ErrNoRows) {
			break
		} else if err != nil {
			t.Fatalf("get upload: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload was not removed after retention was enabled; deletes=%+v", storage.deletedSlugs())
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("retention cleanup worker did not stop")
	}
}

func TestAuditRequestLogsPersistenceFailure(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.Exec(`DROP TABLE audit_log`); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}

	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	req := httptest.NewRequest(http.MethodPost, "/api/upload", nil)
	s.auditRequest(req, "Ada", "upload.create", "slug=page")

	if !strings.Contains(logs.String(), "audit log write failed") {
		t.Fatalf("audit persistence failure was not logged: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "action=upload.create") {
		t.Fatalf("audit action was not logged: %s", logs.String())
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
