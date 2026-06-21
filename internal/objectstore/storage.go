package objectstore

import (
	"context"
	"io"
)

type Storage interface {
	Save(ctx context.Context, slug string, data []byte) error
	Open(ctx context.Context, slug string) (io.ReadCloser, error)
	Delete(ctx context.Context, slug string) error
}
