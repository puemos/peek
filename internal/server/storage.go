package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Storage interface {
	Save(ctx context.Context, slug string, data []byte) error
	Open(ctx context.Context, slug string) (io.ReadCloser, error)
	Delete(ctx context.Context, slug string) error
}

type FileStorage struct {
	Dir string
}

func (fs *FileStorage) objectPath(slug string) string {
	return filepath.Join(fs.Dir, slug+".html")
}

func (fs *FileStorage) Save(_ context.Context, slug string, data []byte) error {
	if err := os.MkdirAll(fs.Dir, 0o755); err != nil {
		return err
	}
	path := fs.objectPath(slug)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (fs *FileStorage) Open(_ context.Context, slug string) (io.ReadCloser, error) {
	return os.Open(fs.objectPath(slug))
}

func (fs *FileStorage) Delete(_ context.Context, slug string) error {
	err := os.Remove(fs.objectPath(slug))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

type S3Storage struct {
	getSetting func(string) string
	client     *http.Client
}

func NewS3Storage(getSetting func(string) string) *S3Storage {
	return &S3Storage{getSetting: getSetting}
}

func (s *S3Storage) endpoint() string { return s.getSetting("s3_endpoint") }
func (s *S3Storage) bucket() string   { return s.getSetting("s3_bucket") }
func (s *S3Storage) region() string {
	if r := s.getSetting("s3_region"); r != "" {
		return r
	}
	return "us-east-1"
}
func (s *S3Storage) accessKey() string { return s.getSetting("s3_access_key") }
func (s *S3Storage) secretKey() string { return s.getSetting("s3_secret_key") }

func (s *S3Storage) objectKey(slug string) string {
	return "uploads/" + slug + ".html"
}

func (s *S3Storage) objectURL(key string) string {
	return strings.TrimRight(s.endpoint(), "/") + "/" + s.bucket() + "/" + key
}

func (s *S3Storage) Save(ctx context.Context, slug string, data []byte) error {
	key := s.objectKey(slug)
	return s.putObject(ctx, key, data)
}

func (s *S3Storage) Open(ctx context.Context, slug string) (io.ReadCloser, error) {
	key := s.objectKey(slug)
	resp, err := s.getObject(ctx, key)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (s *S3Storage) Delete(ctx context.Context, slug string) error {
	key := s.objectKey(slug)
	return s.deleteObject(ctx, key)
}

func (s *S3Storage) httpClient() *http.Client {
	if s.client != nil {
		return s.client
	}
	s.client = &http.Client{Timeout: 30 * time.Second}
	return s.client
}

func (s *S3Storage) putObject(ctx context.Context, key string, data []byte) error {
	url := s.objectURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/html")
	s.signRequest(req, key, strings.NewReader(string(data)))
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("s3 put %s: %s (%s)", key, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *S3Storage) getObject(ctx context.Context, key string) (*http.Response, error) {
	url := s.objectURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	s.signRequest(req, key, nil)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, os.ErrNotExist
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		return nil, fmt.Errorf("s3 get %s: %s (%s)", key, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func (s *S3Storage) deleteObject(ctx context.Context, key string) error {
	url := s.objectURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	s.signRequest(req, key, nil)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("s3 delete %s: %s (%s)", key, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *S3Storage) signRequest(req *http.Request, key string, body io.Reader) {
	t := time.Now().UTC()
	dateShort := t.Format("20060102")
	dateLong := t.Format("20060102T150405Z")
	service := "s3"
	host := req.URL.Host

	req.Header.Set("Host", host)
	req.Header.Set("X-Amz-Date", dateLong)
	req.Header.Set("X-Amz-Content-SHA256", s.bodyHash(body))

	canonicalHeaders, signedHeaders := s.canonicalHeaders(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		req.Header.Get("X-Amz-Content-SHA256"),
	}, "\n")

	credentialScope := dateShort + "/" + s.region() + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		dateLong,
		credentialScope,
		hashString(canonicalRequest),
	}, "\n")

	signingKey := s.deriveSigningKey(dateShort)
	signature := hex.EncodeToString(s3hmacSha256(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey(), credentialScope, signedHeaders, signature,
	))
}

func (s *S3Storage) canonicalHeaders(req *http.Request) (string, string) {
	type kv struct{ k, v string }
	var pairs []kv
	for k, vals := range req.Header {
		lk := strings.ToLower(k)
		if lk == "authorization" {
			continue
		}
		pairs = append(pairs, kv{lk, strings.TrimSpace(vals[0])})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
	var canonLines, names []string
	for _, p := range pairs {
		canonLines = append(canonLines, p.k+":"+p.v+"\n")
		names = append(names, p.k)
	}
	return strings.Join(canonLines, ""), strings.Join(names, ";")
}

func (s *S3Storage) bodyHash(body io.Reader) string {
	if body == nil {
		return hashString("")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		log.Printf("s3 body hash error: %v", err)
		return hashString("")
	}
	return hashString(string(data))
}

func (s *S3Storage) deriveSigningKey(dateShort string) []byte {
	kDate := s3hmacSha256([]byte("AWS4"+s.secretKey()), []byte(dateShort))
	kRegion := s3hmacSha256(kDate, []byte(s.region()))
	kService := s3hmacSha256(kRegion, []byte("s3"))
	return s3hmacSha256(kService, []byte("aws4_request"))
}

func s3hmacSha256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
