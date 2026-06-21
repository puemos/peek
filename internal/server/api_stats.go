package server

import "net/http"

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	tok := bearerToken(r)
	owner, _ := s.store.GetToken(tok)
	if u.OwnerAccountID != owner.AccountID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	total, unique, err := s.store.CountVisits(u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	recent, err := s.store.RecentVisits(u.ID, 50)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	type vrow struct {
		Name      string `json:"name"`
		IP        string `json:"ip"`
		UA        string `json:"user_agent"`
		Timestamp int64  `json:"visited_at"`
	}
	rows := make([]vrow, 0, len(recent))
	for _, v := range recent {
		rows = append(rows, vrow{Name: v.VisitorName, IP: v.IP, UA: v.UserAgent, Timestamp: v.VisitedAt.Unix()})
	}
	jsonOK(w, map[string]any{
		"slug":            slug,
		"filename":        u.Filename,
		"total_visits":    total,
		"unique_visitors": unique,
		"recent":          rows,
	})
}

func (s *Server) handleExportUpload(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	tok := bearerToken(r)
	owner, _ := s.store.GetToken(tok)
	if u.OwnerAccountID != owner.AccountID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	total, unique, _ := s.store.CountVisits(u.ID)
	recent, _ := s.store.RecentVisits(u.ID, 500)
	comments, _ := s.store.ListComments(u.ID)

	type exportComment struct {
		Author    string `json:"author"`
		Body      string `json:"body"`
		Selector  string `json:"selector"`
		Text      string `json:"element_text"`
		CreatedAt int64  `json:"created_at"`
	}
	type exportVisit struct {
		Name      string `json:"name"`
		IP        string `json:"ip"`
		UA        string `json:"user_agent"`
		Timestamp int64  `json:"visited_at"`
	}
	export := map[string]any{
		"slug":            slug,
		"filename":        u.Filename,
		"size":            u.Size,
		"protected":       u.PasswordHash != "",
		"created_at":      u.CreatedAt.Unix(),
		"total_visits":    total,
		"unique_visitors": unique,
	}
	cmts := make([]exportComment, 0, len(comments))
	for _, c := range comments {
		cmts = append(cmts, exportComment{
			Author: c.AuthorName, Body: c.Body, Selector: c.ElementSelector,
			Text: c.ElementText, CreatedAt: c.CreatedAt.Unix(),
		})
	}
	export["comments"] = cmts
	visits := make([]exportVisit, 0, len(recent))
	for _, v := range recent {
		visits = append(visits, exportVisit{
			Name: v.VisitorName, IP: v.IP, UA: v.UserAgent, Timestamp: v.VisitedAt.Unix(),
		})
	}
	export["visits"] = visits

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+slug+`-export.json"`)
	jsonOK(w, export)
}
