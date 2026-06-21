package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

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
