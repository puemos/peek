package cli

import (
	"fmt"
	"strings"
)

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
