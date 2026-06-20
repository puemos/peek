package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/puemos/peek/internal/version"
)

// Run dispatches a command. args is argv without the program name.
func Run(args []string) int {
	if len(args) == 0 {
		usage()
		return 1
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "login":
		err = cmdLogin(rest)
	case "config":
		err = cmdConfig(rest)
	case "upload":
		err = cmdUpload(rest)
	case "list":
		err = cmdList(rest)
	case "delete":
		err = cmdDelete(rest)
	case "password":
		err = cmdPassword(rest)
	case "stats":
		err = cmdStats(rest)
	case "comments":
		err = cmdComments(rest)
	case "export":
		err = cmdExport(rest)
	case "delete-all":
		err = cmdDeleteAll(rest)
	case "token":
		err = cmdToken(rest)
	case "help", "-h", "--help":
		usage()
		return 0
	case "version", "-v", "--version":
		fmt.Println("peek " + version.String())
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprint(os.Stderr, `peek — Peek CLI

Usage:
  peek login [--host <url>]             set host + token (token entered hidden)
  peek config set --host <url>          set host (use 'login' / --token-stdin for the token)
  peek config show
  peek upload <file.html> [--password <pw>] [--name <filename>]
  peek list
  peek delete <slug>
  peek password <slug> --set <pw>      protect a page
  peek password <slug> --clear         remove protection
  peek stats <slug>
  peek comments <slug>                 list comments on one of your uploads
  peek export <slug>                   export all data for an upload (GDPR)
  peek delete-all                      delete all your uploads (GDPR right-to-be-forgotten)
  peek token create --name <name>      create a new user token (admin only)
  peek token list                      list tokens (admin only)
  peek token revoke <id>               revoke a token by id (admin only)

Token input (most secure first):
  peek login                           hidden prompt, nothing in shell history
  PEEK_TOKEN=…  (env override)          handy for CI
  peek config set --token-stdin        read token from a pipe
  peek config set --token-file <path>  read token from a file
  peek config set --token <token>      discouraged: exposed in history & 'ps'

Environment overrides:
  PEEK_HOST, PEEK_TOKEN
`)
}

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
	for i := 0; i < len(args); i++ {
		if args[i] == "--host" && i+1 < len(args) {
			host = args[i+1]
			i++
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
	token, err := readTokenInteractive()
	if err != nil {
		return err
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

// --- config ---

func cmdConfig(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: peek config set|show")
	}
	switch args[0] {
	case "set":
		return configSet(args[1:])
	case "show":
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}
		host := envOr("PEEK_HOST", cfg.Host)
		token := envOr("PEEK_TOKEN", cfg.Token)
		fmt.Printf("host:  %s\n", host)
		fmt.Printf("token: %s%s\n", maskToken(token), envNote())
		return nil
	default:
		return fmt.Errorf("unknown config subcommand: %s", args[0])
	}
}

func configSet(args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	host, token := cfg.Host, cfg.Token
	var tokenStdin, usedTokenFlag bool
	var tokenFile string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--host":
			if i+1 >= len(args) {
				return fmt.Errorf("--host requires a value")
			}
			host = args[i+1]
			i++
		case "--token":
			if i+1 >= len(args) {
				return fmt.Errorf("--token requires a value")
			}
			token = args[i+1]
			i++
			usedTokenFlag = true
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
	if usedTokenFlag {
		fmt.Fprintln(os.Stderr, "warning: --token is exposed in your shell history and process list (ps). Prefer `peek login`, --token-stdin, or --token-file.")
	}
	if host == "" {
		return fmt.Errorf("--host is required")
	}
	cfg.Host = host
	cfg.Token = token
	if err := SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Println("saved.")
	return nil
}

// --- upload ---

func cmdUpload(args []string) error {
	var (
		file     string
		password string
		name     string
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--password":
			if i+1 >= len(args) {
				return fmt.Errorf("--password requires a value")
			}
			password = args[i+1]
			i++
		case a == "--name":
			if i+1 >= len(args) {
				return fmt.Errorf("--name requires a value")
			}
			name = args[i+1]
			i++
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag: %s", a)
		default:
			if file != "" {
				return fmt.Errorf("only one file at a time")
			}
			file = a
		}
	}
	if file == "" {
		return fmt.Errorf("usage: peek upload <file.html> [--password <pw>]")
	}

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	var resp *http.Response
	if password != "" {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if name == "" {
			name = filepath.Base(file)
		}
		fw, err := mw.CreateFormFile("file", name)
		if err != nil {
			return err
		}
		if _, err := io.Copy(fw, f); err != nil {
			return err
		}
		_ = mw.WriteField("password", password)
		mw.Close()
		resp, err = c.req("POST", "/api/upload", &buf, mw.FormDataContentType())
	} else {
		if name == "" {
			name = filepath.Base(file)
		}
		resp, err = c.req("POST", "/api/upload?filename="+url.QueryEscape(name), f, "text/html")
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		Slug string `json:"slug"`
		URL  string `json:"url"`
	}
	if err := decodeResp(resp, &out); err != nil {
		return err
	}
	fmt.Printf("uploaded: %s\n", out.URL)
	fmt.Printf("slug:     %s\n", out.Slug)
	if password != "" {
		fmt.Println("protected: yes")
	}
	return nil
}

// --- list ---

func cmdList(args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	var items []struct {
		Slug      string `json:"slug"`
		Filename  string `json:"filename"`
		Owner     string `json:"owner"`
		Size      int64  `json:"size"`
		Protected bool   `json:"protected"`
		URL       string `json:"url"`
		CreatedAt int64  `json:"created_at"`
	}
	if err := c.getJSON("/api/uploads", &items); err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("no uploads yet.")
		return nil
	}
	fmt.Printf("%-12s  %-6s  %-8s  %-20s  %s\n", "SLUG", "SIZE", "PROTECT", "FILENAME", "URL")
	for _, it := range items {
		prot := "no"
		if it.Protected {
			prot = "yes"
		}
		fmt.Printf("%-12s  %-6s  %-8s  %-20s  %s\n", it.Slug, humanSize(it.Size), prot, truncate(it.Filename, 20), it.URL)
	}
	return nil
}

