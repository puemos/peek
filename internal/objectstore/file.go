package objectstore

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

type FileStorage struct {
	Dir string
}

func (fs *FileStorage) objectPath(slug string) string {
	return filepath.Join(fs.Dir, slug+".html")
}

func (fs *FileStorage) Save(_ context.Context, slug string, data []byte) error {
	if err := os.MkdirAll(fs.Dir, 0o755); err != nil {
		return err
	}
	path := fs.objectPath(slug)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (fs *FileStorage) Open(_ context.Context, slug string) (io.ReadCloser, error) {
	return os.Open(fs.objectPath(slug))
}

func (fs *FileStorage) Delete(_ context.Context, slug string) error {
	err := os.Remove(fs.objectPath(slug))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
