package db

import "errors"

var (
	ErrTotalQuotaExceeded            = errors.New("total storage quota exceeded")
	ErrOwnerUploadCountQuotaExceeded = errors.New("owner upload count quota exceeded")
	ErrOwnerStorageQuotaExceeded     = errors.New("owner storage quota exceeded")
	ErrLastAdmin                     = errors.New("cannot remove or disable the last active admin")
)

type UploadLimits struct {
	MaxTotalSize       int64
	MaxUploadsPerOwner int
	MaxStoragePerOwner int64
}
