package uploads

import (
	"context"
	"errors"
	"strings"
	"testing"
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
	if !ValidatePasswordLength("short") {
		t.Fatalf("short password should be valid")
	}
	if !ValidatePasswordLength(makeString('a', 72)) {
		t.Fatalf("exactly 72 chars should be valid")
	}
	if ValidatePasswordLength(makeString('a', 73)) {
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
		slug, err := generateRandomSlug(context.Background(), mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slug == "" || len(slug) < 8 {
			t.Fatalf("expected non-empty slug, got %q", slug)
		}
	})
	t.Run("retries then free", func(t *testing.T) {
		mock := &mockSlugChecker{counter: 0, returnErrorAfter: 3}
		slug, err := generateRandomSlug(context.Background(), mock)
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
		_, err := generateRandomSlug(context.Background(), mock)
		if err == nil {
			t.Fatalf("expected error after retries exhausted")
		}
	})
	t.Run("lookup failure", func(t *testing.T) {
		wantErr := errors.New("database unavailable")
		mock := &mockSlugChecker{err: wantErr}
		_, err := generateRandomSlug(context.Background(), mock)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected lookup error, got %v", err)
		}
	})
}

type mockSlugChecker struct {
	existing         map[string]bool
	counter          int
	returnErrorAfter int
	err              error
}

func (m *mockSlugChecker) UploadSlugExists(_ context.Context, slug string) (bool, error) {
	m.counter++
	if m.err != nil {
		return false, m.err
	}
	if m.returnErrorAfter > 0 && m.counter < m.returnErrorAfter {
		return true, nil
	}
	return m.existing[slug], nil
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"My Cool Page", "my-cool-page"},
		{"  Hello   World  ", "hello-world"},
		{"annual-report-2025", "annual-report-2025"},
		{"foo.html", "foo-html"},
		{"café & bistro", "caf-bistro"},
		{"--Cool--Page--", "cool-page"},
		{"你好世界", ""},
		{"", ""},
		{"a!@#$%^&*()b", "a-b"},
		{"UPPER lower", "upper-lower"},
		{"single", "single"},
		{"has_underscore", "has-underscore"},
		{"a-b-c", "a-b-c"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := slugify(tc.in)
			if got != tc.want {
				t.Fatalf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGenerateSlugFromName(t *testing.T) {
	t.Run("first upload", func(t *testing.T) {
		mock := &mockSlugChecker{existing: map[string]bool{}}
		slug, err := generateSlugFromName(context.Background(), "My Cool Page", mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slug != "my-cool-page" {
			t.Fatalf("expected my-cool-page, got %q", slug)
		}
	})
	t.Run("clash appends suffix", func(t *testing.T) {
		mock := &mockSlugChecker{existing: map[string]bool{"my-cool-page": true}}
		slug, err := generateSlugFromName(context.Background(), "My Cool Page", mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(slug, "my-cool-page-") {
			t.Fatalf("expected clash suffix, got %q", slug)
		}
		suffix := slug[len("my-cool-page-"):]
		if len(suffix) < 4 {
			t.Fatalf("suffix too short: %q", suffix)
		}
	})
	t.Run("empty name falls back to random", func(t *testing.T) {
		mock := &mockSlugChecker{existing: map[string]bool{}}
		slug, err := generateSlugFromName(context.Background(), "", mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slug == "" || len(slug) < 8 {
			t.Fatalf("expected non-empty random slug, got %q", slug)
		}
	})
	t.Run("all special chars falls back to random", func(t *testing.T) {
		mock := &mockSlugChecker{existing: map[string]bool{}}
		slug, err := generateSlugFromName(context.Background(), "!!!", mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slug == "" || len(slug) < 8 {
			t.Fatalf("expected non-empty random slug, got %q", slug)
		}
	})
	t.Run("exhausted retries falls back to random", func(t *testing.T) {
		allTaken := &mockSlugChecker{existing: map[string]bool{}, returnErrorAfter: 6}
		slug, err := generateSlugFromName(context.Background(), "test", allTaken)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slug == "" || len(slug) < 8 {
			t.Fatalf("expected fallback random slug, got %q", slug)
		}
	})
}
