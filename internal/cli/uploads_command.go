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
		slug          string
		password      string
		passwordStdin bool
		clear         bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--set":
			if i+1 >= len(args) {
				return fmt.Errorf("--set requires a value")
			}
			password = args[i+1]
			i++
		case "--set-stdin":
			passwordStdin = true
		case "--clear":
			clear = true
		default:
			if slug == "" {
				slug = args[i]
			}
		}
	}
	if slug == "" {
		return fmt.Errorf("usage: peek password <slug> --set <pw>|--set-stdin|--clear")
	}
	if password != "" && passwordStdin {
		return fmt.Errorf("use only one of --set or --set-stdin")
	}
	if clear && (password != "" || passwordStdin) {
		return fmt.Errorf("use either --clear or a password setter")
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
	if !clear && password == "" {
		return fmt.Errorf("use --set <pw>, --set-stdin, or --clear")
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
