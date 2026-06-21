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

func (fs *FileStorage) objectPath(slug string) (string, error) {
	name, err := objectName(slug)
	if err != nil {
		return "", err
	}
	return filepath.Join(fs.Dir, name), nil
}

func (fs *FileStorage) Save(_ context.Context, slug string, data []byte) error {
	path, err := fs.objectPath(slug)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(fs.Dir, 0o755); err != nil {
		return err
	}
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
	path, err := fs.objectPath(slug)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (fs *FileStorage) Delete(_ context.Context, slug string) error {
	path, err := fs.objectPath(slug)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
