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

	var run func(*client) error
	switch args[0] {
	case "create":
		var name string
		for i := 1; i < len(args); i++ {
			switch {
			case args[i] == "--name":
				if i+1 >= len(args) {
					return fmt.Errorf("--name requires a value")
				}
				name = args[i+1]
				i++
			case strings.HasPrefix(args[i], "-"):
				return fmt.Errorf("unknown flag: %s", args[i])
			default:
				return fmt.Errorf("unexpected argument: %s", args[i])
			}
		}
		if name == "" {
			return fmt.Errorf("--name is required")
		}
		run = func(c *client) error {
			var out struct {
				Token string `json:"token"`
				Name  string `json:"name"`
			}
			if err := c.postJSON("/api/tokens", map[string]string{"name": name}, &out); err != nil {
				return err
			}
			fmt.Printf("created token for %q:\n  %s\n", out.Name, out.Token)
			return nil
		}
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: peek token list")
		}
		run = func(c *client) error {
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
		}
	case "revoke":
		var id string
		for i := 1; i < len(args); i++ {
			switch {
			case args[i] == "--id":
				if i+1 >= len(args) {
					return fmt.Errorf("--id requires a value")
				}
				if id != "" {
					return fmt.Errorf("usage: peek token revoke <id>  (see ids in `peek token list`)")
				}
				id = args[i+1]
				i++
			case strings.HasPrefix(args[i], "-"):
				return fmt.Errorf("unknown flag: %s", args[i])
			case id == "":
				id = args[i]
			default:
				return fmt.Errorf("usage: peek token revoke <id>  (see ids in `peek token list`)")
			}
		}
		if id == "" {
			return fmt.Errorf("usage: peek token revoke <id>  (see ids in `peek token list`)")
		}
		run = func(c *client) error {
			if err := c.del("/api/tokens/"+id, nil); err != nil {
				return err
			}
			fmt.Printf("revoked token %s\n", id)
			return nil
		}
	default:
		return fmt.Errorf("unknown token subcommand: %s", args[0])
	}

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	return run(c)
}
