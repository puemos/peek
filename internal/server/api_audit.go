package server

import (
	"net/http"
	"strconv"
)

func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := s.store.ListAuditLog(r.Context(), limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	type arow struct {
		ID        int64  `json:"id"`
		Actor     string `json:"actor"`
		Action    string `json:"action"`
		Detail    string `json:"detail"`
		IP        string `json:"ip"`
		CreatedAt int64  `json:"created_at"`
	}
	out := make([]arow, 0, len(entries))
	for _, e := range entries {
		out = append(out, arow{
			ID: e.ID, Actor: e.Actor, Action: e.Action,
			Detail: e.Detail, IP: e.IP, CreatedAt: e.CreatedAt.Unix(),
		})
	}
	jsonOK(w, out)
}
