package server

import (
	"errors"
	"net/http"

	"github.com/puemos/peek/internal/uploadquota"
	"github.com/puemos/peek/internal/uploads"
)

func (s *Server) uploadService() uploads.Service {
	return uploads.Service{Repository: s.store, Storage: s.storage, BaseURL: s.baseURL}
}

func (s *Server) uploadLimits() uploadquota.Limits {
	return uploadquota.Limits{
		MaxTotalSize:       s.settingInt64("max_total_size", 0),
		MaxUploadsPerOwner: s.settingInt("max_uploads_per_token", 0),
		MaxStoragePerOwner: s.settingInt64("max_storage_per_token", 0),
	}
}

func uploadHTTPError(err error) (int, string) {
	var uploadErr *uploads.Error
	if !errors.As(err, &uploadErr) {
		return http.StatusInternalServerError, "upload failed"
	}
	switch uploadErr.Kind {
	case uploads.KindEmptyFile, uploads.KindPasswordTooLong:
		return http.StatusBadRequest, uploadErr.Message
	case uploads.KindInvalidHTML:
		return http.StatusUnsupportedMediaType, uploadErr.Message
	case uploads.KindTotalQuotaExceeded, uploads.KindOwnerCountExceeded, uploads.KindOwnerStorageExceeded:
		return http.StatusRequestEntityTooLarge, uploadErr.Message
	default:
		return http.StatusInternalServerError, uploadErr.Message
	}
}

func uploadErrorMessage(err error) string {
	_, msg := uploadHTTPError(err)
	return msg
}
