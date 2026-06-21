package server

import (
	"net/http"
	"time"

	"github.com/puemos/peek/internal/models"
)

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireAPIToken(w, r)
	if !ok {
		return
	}
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(r.Context(), slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	if u.OwnerAccountID != owner.AccountID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	total, unique, err := s.store.CountVisits(r.Context(), u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	recent, err := s.store.RecentVisits(r.Context(), u.ID, 50)
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
		"name":            u.Name,
		"total_visits":    total,
		"unique_visitors": unique,
		"recent":          rows,
	})
}

func (s *Server) handleExportUpload(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireAPIToken(w, r)
	if !ok {
		return
	}
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(r.Context(), slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	if u.OwnerAccountID != owner.AccountID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	total, unique, err := s.store.CountVisits(r.Context(), u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	recent, err := s.store.RecentVisits(r.Context(), u.ID, 500)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	comments, err := s.store.ListComments(r.Context(), u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}

	type exportComment struct {
		Author     string `json:"author"`
		Body       string `json:"body"`
		Selector   string `json:"selector"`
		Text       string `json:"element_text"`
		AnchorKind string `json:"anchor_kind"`
		CreatedAt  int64  `json:"created_at"`
	}
	type exportVisit struct {
		Name      string `json:"name"`
		IP        string `json:"ip"`
		UA        string `json:"user_agent"`
		Timestamp int64  `json:"visited_at"`
	}
	export := map[string]any{
		"slug":            slug,
		"name":            u.Name,
		"size":            u.Size,
		"visibility":      u.Visibility,
		"created_at":      u.CreatedAt.Unix(),
		"total_visits":    total,
		"unique_visitors": unique,
	}
	cmts := make([]exportComment, 0, len(comments))
	for _, c := range comments {
		cmts = append(cmts, exportComment{
			Author: c.AuthorName, Body: c.Body, Selector: c.ElementSelector,
			Text: c.ElementText, AnchorKind: commentAnchorKind(c.ElementSelector, c.ElementText, c.AnchorKind),
			CreatedAt: c.CreatedAt.Unix(),
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

func (s *Server) handleViews(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(r.Context(), slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.viewerCanAccessUpload(r, u) {
		jsonError(w, http.StatusUnauthorized, uploadAccessRequiredMessage(u))
		return
	}
	total, unique, err := s.store.CountVisits(r.Context(), u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	const daySeconds = 86400
	const viewBucketCount = 7
	since := bucketStart(time.Now().Add(-time.Duration(viewBucketCount-1)*24*time.Hour), daySeconds)
	buckets, err := s.store.VisitBuckets(r.Context(), u.ID, since, daySeconds)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	type bucketJSON struct {
		T int64 `json:"t"`
		N int   `json:"n"`
	}
	filled := fillBuckets(buckets, since, daySeconds, viewBucketCount)
	jb := make([]bucketJSON, len(filled))
	for i, b := range filled {
		jb[i] = bucketJSON{T: b.Time.Unix(), N: b.Count}
	}
	jsonOK(w, map[string]any{
		"total":   total,
		"unique":  unique,
		"buckets": jb,
	})
}

func bucketStart(t time.Time, intervalSec int64) time.Time {
	return time.Unix((t.Unix()/intervalSec)*intervalSec, 0)
}

type bucket struct {
	Time  time.Time
	Count int
}

func fillBuckets(rows []models.VisitBucket, since time.Time, intervalSec int64, count int) []bucket {
	m := make(map[int64]int, len(rows))
	for _, r := range rows {
		m[r.Time.Unix()] = r.Count
	}
	out := make([]bucket, count)
	for i := 0; i < count; i++ {
		t := since.Add(time.Duration(i) * time.Duration(intervalSec) * time.Second)
		out[i] = bucket{Time: t, Count: m[t.Unix()]}
	}
	return out
}