// --- delete ---

func cmdDelete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: peek delete <slug>")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	if err := c.del("/api/uploads/"+args[0], nil); err != nil {
		return err
	}
	fmt.Printf("deleted: %s\n", args[0])
	return nil
}

// --- password ---

func cmdPassword(args []string) error {
	var (
		slug     string
		password string
		clear    bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--set":
			if i+1 >= len(args) {
				return fmt.Errorf("--set requires a value")
			}
			password = args[i+1]
			i++
		case "--clear":
			clear = true
		default:
			if slug == "" {
				slug = args[i]
			}
		}
	}
	if slug == "" {
		return fmt.Errorf("usage: peek password <slug> --set <pw>|--clear")
	}
	if !clear && password == "" {
		return fmt.Errorf("use --set <pw> or --clear")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	body := map[string]any{"password": password, "clear": clear}
	var out struct {
		Protected bool `json:"protected"`
	}
	if err := c.postJSON("/api/uploads/"+slug+"/password", body, &out); err != nil {
		return err
	}
	if out.Protected {
		fmt.Printf("protected: yes\n")
	} else {
		fmt.Printf("protected: no (cleared)\n")
	}
	return nil
}

// --- stats ---

func cmdStats(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: peek stats <slug>")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	var st struct {
		Slug           string `json:"slug"`
		Filename       string `json:"filename"`
		TotalVisits    int    `json:"total_visits"`
		UniqueVisitors int    `json:"unique_visitors"`
		Recent         []struct {
			Name      string `json:"name"`
			IP        string `json:"ip"`
			UA        string `json:"user_agent"`
			Timestamp int64  `json:"visited_at"`
		} `json:"recent"`
	}
	if err := c.getJSON("/api/uploads/"+args[0]+"/stats", &st); err != nil {
		return err
	}
	fmt.Printf("slug:            %s\n", st.Slug)
	fmt.Printf("filename:        %s\n", st.Filename)
	fmt.Printf("total visits:    %d\n", st.TotalVisits)
	fmt.Printf("unique visitors: %d\n", st.UniqueVisitors)
	if len(st.Recent) > 0 {
		fmt.Println("\nrecent visits:")
		fmt.Printf("  %-20s  %-16s  %-20s  %s\n", "WHEN", "NAME", "IP (hashed)", "USER AGENT")
		for _, v := range st.Recent {
			t := time.Unix(v.Timestamp, 0).Format("2006-01-02 15:04")
			name := v.Name
			if name == "" {
				name = "(anonymous)"
			}
			fmt.Printf("  %-20s  %-16s  %-20s  %s\n", t, name, truncate(v.IP, 20), truncate(v.UA, 40))
		}
	}
	return nil
}

