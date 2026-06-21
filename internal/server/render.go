package server

import (
	"log/slog"
	"net/http"
)

func (s *Server) renderHTML(w http.ResponseWriter, status int, templateName string, data any) {
	body, err := s.renderer.Execute(templateName, data)
	if err != nil {
		slog.Error("render html", "template", templateName, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Error("write html response", "template", templateName, "err", err)
	}
}
