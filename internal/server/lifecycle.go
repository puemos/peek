package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

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
	_ = s.store.AddAuditLog("", "system", msg, "")
}

// auditRequest logs an audit event with the actor's IP from the request.
func (s *Server) auditRequest(r *http.Request, actor, action, detail string) {
	ip := s.clientIP(r)
	slog.Info("audit", "actor", actor, "action", action, "detail", detail, "ip", ip)
	_ = s.store.AddAuditLog(actor, action, detail, ip)
}

func (s *Server) startRetentionCleanup(ctx context.Context) {
	retentionDays := s.settingInt("retention_days", 0)
	if retentionDays <= 0 {
		return
	}
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpired(ctx)
		}
	}
}

func (s *Server) cleanupExpired(ctx context.Context) {
	retentionDays := s.settingInt("retention_days", 0)
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	uploads, err := s.store.ListUploadsOlderThan(cutoff)
	if err != nil {
		slog.Error("retention cleanup: list", "err", err)
		return
	}
	for _, u := range uploads {
		if err := s.storage.Delete(ctx, u.Slug); err != nil {
			slog.Error("retention cleanup: storage delete", "slug", u.Slug, "err", err)
		}
		if err := s.store.DeleteUpload(u.ID); err != nil {
			slog.Error("retention cleanup: db delete", "slug", u.Slug, "err", err)
		}
	}
	if len(uploads) > 0 {
		slog.Info("retention cleanup: removed expired uploads", "count", len(uploads), "cutoff", cutoff.Format(time.DateOnly))
	}
}
