package server

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

type failingJSONWriter struct {
	header http.Header
	code   int
}

func (w *failingJSONWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *failingJSONWriter) WriteHeader(code int) {
	w.code = code
}

func (w *failingJSONWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriteJSONLogsEncodeFailure(t *testing.T) {
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	w := &failingJSONWriter{}

	jsonOK(w, map[string]string{"status": "ok"})

	if w.code != http.StatusOK {
		t.Fatalf("status = %d", w.code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q", got)
	}
	if !strings.Contains(logs.String(), "write json response failed") {
		t.Fatalf("write failure was not logged: %s", logs.String())
	}
}
