package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/puemos/peek/internal/db"
	webui "github.com/puemos/peek/internal/web"
)

type dashboardDeleteStorage struct {
	deleted []string
	err     error
}

func (s *dashboardDeleteStorage) Save(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (s *dashboardDeleteStorage) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (s *dashboardDeleteStorage) Delete(_ context.Context, slug string) error {
	s.deleted = append(s.deleted, slug)
	return s.err
}

func TestDashboardReportsUploadListFailure(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	account, err := store.CreateAccount("user@example.test", "User", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`DROP TABLE uploads`); err != nil {
		t.Fatal(err)
	}
	renderer, err := webui.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, renderer: renderer, secret: strings.Repeat("0", 64)}

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(account.ID, 10), sessionTTL)})
	rec := httptest.NewRecorder()

	s.handleDashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "uploads could not be loaded") {
		t.Fatalf("dashboard did not report list failure: %s", rec.Body.String())
	}
}

func TestDashboardDeleteStopsWhenDatabaseDeleteFails(t *testing.T) {
	s, store, storage, accountID := newDashboardDeleteTestServer(t)
	seedDashboardDeleteUpload(t, store, accountID)
	if _, err := store.Exec(`CREATE TRIGGER block_upload_delete BEFORE DELETE ON uploads BEGIN SELECT RAISE(ABORT, 'blocked delete'); END;`); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleDashboardDelete(rec, dashboardDeleteRequest(t, s, accountID, "page"))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if got := u.Query().Get("err"); got != "delete failed" {
		t.Fatalf("err query = %q, location = %q", got, loc)
	}
	if len(storage.deleted) != 0 {
		t.Fatalf("storage delete called after DB failure: %+v", storage.deleted)
	}
	if _, err := store.GetUpload("page"); err != nil {
		t.Fatalf("upload should remain after DB failure: %v", err)
	}
}

func TestDashboardDeleteRemovesUploadAndStorageObject(t *testing.T) {
	s, store, storage, accountID := newDashboardDeleteTestServer(t)
	seedDashboardDeleteUpload(t, store, accountID)

	rec := httptest.NewRecorder()
	s.handleDashboardDelete(rec, dashboardDeleteRequest(t, s, accountID, "page"))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Location") != "/dashboard" {
		t.Fatalf("location = %q", rec.Header().Get("Location"))
	}
	if len(storage.deleted) != 1 || storage.deleted[0] != "page" {
		t.Fatalf("storage deletes = %+v", storage.deleted)
	}
	if _, err := store.GetUpload("page"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected upload to be deleted, err=%v", err)
	}
}

func newDashboardDeleteTestServer(t *testing.T) (*Server, *db.Store, *dashboardDeleteStorage, int64) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	account, err := store.CreateAccount("admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	storage := &dashboardDeleteStorage{}
	return &Server{store: store, storage: storage, secret: strings.Repeat("0", 64)}, store, storage, account.ID
}

func seedDashboardDeleteUpload(t *testing.T, store *db.Store, ownerID int64) {
	t.Helper()
	if err := store.CreateUpload("page", ownerID, 0, "page.html", 42, ""); err != nil {
		t.Fatal(err)
	}
}

func dashboardDeleteRequest(t *testing.T, s *Server, accountID int64, slug string) *http.Request {
	t.Helper()
	form := url.Values{"csrf": {"csrf-token"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/delete/"+slug, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("slug", slug)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(accountID, 10), sessionTTL)})
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-token"})
	return req
}
