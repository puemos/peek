package objectstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

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
