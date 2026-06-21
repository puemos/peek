package db

import (
	"context"
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Store) RecordVisit(ctx context.Context, uploadID int64, cookie, name, ip, ua string) error {
	_, err := s.ExecContext(ctx, `INSERT INTO visits(upload_id,visitor_cookie,visitor_name,ip,user_agent,visited_at)
		VALUES(?,?,?,?,?,?)`, uploadID, cookie, name, ip, ua, time.Now().Unix())
	return err
}

func (s *Store) CountVisits(ctx context.Context, uploadID int64) (total, unique int, err error) {
	err = s.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(DISTINCT visitor_cookie) FROM visits WHERE upload_id=?`, uploadID).Scan(&total, &unique)
	return
}

func (s *Store) VisitBuckets(ctx context.Context, uploadID int64, since time.Time, intervalSec int64) ([]models.VisitBucket, error) {
	rows, err := s.QueryContext(ctx, `
		SELECT (visited_at / ?) * ? AS bucket, COUNT(*) AS n
		FROM visits
		WHERE upload_id = ? AND visited_at >= ?
		GROUP BY bucket
		ORDER BY bucket`, intervalSec, intervalSec, uploadID, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.VisitBucket
	for rows.Next() {
		var b models.VisitBucket
		var ts int64
		if err := rows.Scan(&ts, &b.Count); err != nil {
			return nil, err
		}
		b.Time = time.Unix(ts, 0)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) RecentVisits(ctx context.Context, uploadID int64, limit int) ([]models.Visit, error) {
	rows, err := s.QueryContext(ctx, `SELECT id,upload_id,visitor_cookie,visitor_name,ip,user_agent,visited_at
		FROM visits WHERE upload_id=? ORDER BY visited_at DESC LIMIT ?`, uploadID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Visit
	for rows.Next() {
		var v models.Visit
		var ts int64
		if err := rows.Scan(&v.ID, &v.UploadID, &v.VisitorCookie, &v.VisitorName, &v.IP, &v.UserAgent, &ts); err != nil {
			return nil, err
		}
		v.VisitedAt = time.Unix(ts, 0)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertVisitor(ctx context.Context, cookie, name string) error {
	now := time.Now().Unix()
	_, err := s.ExecContext(ctx, `INSERT INTO visitors(cookie,name,created_at,last_seen) VALUES(?,?,?,?)
		ON CONFLICT(cookie) DO UPDATE SET last_seen=excluded.last_seen, name=CASE WHEN excluded.name='' THEN visitors.name ELSE excluded.name END`,
		cookie, name, now, now)
	return err
}

func (s *Store) GetVisitor(ctx context.Context, cookie string) (*models.Visitor, error) {
	v := &models.Visitor{}
	var created, last int64
	err := s.QueryRowContext(ctx, `SELECT cookie,name,created_at,last_seen FROM visitors WHERE cookie=?`, cookie).
		Scan(&v.Cookie, &v.Name, &created, &last)
	if err != nil {
		return nil, err
	}
	v.CreatedAt = time.Unix(created, 0)
	v.LastSeen = time.Unix(last, 0)
	return v, nil
}
