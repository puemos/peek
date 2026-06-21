package cli

import (
	"fmt"
	"io"
)

// --- export (upload data export) ---

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

// --- delete-all (upload data deletion) ---

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
