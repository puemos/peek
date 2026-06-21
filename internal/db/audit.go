package db

import (
	"context"
	"time"
)

func (s *Store) AddAuditLog(ctx context.Context, actor, action, detail, ip string) error {
	_, err := s.ExecContext(ctx, `INSERT INTO audit_log(actor,action,detail,ip,created_at) VALUES(?,?,?,?,?)`,
		actor, action, detail, ip, time.Now().Unix())
	return err
}

type AuditEntry struct {
	ID        int64
	Actor     string
	Action    string
	Detail    string
	IP        string
	CreatedAt time.Time
}

func (s *Store) ListAuditLog(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.QueryContext(ctx, `SELECT id,actor,action,detail,ip,created_at FROM audit_log ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts int64
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Detail, &e.IP, &ts); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}
