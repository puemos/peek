package server

import (
	"testing"

	"github.com/puemos/peek/internal/models"
)

func TestLooksLikeHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"doctype", "<!DOCTYPE html><html><body>hi</body></html>", true},
		{"html tag", "<html></html>", true},
		{"body tag", "<body></body>", true},
		{"div tag", "<div>hi</div>", true},
		{"p tag", "<p>hi</p>", true},
		{"span tag", "<span>hi</span>", true},
		{"minimal tag", "<a>link</a>", true},
		{"empty", "", false},
		{"plain text", "hello world", false},
		{"binary null", "hello\x00world", false},
		{"invalid utf8", "\xff\xfe", false},
		{"only less", "<", false},
		{"only greater", ">", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeHTML([]byte(tc.in)); got != tc.want {
				t.Fatalf("looksLikeHTML(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidatePasswordLength(t *testing.T) {
	if !validatePasswordLength("short") {
		t.Fatalf("short password should be valid")
	}
	if !validatePasswordLength(makeString('a', 72)) {
		t.Fatalf("exactly 72 chars should be valid")
	}
	if validatePasswordLength(makeString('a', 73)) {
		t.Fatalf("73 chars should be rejected")
	}
}

func makeString(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

func TestGenerateSlugUniqueness(t *testing.T) {
	t.Run("always free", func(t *testing.T) {
		mock := &mockSlugChecker{existing: map[string]bool{}}
		slug, err := generateSlug(mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slug == "" || len(slug) < 8 {
			t.Fatalf("expected non-empty slug, got %q", slug)
		}
	})
	t.Run("retries then free", func(t *testing.T) {
		mock := &mockSlugChecker{counter: 0, returnErrorAfter: 3}
		slug, err := generateSlug(mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slug == "" {
			t.Fatalf("expected non-empty slug")
		}
		if mock.counter < 3 {
			t.Fatalf("expected retries to reach at least 3, got %d", mock.counter)
		}
	})
	t.Run("exhausted retries", func(t *testing.T) {
		mock := &mockSlugChecker{counter: 0, returnErrorAfter: 100}
		_, err := generateSlug(mock)
		if err == nil {
			t.Fatalf("expected error after retries exhausted")
		}
	})
}

type mockSlugChecker struct {
	existing         map[string]bool
	counter          int
	returnErrorAfter int
}

func (m *mockSlugChecker) GetUpload(slug string) (*models.Upload, error) {
	m.counter++
	if m.returnErrorAfter > 0 && m.counter < m.returnErrorAfter {
		return &models.Upload{Slug: slug}, nil
	}
	return nil, errInvalid
}
