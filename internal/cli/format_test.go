package cli

import "testing"

func TestHumanSize(t *testing.T) {
	tests := []struct {
		name string
		size int64
		want string
	}{
		{name: "bytes", size: 512, want: "512B"},
		{name: "kilobytes", size: 1536, want: "1.5K"},
		{name: "megabytes", size: 3 * 1024 * 1024, want: "3.0M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := humanSize(tt.size); got != tt.want {
				t.Fatalf("humanSize(%d) = %q, want %q", tt.size, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "short", in: "short", n: 10, want: "short"},
		{name: "exact", in: "12345", n: 5, want: "12345"},
		{name: "long", in: "1234567890", n: 6, want: "12345…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.in, tt.n); got != tt.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "short", in: "short", want: "short"},
		{name: "boundary", in: "12345678", want: "12345678"},
		{name: "long", in: "1234567890abcdef", want: "1234…cdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskToken(tt.in); got != tt.want {
				t.Fatalf("maskToken(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEnvNote(t *testing.T) {
	t.Setenv("PEEK_HOST", "")
	t.Setenv("PEEK_TOKEN", "")

	if got := envNote(); got != "" {
		t.Fatalf("envNote without env = %q", got)
	}

	t.Setenv("PEEK_HOST", "http://example.test")
	if got := envNote(); got != "  (env override active)" {
		t.Fatalf("envNote with host env = %q", got)
	}
}
