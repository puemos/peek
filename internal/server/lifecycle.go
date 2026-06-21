package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const retentionCleanupInterval = 1 * time.Hour

// Close releases server resources (database connections, etc.).
func (s *Server) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

func (s *Server) audit(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	slog.Info("audit", "action", "system", "detail", msg)
	s.persistAuditLog(s.ctx, "", "system", msg, "")
}

// auditRequest logs an audit event with the actor's IP from the request.
func (s *Server) auditRequest(r *http.Request, actor, action, detail string) {
	ip := s.clientIP(r)
	slog.Info("audit", "actor", actor, "action", action, "detail", detail, "ip", ip)
	s.persistAuditLog(r.Context(), actor, action, detail, ip)
}

func (s *Server) persistAuditLog(ctx context.Context, actor, action, detail, ip string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.store.AddAuditLog(ctx, actor, action, detail, ip); err != nil {
		slog.Error("audit log write failed", "actor", actor, "action", action, "detail", detail, "ip", ip, "err", err)
	}
}

func (s *Server) startRetentionCleanup(ctx context.Context) {
	s.runRetentionCleanup(ctx, retentionCleanupInterval)
}

func (s *Server) runRetentionCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = retentionCleanupInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.runRetentionCleanupLoop(ctx, ticker.C, nil)
}

func (s *Server) runRetentionCleanupLoop(ctx context.Context, ticks <-chan time.Time, afterCleanup func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			s.cleanupExpired(ctx)
			if afterCleanup != nil {
				afterCleanup()
			}
		}
	}
}

func (s *Server) cleanupExpired(ctx context.Context) {
	retentionDays := s.settingInt(ctx, "retention_days", 0)
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	uploads, err := s.store.ListUploadsOlderThan(ctx, cutoff)
	if err != nil {
		slog.Error("retention cleanup: list", "err", err)
		return
	}
	removed := 0
	for _, u := range uploads {
		if err := s.deleteUpload(ctx, u); err != nil {
			slog.Error("retention cleanup: delete upload", "slug", u.Slug, "err", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("retention cleanup: removed expired uploads", "count", removed, "cutoff", cutoff.Format(time.DateOnly))
	}
}
