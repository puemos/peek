package main

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
