package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/models"
)

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAPIToken(w, r)
	if !ok {
		return
	}
	var body struct {
		Name     string `json:"name"`
		ExpiresH int    `json:"expires_hours"`
	}
	if err := decodeJSON(w, r, &body, smallJSONBodyLimit); err != nil || strings.TrimSpace(body.Name) == "" {
		jsonError(w, http.StatusBadRequest, "name required")
		return
	}
	t, err := randID(24)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	var expiresAt int64
	if body.ExpiresH > 0 {
		expiresAt = time.Now().Add(time.Duration(body.ExpiresH) * time.Hour).Unix()
	}
	if err := s.store.CreateToken(t, strings.TrimSpace(body.Name), false, expiresAt); err != nil {
		jsonError(w, http.StatusInternalServerError, "db failed")
		return
	}
	s.auditRequest(r, actorName(actor), "token.create", "name="+body.Name)
	jsonOK(w, map[string]any{"token": t, "name": body.Name})
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.store.ListTokens()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	// Tokens are stored hashed and never returned after creation.
	type trow struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Admin     bool   `json:"admin"`
		ExpiresAt int64  `json:"expires_at"`
	}
	out := make([]trow, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, trow{ID: t.ID, Name: t.Name, Admin: t.IsAdmin, ExpiresAt: t.ExpiresAt})
	}
	jsonOK(w, out)
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAPIToken(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad token id")
		return
	}
	t, err := s.store.DeleteTokenChecked(id)
	if errors.Is(err, sql.ErrNoRows) {
		jsonError(w, http.StatusNotFound, "token not found")
		return
	}
	if errors.Is(err, db.ErrLastAdmin) {
		jsonError(w, http.StatusBadRequest, "cannot revoke the last admin token")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.auditRequest(r, actorName(actor), "token.revoke", "id="+strconv.FormatInt(id, 10)+" name="+t.Name)
	jsonOK(w, map[string]any{"revoked": id})
}

func actorName(t *models.Token) string {
	if t != nil {
		return t.Name
	}
	return "unknown"
}