// --- comments ---

func cmdComments(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: peek comments <slug>")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	var list []struct {
		ID        int64  `json:"id"`
		Selector  string `json:"selector"`
		Text      string `json:"element_text"`
		Author    string `json:"author"`
		Body      string `json:"body"`
		CreatedAt int64  `json:"created_at"`
	}
	if err := c.getJSON("/api/uploads/"+args[0]+"/comments", &list); err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("no comments yet.")
		return nil
	}
	for i, cm := range list {
		when := time.Unix(cm.CreatedAt, 0).Format("2006-01-02 15:04")
		author := cm.Author
		if author == "" {
			author = "anonymous"
		}
		// Context: the on-page anchor a comment points at.
		var ctx string
		switch {
		case cm.Text != "":
			ctx = "“" + truncate(cm.Text, 60) + "”"
		case cm.Selector != "":
			ctx = cm.Selector
		default:
			ctx = "whole page"
		}
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%s · %s\n", author, when)
		fmt.Printf("  on: %s\n", ctx)
		fmt.Printf("  %s\n", cm.Body)
	}
	return nil
}

// --- export (GDPR data export) ---

func cmdExport(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: peek export <slug>")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	resp, err := c.req("GET", "/api/uploads/"+args[0]+"/export", nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// --- delete-all (GDPR right-to-be-forgotten) ---

func cmdDeleteAll(args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	var out struct {
		Deleted int `json:"deleted"`
	}
	if err := c.del("/api/uploads-by-owner", &out); err != nil {
		return err
	}
	fmt.Printf("deleted %d uploads\n", out.Deleted)
	return nil
}

// --- token ---

func cmdToken(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: peek token create --name <name> | peek token list")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	switch args[0] {
	case "create":
		var name string
		for i := 1; i < len(args); i++ {
			if args[i] == "--name" && i+1 < len(args) {
				name = args[i+1]
				i++
			}
		}
		if name == "" {
			return fmt.Errorf("--name is required")
		}
		var out struct {
			Token string `json:"token"`
			Name  string `json:"name"`
		}
		if err := c.postJSON("/api/tokens", map[string]string{"name": name}, &out); err != nil {
			return err
		}
		fmt.Printf("created token for %q:\n  %s\n", out.Name, out.Token)
		return nil
	case "list":
		var out []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Admin bool   `json:"admin"`
		}
		if err := c.getJSON("/api/tokens", &out); err != nil {
			return err
		}
		fmt.Printf("%-5s  %-8s  %s\n", "ID", "ADMIN", "NAME")
		for _, t := range out {
			adm := "no"
			if t.Admin {
				adm = "yes"
			}
			fmt.Printf("%-5d  %-8s  %s\n", t.ID, adm, truncate(t.Name, 40))
		}
		fmt.Println("\nTokens are stored hashed and shown only once at creation.")
		return nil
	case "revoke":
		var id string
		for i := 1; i < len(args); i++ {
			if args[i] == "--id" && i+1 < len(args) {
				id = args[i+1]
				i++
			} else if !strings.HasPrefix(args[i], "-") {
				id = args[i]
			}
		}
		if id == "" {
			return fmt.Errorf("usage: peek token revoke <id>  (see ids in `peek token list`)")
		}
		if err := c.del("/api/tokens/"+id, nil); err != nil {
			return err
		}
		fmt.Printf("revoked token %s\n", id)
		return nil
	default:
		return fmt.Errorf("unknown token subcommand: %s", args[0])
	}
}

// --- formatting helpers ---

func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func maskToken(t string) string {
	if len(t) <= 8 {
		return t
	}
	return t[:4] + "…" + t[len(t)-4:]
}

func envNote() string {
	host := os.Getenv("PEEK_HOST")
	tok := os.Getenv("PEEK_TOKEN")
	if host != "" || tok != "" {
		return "  (env override active)"
	}
	return ""
}
