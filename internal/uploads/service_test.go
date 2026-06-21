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
)

type memoryStorage struct {
	saved   map[string][]byte
	deleted []string
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
		Filename:       "page.html",
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

func TestServiceDeletesStorageObjectWhenDBRejectsUpload(t *testing.T) {
	store, owner := serviceTestStore(t)
	defer store.Close()
	st := newMemoryStorage()
	svc := Service{Repository: store, Storage: st, BaseURL: "http://localhost:7700"}

	_, err := svc.Create(context.Background(), CreateInput{
		OwnerAccountID: owner.AccountID,
		OwnerTokenID:   owner.ID,
		Filename:       "page.html",
		Data:           []byte("<!doctype html><html></html>"),
		Limits:         db.UploadLimits{MaxTotalSize: 1},
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

func serviceTestStore(t *testing.T) (*db.Store, *models.Token) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateToken("owner", "owner", false, 0); err != nil {
		t.Fatal(err)
	}
	owner, err := store.GetToken("owner")
	if err != nil {
		t.Fatal(err)
	}
	return store, owner
}
