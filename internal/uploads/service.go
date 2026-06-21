package uploads

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/objectstore"
	"github.com/puemos/peek/internal/uploadquota"
)

type Service struct {
	Repository Repository
	Storage    objectstore.Storage
	BaseURL    string
}

type Repository interface {
	UploadSlugExists(slug string) (bool, error)
	CreateUploadChecked(slug string, ownerAccountID, ownerTokenID int64, filename string, size int64, passwordHash string, limits uploadquota.Limits) error
}

type CreateInput struct {
	OwnerAccountID int64
	OwnerTokenID   int64
	Filename       string
	Password       string
	Data           []byte
	Limits         uploadquota.Limits
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

type CleanupError struct {
	Slug string
	Err  error
}

func (e *CleanupError) Error() string {
	return "cleanup storage object " + e.Slug + ": " + e.Err.Error()
}

func (e *CleanupError) Unwrap() error {
	return e.Err
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

	slug, err := generateSlug(svc.Repository)
	if err != nil {
		return nil, newError(KindSlugGeneration, "slug generation failed")
	}
	if err := svc.Storage.Save(ctx, slug, in.Data); err != nil {
		return nil, newError(KindStorageWrite, "storage failed")
	}
	if err := svc.Repository.CreateUploadChecked(slug, in.OwnerAccountID, in.OwnerTokenID, in.Filename, int64(len(in.Data)), pwHash, in.Limits); err != nil {
		uploadErr := storeError(err)
		if cleanupErr := svc.Storage.Delete(ctx, slug); cleanupErr != nil {
			return nil, errors.Join(uploadErr, &CleanupError{Slug: slug, Err: cleanupErr})
		}
		return nil, uploadErr
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
	case errors.Is(err, uploadquota.ErrTotalExceeded):
		return newError(KindTotalQuotaExceeded, "total storage quota exceeded")
	case errors.Is(err, uploadquota.ErrOwnerCountExceeded):
		return newError(KindOwnerCountExceeded, "per-token upload count quota exceeded")
	case errors.Is(err, uploadquota.ErrOwnerStorageExceeded):
		return newError(KindOwnerStorageExceeded, "per-token storage quota exceeded")
	default:
		return newError(KindPersistenceFailure, "db failed")
	}
}
