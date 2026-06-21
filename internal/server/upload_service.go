package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/db"
)

type uploadService struct {
	store   *db.Store
	storage Storage
	baseURL string
}

type uploadCreateInput struct {
	OwnerAccountID int64
	OwnerTokenID   int64
	Filename       string
	Password       string
	Data           []byte
	Limits         db.UploadLimits
}

type uploadCreateResult struct {
	Slug     string
	URL      string
	Filename string
	Size     int
}

type uploadError struct {
	Status  int
	Message string
}

func (e *uploadError) Error() string {
	return e.Message
}

func newUploadError(status int, message string) error {
	return &uploadError{Status: status, Message: message}
}

func (s *Server) uploadService() uploadService {
	return uploadService{store: s.store, storage: s.storage, baseURL: s.baseURL}
}

func (s *Server) uploadLimits() db.UploadLimits {
	return db.UploadLimits{
		MaxTotalSize:       s.settingInt64("max_total_size", 0),
		MaxUploadsPerOwner: s.settingInt("max_uploads_per_token", 0),
		MaxStoragePerOwner: s.settingInt64("max_storage_per_token", 0),
	}
}

func (svc uploadService) Create(ctx context.Context, in uploadCreateInput) (*uploadCreateResult, error) {
	in.Filename = strings.TrimSpace(in.Filename)
	if in.Filename == "" {
		in.Filename = "page.html"
	}
	in.Password = strings.TrimSpace(in.Password)

	if len(in.Data) == 0 {
		return nil, newUploadError(http.StatusBadRequest, "empty file")
	}
	if !looksLikeHTML(in.Data) {
		return nil, newUploadError(http.StatusUnsupportedMediaType, "file does not look like HTML")
	}

	pwHash := ""
	if in.Password != "" {
		if !validatePasswordLength(in.Password) {
			return nil, newUploadError(http.StatusBadRequest, "password must be 72 characters or fewer")
		}
		h, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, newUploadError(http.StatusInternalServerError, "hash failed")
		}
		pwHash = string(h)
	}

	slug, err := generateSlug(svc.store)
	if err != nil {
		return nil, newUploadError(http.StatusInternalServerError, "slug generation failed")
	}
	if err := svc.storage.Save(ctx, slug, in.Data); err != nil {
		return nil, newUploadError(http.StatusInternalServerError, "storage failed")
	}
	if err := svc.store.CreateUploadChecked(slug, in.OwnerAccountID, in.OwnerTokenID, in.Filename, int64(len(in.Data)), pwHash, in.Limits); err != nil {
		_ = svc.storage.Delete(ctx, slug)
		return nil, uploadStoreError(err)
	}

	return &uploadCreateResult{
		Slug:     slug,
		URL:      svc.baseURL + "/p/" + slug,
		Filename: in.Filename,
		Size:     len(in.Data),
	}, nil
}

func uploadStoreError(err error) error {
	switch {
	case errors.Is(err, db.ErrTotalQuotaExceeded):
		return newUploadError(http.StatusRequestEntityTooLarge, "total storage quota exceeded")
	case errors.Is(err, db.ErrOwnerUploadCountQuotaExceeded):
		return newUploadError(http.StatusRequestEntityTooLarge, "per-token upload count quota exceeded")
	case errors.Is(err, db.ErrOwnerStorageQuotaExceeded):
		return newUploadError(http.StatusRequestEntityTooLarge, "per-token storage quota exceeded")
	default:
		return newUploadError(http.StatusInternalServerError, "db failed")
	}
}
