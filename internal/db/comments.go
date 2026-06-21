package db

import (
	"context"
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Store) AddComment(ctx context.Context, uploadID int64, selector, text, anchorKind, author, cookie, body string) error {
	_, err := s.ExecContext(ctx, `INSERT INTO comments(upload_id,element_selector,element_text,anchor_kind,author_name,author_cookie,body,created_at)
		VALUES(?,?,?,?,?,?,?,?)`, uploadID, selector, text, anchorKind, author, cookie, body, time.Now().Unix())
	return err
}

func (s *Store) ListComments(ctx context.Context, uploadID int64) ([]models.Comment, error) {
	rows, err := s.QueryContext(ctx, `SELECT id,upload_id,element_selector,element_text,anchor_kind,author_name,author_cookie,body,created_at
		FROM comments WHERE upload_id=? ORDER BY created_at ASC`, uploadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Comment
	for rows.Next() {
		var c models.Comment
		var ts int64
		if err := rows.Scan(&c.ID, &c.UploadID, &c.ElementSelector, &c.ElementText, &c.AnchorKind, &c.AuthorName, &c.AuthorCookie, &c.Body, &ts); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(ts, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}
