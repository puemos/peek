package server

import (
	"context"
	"fmt"

	"github.com/puemos/peek/internal/models"
)

func (s *Server) deleteUpload(ctx context.Context, u models.Upload) error {
	if err := s.storage.Delete(ctx, u.Slug); err != nil {
		return fmt.Errorf("delete upload object %q: %w", u.Slug, err)
	}
	if err := s.store.DeleteUpload(u.ID); err != nil {
		return fmt.Errorf("delete upload record %q: %w", u.Slug, err)
	}
	return nil
}
