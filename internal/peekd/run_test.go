package peekd

import (
	"path/filepath"
	"testing"
)

func TestHealthcheckURL(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "bare port", addr: ":7700", want: "http://localhost:7700/healthz"},
		{name: "host port", addr: "127.0.0.1:7700", want: "http://127.0.0.1:7700/healthz"},
		{name: "http url", addr: "http://example.test:7700/", want: "http://example.test:7700/healthz"},
		{name: "https url", addr: "https://peek.example.com", want: "https://peek.example.com/healthz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := healthcheckURL(tt.addr); got != tt.want {
				t.Fatalf("healthcheckURL(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestBackupArgs(t *testing.T) {
	dir := t.TempDir()
	dataDir, backupPath, err := backupArgs([]string{"--data", dir})
	if err != nil {
		t.Fatalf("backupArgs: %v", err)
	}
	if dataDir != dir {
		t.Fatalf("dataDir = %q, want %q", dataDir, dir)
	}
	if backupPath != filepath.Join(dir, "peek-backup.db") {
		t.Fatalf("backupPath = %q", backupPath)
	}

	custom := filepath.Join(t.TempDir(), "custom.db")
	_, backupPath, err = backupArgs([]string{"--data", dir, custom})
	if err != nil {
		t.Fatalf("backupArgs custom: %v", err)
	}
	if backupPath != custom {
		t.Fatalf("backupPath = %q, want %q", backupPath, custom)
	}
}

func TestBackupArgsRejectsExtraArgs(t *testing.T) {
	if _, _, err := backupArgs([]string{"one.db", "two.db"}); err == nil {
		t.Fatal("expected extra backup args to fail")
	}
}

func TestParseServeConfigMapsFlagsAndEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PEEK_ADDR", ":8800")
	t.Setenv("PEEK_S3_REGION", "auto")
	t.Setenv("PEEK_TRUSTED_PROXY", "true")

	cfg, showVersion, err := parseServeConfig([]string{
		"--data", dir,
		"--base-url", "https://peek.example.test",
		"--max-upload", "12345",
		"--storage", "s3",
		"--s3-endpoint", "https://s3.example.test",
		"--s3-bucket", "peek",
		"--s3-access-key", "access",
		"--s3-secret-key", "secret",
		"--s3-allow-private-endpoint",
		"--max-total-size", "999",
		"--retention-days", "7",
	})
	if err != nil {
		t.Fatalf("parseServeConfig: %v", err)
	}
	if showVersion {
		t.Fatal("showVersion = true")
	}
	if cfg.Addr != ":8800" || cfg.DataDir != dir || cfg.BaseURL != "https://peek.example.test" {
		t.Fatalf("basic config = %+v", cfg)
	}
	if cfg.MaxUpload != 12345 || cfg.MaxTotalSize != 999 || cfg.RetentionDays != 7 {
		t.Fatalf("limits = %+v", cfg)
	}
	if cfg.Storage != "s3" || cfg.S3Endpoint != "https://s3.example.test" || cfg.S3Bucket != "peek" {
		t.Fatalf("s3 config = %+v", cfg)
	}
	if cfg.S3Region != "auto" || cfg.S3AccessKey != "access" || cfg.S3SecretKey != "secret" || !cfg.S3AllowPrivateEndpoint {
		t.Fatalf("s3 credentials/config = %+v", cfg)
	}
	if !cfg.TrustedProxy {
		t.Fatal("TrustedProxy = false")
	}
}

func TestParseServeConfigRejectsInvalidIntegerEnv(t *testing.T) {
	t.Setenv("PEEK_MAX_UPLOAD", "two-mib")

	_, _, err := parseServeConfig(nil)
	if err == nil || err.Error() != "PEEK_MAX_UPLOAD must be an integer" {
		t.Fatalf("error = %v", err)
	}
}

func TestParseServeConfigRejectsInvalidBoolEnv(t *testing.T) {
	t.Setenv("PEEK_TRUSTED_PROXY", "maybe")

	_, _, err := parseServeConfig(nil)
	if err == nil || err.Error() != "PEEK_TRUSTED_PROXY must be a boolean" {
		t.Fatalf("error = %v", err)
	}
}

func TestParseServeConfigAcceptsCaseInsensitiveBoolEnv(t *testing.T) {
	t.Setenv("PEEK_TRUSTED_PROXY", "ON")
	t.Setenv("PEEK_S3_ALLOW_PRIVATE_ENDPOINT", "Yes")

	cfg, showVersion, err := parseServeConfig([]string{"--data", t.TempDir()})
	if err != nil {
		t.Fatalf("parseServeConfig: %v", err)
	}
	if showVersion {
		t.Fatal("showVersion = true")
	}
	if !cfg.TrustedProxy || !cfg.S3AllowPrivateEndpoint {
		t.Fatalf("bool config = %+v", cfg)
	}
}
