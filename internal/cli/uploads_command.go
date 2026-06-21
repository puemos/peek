package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// --- list ---

func cmdList(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: peek list")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	var items []struct {
		Slug       string `json:"slug"`
		Name       string `json:"name"`
		Owner      string `json:"owner"`
		Size       int64  `json:"size"`
		Visibility string `json:"visibility"`
		URL        string `json:"url"`
		CreatedAt  int64  `json:"created_at"`
	}
	if err := c.getJSON("/api/uploads", &items); err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("no uploads yet.")
		return nil
	}
	fmt.Printf("%-12s  %-6s  %-10s  %-20s  %s\n", "SLUG", "SIZE", "VISIBILITY", "NAME", "URL")
	for _, it := range items {
		fmt.Printf("%-12s  %-6s  %-10s  %-20s  %s\n", it.Slug, humanSize(it.Size), it.Visibility, truncate(it.Name, 20), it.URL)
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

// --- visibility ---

func cmdVisibility(args []string) error {
	var (
		slug          string
		visibility    string
		password      string
		passwordStdin bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--password":
			if i+1 >= len(args) {
				return fmt.Errorf("--password requires a value")
			}
			password = args[i+1]
			i++
		case "--password-stdin":
			passwordStdin = true
		default:
			switch {
			case strings.HasPrefix(args[i], "-"):
				return fmt.Errorf("unknown flag: %s", args[i])
			case slug == "":
				slug = args[i]
			case visibility == "":
				visibility = args[i]
			default:
				return fmt.Errorf("unexpected argument: %s", args[i])
			}
		}
	}
	if slug == "" || visibility == "" {
		return fmt.Errorf("usage: peek visibility <slug> public|private|password [--password <pw>|--password-stdin]")
	}
	if !validVisibility(visibility) {
		return fmt.Errorf("visibility must be public, password, or private")
	}
	if password != "" && passwordStdin {
		return fmt.Errorf("use only one of --password or --password-stdin")
	}
	if passwordStdin {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		password = strings.TrimSpace(line)
		if password == "" {
			return fmt.Errorf("no password provided on stdin")
		}
	}
	if visibility == "password" && password == "" {
		return fmt.Errorf("password visibility requires --password or --password-stdin")
	}
	if visibility != "password" && password != "" {
		return fmt.Errorf("--password is only valid with password visibility")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	body := map[string]any{"visibility": visibility, "password": password}
	var out struct {
		Visibility string `json:"visibility"`
	}
	if err := c.postJSON("/api/uploads/"+slug+"/visibility", body, &out); err != nil {
		return err
	}
	fmt.Printf("visibility: %s\n", out.Visibility)
	return nil
}
