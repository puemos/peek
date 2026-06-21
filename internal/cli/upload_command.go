package cli

import (
	"bufio"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
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
		return fmt.Errorf("usage: peek upload <file.html> [--password <pw>] [--name <name>]")
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
		if name == "" {
			name = fileNameWithoutExt(file)
		}
		body, contentType := streamMultipartUpload(f, name, password)
		resp, err = c.req("POST", "/api/upload", body, contentType)
		if err != nil {
			_ = body.Close()
			return err
		}
	} else {
		if name == "" {
			name = fileNameWithoutExt(file)
		}
		resp, err = c.req("POST", "/api/upload?filename="+url.QueryEscape(name), f, "text/html")
		if err != nil {
			return err
		}
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

func streamMultipartUpload(file io.Reader, filename, password string) (io.ReadCloser, string) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		if err := writeMultipartUpload(mw, file, filename, password); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()
	return pr, mw.FormDataContentType()
}

func writeMultipartUpload(mw *multipart.Writer, file io.Reader, filename, password string) error {
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, file); err != nil {
		return err
	}
	if err := mw.WriteField("password", password); err != nil {
		return err
	}
	return mw.Close()
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
