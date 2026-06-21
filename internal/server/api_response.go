package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

func jsonOK(w http.ResponseWriter, v any) {
	writeJSON(w, http.StatusOK, v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write json response failed", "err", err)
	}
}
