package uploads

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/objectstore"
)

type Service struct {
	Store   *db.Store
	Storage objectstore.Storage
	BaseURL string
}

type CreateInput struct {
	OwnerAccountID int64
	OwnerTokenID   int64
	Filename       string
	Password       string
	Data           []byte
	Limits         db.UploadLimits
}

type CreateResult struct {
	Slug     string
	URL      string
	Filename string
	Size     int
}

type ErrorKind string

const (
	KindEmptyFile            ErrorKind = "empty_file"
	KindInvalidHTML          ErrorKind = "invalid_html"
	KindPasswordTooLong      ErrorKind = "password_too_long"
	KindPasswordHash         ErrorKind = "password_hash"
	KindSlugGeneration       ErrorKind = "slug_generation"
	KindStorageWrite         ErrorKind = "storage_write"
	KindTotalQuotaExceeded   ErrorKind = "total_quota_exceeded"
	KindOwnerCountExceeded   ErrorKind = "owner_count_exceeded"
	KindOwnerStorageExceeded ErrorKind = "owner_storage_exceeded"
	KindPersistenceFailure   ErrorKind = "persistence_failure"
)

type Error struct {
	Kind    ErrorKind
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func newError(kind ErrorKind, message string) error {
	return &Error{Kind: kind, Message: message}
}

func (svc Service) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	in.Filename = strings.TrimSpace(in.Filename)
	if in.Filename == "" {
		in.Filename = "page.html"
	}
	in.Password = strings.TrimSpace(in.Password)

	if len(in.Data) == 0 {
		return nil, newError(KindEmptyFile, "empty file")
	}
	if !looksLikeHTML(in.Data) {
		return nil, newError(KindInvalidHTML, "file does not look like HTML")
	}

	pwHash := ""
	if in.Password != "" {
		if !ValidatePasswordLength(in.Password) {
			return nil, newError(KindPasswordTooLong, "password must be 72 characters or fewer")
		}
		h, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, newError(KindPasswordHash, "hash failed")
		}
		pwHash = string(h)
	}

	slug, err := generateSlug(svc.Store)
	if err != nil {
		return nil, newError(KindSlugGeneration, "slug generation failed")
	}
	if err := svc.Storage.Save(ctx, slug, in.Data); err != nil {
		return nil, newError(KindStorageWrite, "storage failed")
	}
	if err := svc.Store.CreateUploadChecked(slug, in.OwnerAccountID, in.OwnerTokenID, in.Filename, int64(len(in.Data)), pwHash, in.Limits); err != nil {
		_ = svc.Storage.Delete(ctx, slug)
		return nil, storeError(err)
	}

	return &CreateResult{
		Slug:     slug,
		URL:      svc.BaseURL + "/p/" + slug,
		Filename: in.Filename,
		Size:     len(in.Data),
	}, nil
}

func storeError(err error) error {
	switch {
	case errors.Is(err, db.ErrTotalQuotaExceeded):
		return newError(KindTotalQuotaExceeded, "total storage quota exceeded")
	case errors.Is(err, db.ErrOwnerUploadCountQuotaExceeded):
		return newError(KindOwnerCountExceeded, "per-token upload count quota exceeded")
	case errors.Is(err, db.ErrOwnerStorageQuotaExceeded):
		return newError(KindOwnerStorageExceeded, "per-token storage quota exceeded")
	default:
		return newError(KindPersistenceFailure, "db failed")
	}
}
