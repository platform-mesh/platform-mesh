/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// IsBlockedHost reports whether host must not be used for outbound OIDC or discovery fetches.
func IsBlockedHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "" || host == "localhost" || host == "0.0.0.0" || host == "::" {
		return true
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	ip := parseHostIP(host)
	if ip == nil {
		return false
	}
	return isBlockedIP(ip)
}

func parseHostIP(host string) net.IP {
	if strings.HasPrefix(host, "::ffff:") {
		return net.ParseIP(strings.TrimPrefix(host, "::ffff:"))
	}
	return net.ParseIP(host)
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return true
	}
	return false
}

// ValidateHTTPSURL ensures raw is an https URL whose host is not blocked.
func ValidateHTTPSURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid URL %q", raw)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("URL %q must use https", raw)
	}
	if IsBlockedHost(parsed.Hostname()) {
		return fmt.Errorf("URL %q must not target internal or private addresses", raw)
	}
	return nil
}

// ValidateDiscoveryURL validates an OIDC discovery document URL.
func ValidateDiscoveryURL(raw string) error {
	if err := ValidateHTTPSURL(raw); err != nil {
		return fmt.Errorf("discoveryUrl: %w", err)
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("discoveryUrl: invalid URL")
	}
	if IsBlockedHost(parsed.Hostname()) {
		return fmt.Errorf("discoveryUrl must not target internal or private addresses")
	}
	return nil
}

// ValidateOutboundHost resolves hostnames and rejects blocked addresses.
func ValidateOutboundHost(ctx context.Context, host string) error {
	if IsBlockedHost(host) {
		return fmt.Errorf("host %q must not target internal or private addresses", host)
	}
	if parseHostIP(host) != nil {
		return nil
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolving host %q: %w", host, err)
	}
	for _, ipAddr := range ips {
		if isBlockedIP(ipAddr.IP) {
			return fmt.Errorf("host %q resolves to blocked address %s", host, ipAddr.IP)
		}
	}
	return nil
}

// ValidateOIDCEndpointURLs validates issuer and endpoint URLs from discovery or manual config.
func ValidateOIDCEndpointURLs(urls ...string) error {
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if err := ValidateHTTPSURL(raw); err != nil {
			return err
		}
	}
	return nil
}

// NewSafeDiscoveryHTTPClient returns an HTTP client that blocks redirects to unsafe hosts.
func NewSafeDiscoveryHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if err := ValidateHTTPSURL(req.URL.String()); err != nil {
				return err
			}
			return ValidateOutboundHost(req.Context(), req.URL.Hostname())
		},
	}
}
