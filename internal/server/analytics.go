package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/puemos/peek/internal/models"
)

type visitEvent struct {
	UploadID int64
	VID      string
	Name     string
	IPHash   string
	UA       string
	done     chan struct{}
}

// visitorID returns the stable visitor id from the hn_vid cookie, setting a
// fresh long-lived one if absent.
func (s *Server) visitorID(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(visitorCookie); err == nil && c.Value != "" {
		return c.Value
	}
	id, err := randID(18)
	if err != nil {
		return "anon"
	}
	s.setCookie(w, &http.Cookie{
		Name:     visitorCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   int((10 * 365 * 24 * time.Hour).Seconds()),
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
	return id
}

// recordVisit logs a page view, hashing the IP for privacy. vid is the visitor
// id already resolved (and cookie set) by the page handler.
func (s *Server) recordVisit(r *http.Request, u *models.Upload, vid string) {
	name := ""
	if c, err := r.Cookie(nameCookie); err == nil {
		name = strings.TrimSpace(c.Value)
	}
	ip := s.clientIP(r)
	h := sha256.Sum256([]byte(s.secret + "|" + ip))
	ipHash := hex.EncodeToString(h[:])[:16]
	if ip == "" {
		ipHash = ""
	}
	ua := r.Header.Get("User-Agent")
	if len(ua) > 300 {
		ua = ua[:300]
	}
	ev := visitEvent{UploadID: u.ID, VID: vid, Name: name, IPHash: ipHash, UA: ua}
	select {
	case s.visitQueue <- ev:
	default:
		slog.Warn("visit queue full; dropping analytics event", "upload_id", u.ID)
	}
}

func (s *Server) startVisitWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.visitQueue:
			if ev.done != nil {
				close(ev.done)
				continue
			}
			if err := s.store.RecordVisit(ev.UploadID, ev.VID, ev.Name, ev.IPHash, ev.UA); err != nil {
				slog.Warn("record visit failed", "upload_id", ev.UploadID, "err", err)
				continue
			}
			if ev.Name != "" {
				if err := s.store.UpsertVisitor(ev.VID, ev.Name); err != nil {
					slog.Warn("upsert visitor failed", "upload_id", ev.UploadID, "err", err)
				}
			}
		}
	}
}

// FlushVisits waits until all visit events already accepted by the queue have
// been processed by the analytics worker.
func (s *Server) FlushVisits(ctx context.Context) error {
	done := make(chan struct{})
	select {
	case s.visitQueue <- visitEvent{done: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) clientIP(r *http.Request) string {
	if s.trustedProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
	}
	addr := r.RemoteAddr
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		addr = addr[:i]
	}
	return strings.Trim(addr, "[]")
}
