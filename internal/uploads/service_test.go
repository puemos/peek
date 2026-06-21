package uploads

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/models"
	"github.com/puemos/peek/internal/uploadquota"
)

type memoryStorage struct {
	saved     map[string][]byte
	deleted   []string
	deleteErr error
}

func newMemoryStorage() *memoryStorage {
	return &memoryStorage{saved: map[string][]byte{}}
}

func (m *memoryStorage) Save(_ context.Context, slug string, data []byte) error {
	m.saved[slug] = append([]byte(nil), data...)
	return nil
}

func (m *memoryStorage) Open(_ context.Context, slug string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.saved[slug])), nil
}

func (m *memoryStorage) Delete(_ context.Context, slug string) error {
	m.deleted = append(m.deleted, slug)
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.saved, slug)
	return nil
}

func TestServiceRejectsInvalidPasswordBeforeStorageWrite(t *testing.T) {
	store, owner := serviceTestStore(t)
	defer store.Close()
	st := newMemoryStorage()
	svc := Service{Repository: store, Storage: st, BaseURL: "http://localhost:7700"}

	_, err := svc.Create(context.Background(), CreateInput{
		OwnerAccountID: owner.AccountID,
		OwnerTokenID:   owner.ID,
		Name:           "page.html",
		Password:       makeString('a', 73),
		Data:           []byte("<!doctype html><html></html>"),
	})
	if err == nil {
		t.Fatal("expected invalid password error")
	}
	var uploadErr *Error
	if !errors.As(err, &uploadErr) {
		t.Fatalf("error = %T %[1]v, want *uploads.Error", err)
	}
	if uploadErr.Kind != KindPasswordTooLong {
		t.Fatalf("kind = %q, want %q", uploadErr.Kind, KindPasswordTooLong)
	}
	if len(st.saved) != 0 {
		t.Fatalf("invalid password wrote storage object: %+v", st.saved)
	}
}

func TestServiceDefaultsToPasswordVisibility(t *testing.T) {
	store, owner := serviceTestStore(t)
	defer store.Close()
	st := newMemoryStorage()
	svc := Service{Repository: store, Storage: st, BaseURL: "http://localhost:7700"}

	_, err := svc.Create(context.Background(), CreateInput{
		OwnerAccountID: owner.AccountID,
		OwnerTokenID:   owner.ID,
		Name:           "page.html",
		Data:           []byte("<!doctype html><html></html>"),
	})
	if err == nil {
		t.Fatal("expected missing password error")
	}
	var uploadErr *Error
	if !errors.As(err, &uploadErr) || uploadErr.Kind != KindPasswordRequired {
		t.Fatalf("error = %T %[1]v, want %q", err, KindPasswordRequired)
	}
	if len(st.saved) != 0 {
		t.Fatalf("missing password wrote storage object: %+v", st.saved)
	}
}

func TestServiceStoresPasswordHashOnlyForPasswordVisibility(t *testing.T) {
	store, owner := serviceTestStore(t)
	defer store.Close()
	st := newMemoryStorage()
	svc := Service{Repository: store, Storage: st, BaseURL: "http://localhost:7700"}

	up, err := svc.Create(context.Background(), CreateInput{
		OwnerAccountID: owner.AccountID,
		OwnerTokenID:   owner.ID,
		Name:           "page.html",
		Password:       "secret",
		Data:           []byte("<!doctype html><html></html>"),
	})
	if err != nil {
		t.Fatalf("create password upload: %v", err)
	}
	if up.Visibility != "password" {
		t.Fatalf("result visibility = %q", up.Visibility)
	}
	got, err := store.GetUpload(context.Background(), up.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Visibility != "password" || got.PasswordHash == "" {
		t.Fatalf("password upload not stored correctly: %+v", got)
	}

	_, err = svc.Create(context.Background(), CreateInput{
		OwnerAccountID: owner.AccountID,
		OwnerTokenID:   owner.ID,
		Name:           "public.html",
		Visibility:     "public",
		Password:       "secret",
		Data:           []byte("<!doctype html><html></html>"),
	})
	if err == nil {
		t.Fatal("expected public upload with password to fail")
	}
	var uploadErr *Error
	if !errors.As(err, &uploadErr) || uploadErr.Kind != KindPasswordNotAllowed {
		t.Fatalf("error = %T %[1]v, want %q", err, KindPasswordNotAllowed)
	}
}

func TestServiceDeletesStorageObjectWhenDBRejectsUpload(t *testing.T) {
	store, owner := serviceTestStore(t)
	defer store.Close()
	st := newMemoryStorage()
	svc := Service{Repository: store, Storage: st, BaseURL: "http://localhost:7700"}

	_, err := svc.Create(context.Background(), CreateInput{
		OwnerAccountID: owner.AccountID,
		OwnerTokenID:   owner.ID,
		Name:           "page.html",
		Visibility:     "public",
		Data:           []byte("<!doctype html><html></html>"),
		Limits:         uploadquota.Limits{MaxTotalSize: 1},
	})
	if err == nil {
		t.Fatal("expected quota error")
	}
	var uploadErr *Error
	if !errors.As(err, &uploadErr) {
		t.Fatalf("error = %T %[1]v, want *uploads.Error", err)
	}
	if uploadErr.Kind != KindTotalQuotaExceeded {
		t.Fatalf("kind = %q, want %q", uploadErr.Kind, KindTotalQuotaExceeded)
	}
	if len(st.deleted) != 1 {
		t.Fatalf("expected one cleanup delete, got %+v", st.deleted)
	}
	if len(st.saved) != 0 {
		t.Fatalf("quota failure left saved object: %+v", st.saved)
	}
}

func TestServiceReturnsCleanupErrorWhenStorageDeleteFails(t *testing.T) {
	store, owner := serviceTestStore(t)
	defer store.Close()
	deleteErr := errors.New("delete failed")
	st := newMemoryStorage()
	st.deleteErr = deleteErr
	svc := Service{Repository: store, Storage: st, BaseURL: "http://localhost:7700"}

	_, err := svc.Create(context.Background(), CreateInput{
		OwnerAccountID: owner.AccountID,
		OwnerTokenID:   owner.ID,
		Name:           "page.html",
		Visibility:     "public",
		Data:           []byte("<!doctype html><html></html>"),
		Limits:         uploadquota.Limits{MaxTotalSize: 1},
	})
	if err == nil {
		t.Fatal("expected quota error")
	}
	var uploadErr *Error
	if !errors.As(err, &uploadErr) {
		t.Fatalf("error = %T %[1]v, want *uploads.Error", err)
	}
	if uploadErr.Kind != KindTotalQuotaExceeded {
		t.Fatalf("kind = %q, want %q", uploadErr.Kind, KindTotalQuotaExceeded)
	}
	var cleanupErr *CleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("error = %T %[1]v, want *uploads.CleanupError", err)
	}
	if !errors.Is(err, deleteErr) {
		t.Fatalf("error does not wrap delete failure: %v", err)
	}
	if len(st.deleted) != 1 || st.deleted[0] != cleanupErr.Slug {
		t.Fatalf("cleanup deletes = %+v, cleanup slug = %q", st.deleted, cleanupErr.Slug)
	}
	if _, ok := st.saved[cleanupErr.Slug]; !ok {
		t.Fatalf("storage object should remain when cleanup delete fails: saved=%+v cleanup=%+v", st.saved, cleanupErr)
	}
}

func serviceTestStore(t *testing.T) (*db.Store, *models.Token) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateToken(context.Background(), "owner", "owner", false, 0); err != nil {
		t.Fatal(err)
	}
	owner, err := store.GetToken(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	return store, owner
}
