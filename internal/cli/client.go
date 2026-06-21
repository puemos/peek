package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config holds the saved host+token pair, stored in <user-config-dir>/peek/config.json.
type Config struct {
	Host  string `json:"host"`
	Token string `json:"token"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	p := filepath.Join(dir, "peek")
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(p, "config.json"), nil
}

func LoadConfig() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return &Config{}, nil
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func SaveConfig(c *Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// client wraps the API with host+token from env or saved config.
type client struct {
	host  string
	token string
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func newClient(cfg *Config) (*client, error) {
	host := envOr("PEEK_HOST", cfg.Host)
	token := envOr("PEEK_TOKEN", cfg.Token)
	if host == "" {
		return nil, fmt.Errorf("host not set. Run: peek login --host <url>")
	}
	if token == "" {
		return nil, fmt.Errorf("token not set. Run: peek login --host <url>")
	}
	return &client{host: strings.TrimRight(host, "/"), token: token}, nil
}

func (c *client) req(method, path string, body io.Reader, ct string) (*http.Response, error) {
	req, err := http.NewRequest(method, c.host+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	return httpClient.Do(req)
}

func (c *client) getJSON(path string, out any) error {
	resp, err := c.req("GET", path, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResp(resp, out)
}

func (c *client) postJSON(path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(b))
	}
	resp, err := c.req("POST", path, body, "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResp(resp, out)
}

func (c *client) del(path string, out any) error {
	resp, err := c.req("DELETE", path, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResp(resp, out)
}

func decodeResp(resp *http.Response, out any) error {
	if resp.StatusCode >= 400 {
		return decodeErrorResp(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func decodeErrorResp(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return fmt.Errorf("%s: read error response: %w", resp.Status, err)
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("%s", resp.Status)
	}
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && strings.TrimSpace(e.Error) != "" {
		return fmt.Errorf("%s", strings.TrimSpace(e.Error))
	}
	return fmt.Errorf("%s: %s", resp.Status, msg)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
