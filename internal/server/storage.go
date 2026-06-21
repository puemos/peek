package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
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
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
	getSetting           func(string) string
	allowPrivateEndpoint bool
	client               *http.Client
}

func NewS3Storage(secret string, allowPrivateEndpoint bool, getSetting func(string) string) *S3Storage {
	return &S3Storage{allowPrivateEndpoint: allowPrivateEndpoint, getSetting: func(key string) string {
		v := getSetting(key)
		if v == "" {
			return ""
		}
		if secretSettingKeys[key] {
			dec, err := decryptSecret(secret, v)
			if err != nil {
				return ""
			}
			return dec
		}
		return v
	}}
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

// validateS3Endpoint checks that the endpoint URL is safe by default: HTTPS
// only, not pointing at private/link-local/metadata IPs. Private/dev endpoints
// such as MinIO require an explicit allowPrivateEndpoint opt-in.
func validateS3Endpoint(endpoint string, allowPrivateEndpoint bool) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("endpoint missing host")
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("S3 endpoint must use http or https scheme")
	}
	if u.Scheme == "http" && !allowPrivateEndpoint {
		return errors.New("S3 endpoint must use HTTPS unless private endpoints are explicitly allowed")
	}
	if allowPrivateEndpoint {
		return nil
	}

	// Resolve and check for private/metadata IPs (skip for hostnames that
	// don't resolve here; the S3 transport validates the dialed endpoint again).
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrMetadataIP(ip) {
			return fmt.Errorf("S3 endpoint IP %s is private or link-local", ip)
		}
	} else {
		ips, err := net.LookupIP(host)
		if err == nil {
			for _, ip := range ips {
				if isPrivateOrMetadataIP(ip) {
					return fmt.Errorf("S3 endpoint %s resolves to private IP %s", host, ip)
				}
			}
		}
	}
	return nil
}

// isPrivateOrMetadataIP returns true for RFC 1918, link-local, loopback (non-
// localhost check), and cloud metadata endpoints.
func isPrivateOrMetadataIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Cloud metadata endpoints.
	cloudMetadata := []string{"169.254.169.254", "fd00:ec2::254"}
	for _, addr := range cloudMetadata {
		if mdIP := net.ParseIP(addr); mdIP != nil && ip.Equal(mdIP) {
			return true
		}
	}
	return false
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
	s.client = newS3HTTPClient(s.allowPrivateEndpoint)
	return s.client
}

func newS3HTTPClient(allowPrivateEndpoint bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = safeS3DialContext(allowPrivateEndpoint)
	return &http.Client{Timeout: 30 * time.Second, Transport: transport}
}

func safeS3DialContext(allowPrivateEndpoint bool) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("S3 endpoint %s resolved to no IPs", host)
		}
		if !allowPrivateEndpoint {
			for _, ip := range ips {
				if isPrivateOrMetadataIP(ip.IP) {
					return nil, fmt.Errorf("S3 endpoint %s resolved to private or link-local IP %s", host, ip.IP)
				}
			}
		}
		for _, ip := range ips {
			if network == "tcp4" && ip.IP.To4() == nil {
				continue
			}
			if network == "tcp6" && ip.IP.To4() != nil {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		}
		return nil, fmt.Errorf("S3 endpoint %s has no address for %s", host, network)
	}
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
		slog.Error("s3 body hash", "err", err)
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
