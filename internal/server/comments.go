package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/puemos/peek/internal/models"
)

type commentIn struct {
	Selector string `json:"selector"`
	Text     string `json:"element_text"`
	Name     string `json:"name"`
	Body     string `json:"body"`
}

type commentOut struct {
	ID        int64  `json:"id"`
	Selector  string `json:"selector"`
	Text      string `json:"element_text"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	// API callers (the CLI) authenticate with a bearer token and may only read
	// comments for uploads they own. Browser viewers send no token and go
	// through the public / password-cookie gate instead.
	if tok := bearerToken(r); tok != "" {
		owner, err := s.store.GetToken(tok)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if owner.Disabled {
			jsonError(w, http.StatusForbidden, "account disabled")
			return
		}
		if u.OwnerAccountID != owner.AccountID && !owner.IsAdmin {
			jsonError(w, http.StatusForbidden, "not owner")
			return
		}
	} else if !s.pageAuthorized(r, u) {
		jsonError(w, http.StatusUnauthorized, "password required")
		return
	}
	list, err := s.store.ListComments(u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	out := make([]commentOut, 0, len(list))
	for _, c := range list {
		out = append(out, commentOut{
			ID: c.ID, Selector: c.ElementSelector, Text: c.ElementText,
			Author: c.AuthorName, Body: c.Body, CreatedAt: c.CreatedAt.Unix(),
		})
	}
	jsonOK(w, out)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	if !s.commentLimiter.allow(s.clientIP(r)) {
		jsonError(w, http.StatusTooManyRequests, "too many comments, slow down")
		return
	}
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.pageAuthorized(r, u) {
		jsonError(w, http.StatusUnauthorized, "password required")
		return
	}

	var in commentIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		jsonError(w, http.StatusBadRequest, "bad json")
		return
	}
	in.Selector = strings.TrimSpace(in.Selector)
	in.Body = strings.TrimSpace(in.Body)
	in.Name = strings.TrimSpace(in.Name)
	in.Text = strings.TrimSpace(in.Text)
	if in.Body == "" {
		jsonError(w, http.StatusBadRequest, "comment body required")
		return
	}
	if len(in.Body) > 4000 || len(in.Name) > 100 || len(in.Selector) > 500 || len(in.Text) > 200 {
		jsonError(w, http.StatusRequestEntityTooLarge, "field too long")
		return
	}
	if in.Name == "" {
		in.Name = "anonymous"
	}

	vid := s.visitorID(w, r)
	if in.Name != "anonymous" {
		if err := s.store.UpsertVisitor(vid, in.Name); err != nil {
			slog.Warn("comment visitor upsert failed", "upload_id", u.ID, "err", err)
		}
		s.setNameCookie(w, in.Name)
	}

	h := sha256.Sum256([]byte(s.secret + "|" + vid))
	vidHash := hex.EncodeToString(h[:])[:16]

	if err := s.store.AddComment(u.ID, in.Selector, in.Text, in.Name, vidHash, in.Body); err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}

	list, err := s.store.ListComments(u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	out := make([]commentOut, 0, len(list))
	for _, c := range list {
		out = append(out, commentOut{
			ID: c.ID, Selector: c.ElementSelector, Text: c.ElementText,
			Author: c.AuthorName, Body: c.Body, CreatedAt: c.CreatedAt.Unix(),
		})
	}
	jsonOK(w, out)
}

// pageAuthorized returns true if the viewer may see the page. For unprotected
// uploads it always succeeds; for protected ones it requires a valid signed
// session cookie for that slug.
func (s *Server) pageAuthorized(r *http.Request, u *models.Upload) bool {
	if u.PasswordHash == "" {
		return true
	}
	c, err := r.Cookie(authCookieName(u.Slug))
	if err != nil {
		return false
	}
	return verifySessionCookie(s.secret, c.Value, u.Slug)
}

func authCookieName(slug string) string {
	return "hn_auth_" + slug
}

// setNameCookie sets a long-lived, JS-readable cookie so the name is remembered.
func (s *Server) setNameCookie(w http.ResponseWriter, name string) {
	s.setCookie(w, &http.Cookie{
		Name:     nameCookie,
		Value:    name,
		Path:     "/",
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
		SameSite: http.SameSiteLaxMode,
		HttpOnly: false,
	})
}
