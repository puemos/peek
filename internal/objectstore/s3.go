package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type S3Storage struct {
	getSetting           func(string) string
	allowPrivateEndpoint bool
	client               *http.Client
}

func NewS3Storage(allowPrivateEndpoint bool, getSetting func(string) string) *S3Storage {
	return &S3Storage{
		allowPrivateEndpoint: allowPrivateEndpoint,
		getSetting:           getSetting,
		client:               newS3HTTPClient(allowPrivateEndpoint),
	}
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

func (s *S3Storage) objectKey(slug string) (string, error) {
	name, err := objectName(slug)
	if err != nil {
		return "", err
	}
	return "uploads/" + name, nil
}

func (s *S3Storage) objectURL(key string) string {
	return strings.TrimRight(s.endpoint(), "/") + "/" + s.bucket() + "/" + key
}

func (s *S3Storage) Save(ctx context.Context, slug string, data []byte) error {
	key, err := s.objectKey(slug)
	if err != nil {
		return err
	}
	return s.putObject(ctx, key, data)
}

func (s *S3Storage) Open(ctx context.Context, slug string) (io.ReadCloser, error) {
	key, err := s.objectKey(slug)
	if err != nil {
		return nil, err
	}
	resp, err := s.getObject(ctx, key)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (s *S3Storage) Delete(ctx context.Context, slug string) error {
	key, err := s.objectKey(slug)
	if err != nil {
		return err
	}
	return s.deleteObject(ctx, key)
}

func (s *S3Storage) httpClient() *http.Client {
	if s.client != nil {
		return s.client
	}
	return newS3HTTPClient(s.allowPrivateEndpoint)
}

func (s *S3Storage) putObject(ctx context.Context, key string, data []byte) error {
	url := s.objectURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/html")
	s.signRequest(req, key, data)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("s3 put %s: %s (%s)", key, resp.Status, readS3ErrorBody(resp.Body))
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
		body := readS3ErrorBody(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("s3 get %s: %s (%s)", key, resp.Status, body)
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
		return fmt.Errorf("s3 delete %s: %s (%s)", key, resp.Status, readS3ErrorBody(resp.Body))
	}
	return nil
}

func readS3ErrorBody(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, 1024))
	if err != nil {
		return "read response body: " + err.Error()
	}
	return strings.TrimSpace(string(data))
}
