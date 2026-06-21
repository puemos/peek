package uploadquota

import "errors"

var (
	ErrTotalExceeded        = errors.New("total storage quota exceeded")
	ErrOwnerCountExceeded   = errors.New("owner upload count quota exceeded")
	ErrOwnerStorageExceeded = errors.New("owner storage quota exceeded")
)

type Limits struct {
	MaxTotalSize       int64
	MaxUploadsPerOwner int
	MaxStoragePerOwner int64
}
