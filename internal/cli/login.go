package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"
)

// --- login ---

// readTokenInteractive reads a token without echoing it on a TTY, or from a
// pipe when stdin is not a terminal. Either way the token never lands in argv
// or shell history.
func readTokenInteractive() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "Paste your token (input hidden): ")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func cmdLogin(args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	host := cfg.Host
	var (
		forceOAuth    bool
		tokenFlag     string
		tokenFile     string
		tokenStdin    bool
		usedTokenFlag bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--host":
			if i+1 >= len(args) {
				return fmt.Errorf("--host requires a value")
			}
			host = args[i+1]
			i++
		case "--oauth":
			forceOAuth = true
		case "--token":
			if i+1 >= len(args) {
				return fmt.Errorf("--token requires a value")
			}
			tokenFlag = args[i+1]
			usedTokenFlag = true
			i++
		case "--token-file":
			if i+1 >= len(args) {
				return fmt.Errorf("--token-file requires a value")
			}
			tokenFile = args[i+1]
			i++
		case "--token-stdin":
			tokenStdin = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	if host == "" {
		fmt.Fprint(os.Stderr, "Host (e.g. https://example.com): ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		host = strings.TrimSpace(line)
	}
	if host == "" {
		return fmt.Errorf("host is required")
	}

	tokenMode := tokenFlag != "" || tokenFile != "" || tokenStdin || !term.IsTerminal(int(os.Stdin.Fd()))
	discovery, discoveryErr := discoverAuth(host)
	if forceOAuth || !tokenMode {
		if discoveryErr == nil && discovery.BrowserLogin {
			return loginOAuth(cfg, host)
		}
		if forceOAuth {
			if discoveryErr != nil {
				return discoveryErr
			}
			return fmt.Errorf("browser login is not configured on %s", host)
		}
	}
	if tokenMode && discoveryErr == nil && len(discovery.Providers) > 0 {
		return fmt.Errorf("this server requires OAuth browser login; run `peek login --host %s`", host)
	}

	token := strings.TrimSpace(tokenFlag)
	if tokenFile != "" {
		b, err := os.ReadFile(tokenFile)
		if err != nil {
			return err
		}
		token = strings.TrimSpace(string(b))
	}
	if tokenStdin {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		token = strings.TrimSpace(line)
	}
	if token == "" {
		token, err = readTokenInteractive()
		if err != nil {
			return err
		}
	}
	if usedTokenFlag {
		fmt.Fprintln(os.Stderr, "warning: --token is exposed in your shell history and process list (ps). Prefer browser login, --token-stdin, or --token-file.")
	}
	if token == "" {
		return fmt.Errorf("no token provided")
	}
	cfg.Host = host
	cfg.Token = token
	if err := SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Println("saved.")
	return nil
}

type authDiscovery struct {
	Providers []struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"providers"`
	BrowserLogin  bool `json:"browser_login"`
	OAuthRequired bool `json:"oauth_required"`
}

func discoverAuth(host string) (authDiscovery, error) {
	var out authDiscovery
	resp, err := httpClient.Get(strings.TrimRight(host, "/") + "/api/auth/providers")
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return out, nil
	}
	if resp.StatusCode >= 400 {
		return out, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	if len(out.Providers) > 0 {
		out.BrowserLogin = true
	}
	return out, nil
}

func oauthAvailable(host string) (bool, error) {
	out, err := discoverAuth(host)
	if err != nil {
		return false, err
	}
	return out.BrowserLogin, nil
}

func loginOAuth(cfg *Config, host string) error {
	host = strings.TrimRight(host, "/")
	var start struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := postJSONNoAuth(host, "/api/cli/login/start", nil, &start); err != nil {
		return err
	}
	if start.Interval <= 0 {
		start.Interval = 2
	}
	if start.ExpiresIn <= 0 {
		start.ExpiresIn = 900
	}
	fmt.Fprintf(os.Stderr, "Opening browser for Peek login.\nCode: %s\nURL:  %s\n", start.UserCode, start.VerificationURL)
	if err := openBrowser(start.VerificationURL); err != nil {
		fmt.Fprintln(os.Stderr, "Open the URL above to continue.")
	}
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(start.Interval) * time.Second)
		var poll struct {
			Status string `json:"status"`
			Token  string `json:"token"`
		}
		body := map[string]string{"device_code": start.DeviceCode}
		if err := postJSONNoAuth(host, "/api/cli/login/poll", body, &poll); err != nil {
			return err
		}
		switch poll.Status {
		case "pending":
			continue
		case "approved":
			if poll.Token == "" {
				return fmt.Errorf("server approved login without a token")
			}
			cfg.Host = host
			cfg.Token = poll.Token
			if err := SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Println("saved.")
			return nil
		case "denied":
			return fmt.Errorf("login denied")
		case "expired":
			return fmt.Errorf("login expired")
		case "consumed":
			return fmt.Errorf("login already consumed")
		default:
			return fmt.Errorf("unexpected login status: %s", poll.Status)
		}
	}
	return fmt.Errorf("login expired")
}

func postJSONNoAuth(host, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(in); err != nil {
			return err
		}
		body = &buf
	}
	req, err := http.NewRequest(http.MethodPost, host+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResp(resp, out)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
