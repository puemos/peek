package cli

import (
	"io"
	"os"
	"testing"
)

func configureTestClient(t *testing.T, host string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PEEK_HOST", host)
	t.Setenv("PEEK_TOKEN", "test-token")
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}

	readDone := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		data, err := io.ReadAll(r)
		readDone <- struct {
			out string
			err error
		}{out: string(data), err: err}
	}()

	os.Stdout = w
	runErr := fn()
	os.Stdout = old

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	result := <-readDone
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	if result.err != nil {
		t.Fatalf("read captured stdout: %v", result.err)
	}
	return result.out, runErr
}
