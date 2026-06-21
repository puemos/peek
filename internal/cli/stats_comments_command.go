package cli

import (
	"fmt"
	"time"
)

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
		Name           string `json:"name"`
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
	fmt.Printf("name:            %s\n", st.Name)
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
		ID         int64  `json:"id"`
		Selector   string `json:"selector"`
		Text       string `json:"element_text"`
		AnchorKind string `json:"anchor_kind"`
		Author     string `json:"author"`
		Body       string `json:"body"`
		CreatedAt  int64  `json:"created_at"`
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
		kind := cm.AnchorKind
		if kind == "" {
			if cm.Selector == "" {
				kind = "page"
			} else if cm.Text != "" {
				kind = "text"
			} else {
				kind = "element"
			}
		}
		switch {
		case kind == "text" && cm.Text != "":
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
