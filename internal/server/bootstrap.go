package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/puemos/peek/internal/db"
)

func isLocalBaseURL(u string) bool {
	return strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1") || strings.Contains(u, "[::1]")
}

// setCookie applies the deployment-wide Secure flag, then writes the cookie.
func (s *Server) setCookie(w http.ResponseWriter, c *http.Cookie) {
	c.Secure = s.secure
	http.SetCookie(w, c)
}

func loadOrCreateSecret(path string) string {
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		b := make([]byte, 128)
		n, _ := f.Read(b)
		if n >= 32 {
			return string(b[:n])
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("generating secret: " + err.Error())
	}
	s := hex.EncodeToString(b)
	_ = os.WriteFile(path, []byte(s), 0o600)
	return s
}

const setupCodeFile = "setup.key"

func setupCodePath(dataDir string) string {
	return filepath.Join(dataDir, setupCodeFile)
}

// bootstrapSetup prepares a one-time setup URL when the database has no
// accounts. The first admin is created through /setup, not through a token.
func bootstrapSetup(store *db.Store, dataDir, baseURL string) (string, error) {
	n, err := store.CountAccounts()
	if err != nil {
		return "", err
	}
	if n > 0 {
		_ = os.Remove(setupCodePath(dataDir))
		return "", nil
	}

	path := setupCodePath(dataDir)
	if b, err := os.ReadFile(path); err == nil {
		code := strings.TrimSpace(string(b))
		if code != "" {
			printSetupURL(baseURL, code)
			return code, nil
		}
	}
	code, err := randID(24)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(code+"\n"), 0o600); err != nil {
		return "", err
	}
	printSetupURL(baseURL, code)
	return code, nil
}

func printSetupURL(baseURL, code string) {
	fmt.Println("==========================================================")
	fmt.Println(" Peek first-run setup")
	fmt.Println(" Open this URL to create the first admin account:")
	fmt.Printf("   %s/setup?code=%s\n", baseURL, url.QueryEscape(code))
	fmt.Println("==========================================================")
}

func (s *Server) setupRequired() bool {
	n, err := s.store.CountAccounts()
	return err == nil && n == 0
}

func (s *Server) readSetupCode() string {
	b, err := os.ReadFile(setupCodePath(s.dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (s *Server) validSetupCode(code string) bool {
	if !s.setupRequired() {
		return false
	}
	want := s.readSetupCode()
	if want == "" || strings.TrimSpace(code) != want {
		return false
	}
	return true
}

func (s *Server) clearSetupCode() {
	_ = os.Remove(setupCodePath(s.dataDir))
}
