package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

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
	if usedTokenFlag || tokenFile != "" || tokenStdin {
		discovery, err := discoverAuth(host)
		if err == nil && len(discovery.Providers) > 0 {
			return fmt.Errorf("this server requires OAuth browser login; run `peek login --host %s`", host)
		}
	}
	cfg.Host = host
	cfg.Token = token
	if err := SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Println("saved.")
	return nil
}
