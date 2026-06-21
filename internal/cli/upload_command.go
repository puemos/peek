package cli

import (
	"bufio"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

// --- upload ---

func cmdUpload(args []string) error {
	var (
		file          string
		password      string
		passwordStdin bool
		name          string
		visibility    string
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
		case a == "--password-stdin":
			passwordStdin = true
		case a == "--name":
			if i+1 >= len(args) {
				return fmt.Errorf("--name requires a value")
			}
			name = args[i+1]
			i++
		case a == "--visibility":
			if i+1 >= len(args) {
				return fmt.Errorf("--visibility requires a value")
			}
			visibility = args[i+1]
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
		return fmt.Errorf("usage: peek upload <file.html> [--visibility public|password|private] [--password <pw>] [--name <name>]")
	}
	if visibility == "" {
		visibility = "password"
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
		return fmt.Errorf("--password is only valid with --visibility password")
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

	if name == "" {
		name = fileNameWithoutExt(file)
	}
	body, contentType := streamMultipartUpload(f, name, visibility, password)
	resp, err := c.req("POST", "/api/upload", body, contentType)
	if err != nil {
		_ = body.Close()
		return err
	}
	defer resp.Body.Close()
	var out struct {
		Slug       string `json:"slug"`
		URL        string `json:"url"`
		Visibility string `json:"visibility"`
	}
	if err := decodeResp(resp, &out); err != nil {
		return err
	}
	fmt.Printf("uploaded: %s\n", out.URL)
	fmt.Printf("slug:     %s\n", out.Slug)
	fmt.Printf("visibility: %s\n", out.Visibility)
	return nil
}

func streamMultipartUpload(file io.Reader, filename, visibility, password string) (io.ReadCloser, string) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		if err := writeMultipartUpload(mw, file, filename, visibility, password); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()
	return pr, mw.FormDataContentType()
}

func writeMultipartUpload(mw *multipart.Writer, file io.Reader, filename, visibility, password string) error {
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, file); err != nil {
		return err
	}
	if err := mw.WriteField("visibility", visibility); err != nil {
		return err
	}
	if password != "" {
		if err := mw.WriteField("password", password); err != nil {
			return err
		}
	}
	return mw.Close()
}

func validVisibility(visibility string) bool {
	return visibility == "public" || visibility == "password" || visibility == "private"
}

func fileNameWithoutExt(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		base = base[:len(base)-len(ext)]
	}
	if base == "" {
		base = "page"
	}
	return base
}
