package server

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
)

// Counters for basic request metrics. Atomic for lock-free increments.
var (
	reqTotal    atomic.Int64
	reqErrors   atomic.Int64
	uploadsTotal atomic.Int64
	commentsTotal atomic.Int64
)

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	var sb strings.Builder

	// Go runtime metrics.
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	sb.WriteString(fmt.Sprintf("# HELP peek_go_goroutines Number of goroutines.\n"))
	sb.WriteString(fmt.Sprintf("# TYPE peek_go_goroutines gauge\n"))
	sb.WriteString(fmt.Sprintf("peek_go_goroutines %d\n", runtime.NumGoroutine()))

	sb.WriteString(fmt.Sprintf("# HELP peek_go_mem_alloc_bytes Bytes allocated and still in use.\n"))
	sb.WriteString(fmt.Sprintf("# TYPE peek_go_mem_alloc_bytes gauge\n"))
	sb.WriteString(fmt.Sprintf("peek_go_mem_alloc_bytes %d\n", m.Alloc))

	sb.WriteString(fmt.Sprintf("# HELP peek_go_mem_sys_bytes Bytes obtained from system.\n"))
	sb.WriteString(fmt.Sprintf("# TYPE peek_go_mem_sys_bytes gauge\n"))
	sb.WriteString(fmt.Sprintf("peek_go_mem_sys_bytes %d\n", m.Sys))

	sb.WriteString(fmt.Sprintf("# HELP peek_go_gc_count Number of completed GC cycles.\n"))
	sb.WriteString(fmt.Sprintf("# TYPE peek_go_gc_count counter\n"))
	sb.WriteString(fmt.Sprintf("peek_go_gc_count %d\n", m.NumGC))

	// Application metrics.
	sb.WriteString(fmt.Sprintf("# HELP peek_requests_total Total HTTP requests.\n"))
	sb.WriteString(fmt.Sprintf("# TYPE peek_requests_total counter\n"))
	sb.WriteString(fmt.Sprintf("peek_requests_total %d\n", reqTotal.Load()))

	sb.WriteString(fmt.Sprintf("# HELP peek_request_errors_total Total HTTP errors (4xx/5xx).\n"))
	sb.WriteString(fmt.Sprintf("# TYPE peek_request_errors_total counter\n"))
	sb.WriteString(fmt.Sprintf("peek_request_errors_total %d\n", reqErrors.Load()))

	sb.WriteString(fmt.Sprintf("# HELP peek_uploads_total Total uploads created.\n"))
	sb.WriteString(fmt.Sprintf("# TYPE peek_uploads_total counter\n"))
	sb.WriteString(fmt.Sprintf("peek_uploads_total %d\n", uploadsTotal.Load()))

	sb.WriteString(fmt.Sprintf("# HELP peek_comments_total Total comments posted.\n"))
	sb.WriteString(fmt.Sprintf("# TYPE peek_comments_total counter\n"))
	sb.WriteString(fmt.Sprintf("peek_comments_total %d\n", commentsTotal.Load()))

	// Upload count from DB.
	count, err := s.store.CountUploads()
	if err == nil {
		sb.WriteString(fmt.Sprintf("# HELP peek_uploads_current Current number of uploads.\n"))
		sb.WriteString(fmt.Sprintf("# TYPE peek_uploads_current gauge\n"))
		sb.WriteString(fmt.Sprintf("peek_uploads_current %d\n", count))
	}

	// Total storage used.
	totalSize, err := s.store.SumUploadSizes()
	if err == nil {
		sb.WriteString(fmt.Sprintf("# HELP peek_storage_bytes Total bytes used by uploads.\n"))
		sb.WriteString(fmt.Sprintf("# TYPE peek_storage_bytes gauge\n"))
		sb.WriteString(fmt.Sprintf("peek_storage_bytes %d\n", totalSize))
	}

	// Token count.
	tokenCount, err := s.store.CountTokens()
	if err == nil {
		sb.WriteString(fmt.Sprintf("# HELP peek_tokens_current Current number of tokens.\n"))
		sb.WriteString(fmt.Sprintf("# TYPE peek_tokens_current gauge\n"))
		sb.WriteString(fmt.Sprintf("peek_tokens_current %d\n", tokenCount))
	}

	_, _ = w.Write([]byte(sb.String()))
}
