package objectstore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ValidateS3Endpoint checks that the endpoint URL is safe by default: HTTPS
// only, not pointing at private/link-local/metadata IPs. Private/dev endpoints
// such as MinIO require an explicit allowPrivateEndpoint opt-in.
func ValidateS3Endpoint(endpoint string, allowPrivateEndpoint bool) error {
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
