package server

import (
	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/uploads"
)

func (s *Server) uploadService() uploads.Service {
	return uploads.Service{Store: s.store, Storage: s.storage, BaseURL: s.baseURL}
}

func (s *Server) uploadLimits() db.UploadLimits {
	return db.UploadLimits{
		MaxTotalSize:       s.settingInt64("max_total_size", 0),
		MaxUploadsPerOwner: s.settingInt("max_uploads_per_token", 0),
		MaxStoragePerOwner: s.settingInt64("max_storage_per_token", 0),
	}
}
