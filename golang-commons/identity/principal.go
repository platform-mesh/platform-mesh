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

// Package identity defines canonical OIDC principals. It does not activate
// their use by authentication or authorization consumers.
package identity

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	usernamePrefix = "oidc:v1:"

	// MaxUsernameBytes is a conservative operational bound on the UTF-8 encoded
	// username; deployments may have stricter request or storage limits.
	MaxUsernameBytes = 1024

	// UsernameExpression is the KCP CEL mapping for validated OIDC claims.
	UsernameExpression = `"oidc:v1:" + string(size(claims.iss)) + ":" + claims.iss + claims.sub`

	// ClaimValidationExpression checks claim types, controls and username size.
	// The configured issuer must first pass Validate and KCP must verify exact
	// issuer equality and UTF-8 token decoding independently.
	ClaimValidationExpression = `has(claims.iss) && has(claims.sub) && ` +
		`type(claims.iss) == string && type(claims.sub) == string && ` +
		`claims.iss != "" && claims.sub != "" && ` +
		`!claims.iss.matches("\\p{Cc}") && !claims.sub.matches("\\p{Cc}") && ` +
		`size(bytes(` + UsernameExpression + `)) <= 1024`
)

type Principal struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// Validate checks the principal and the size of its canonical username.
func (p Principal) Validate() error {
	if !utf8.ValidString(p.Issuer) || hasControl(p.Issuer) {
		return fmt.Errorf("issuer must be valid UTF-8 without control characters")
	}
	u, err := url.Parse(p.Issuer)
	if err != nil {
		return fmt.Errorf("invalid issuer URL: %w", err)
	}
	if u.Scheme != "https" || u.Opaque != "" || u.Host == "" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(p.Issuer, "?#") {
		return fmt.Errorf("issuer must be an absolute HTTPS URL with host and without userinfo, query, or fragment")
	}
	if p.Subject == "" || !utf8.ValidString(p.Subject) || hasControl(p.Subject) {
		return fmt.Errorf("subject must be nonempty valid UTF-8 without control characters")
	}
	length := len(usernamePrefix) + len(strconv.Itoa(utf8.RuneCountInString(p.Issuer))) + 1 + len(p.Issuer) + len(p.Subject)
	if length > MaxUsernameBytes {
		return fmt.Errorf("canonical username exceeds %d bytes", MaxUsernameBytes)
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// Username returns the length-prefixed, lossless OIDC identity.
func (p Principal) Username() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	// Count code points, as CEL size(string) does, rather than UTF-8 bytes.
	return usernamePrefix + strconv.Itoa(utf8.RuneCountInString(p.Issuer)) + ":" + p.Issuer + p.Subject, nil
}

// ParseUsername parses only canonical and valid OIDC usernames.
func ParseUsername(username string) (Principal, error) {
	if len(username) > MaxUsernameBytes || !utf8.ValidString(username) || !strings.HasPrefix(username, usernamePrefix) {
		return Principal{}, fmt.Errorf("invalid canonical username")
	}
	rest := strings.TrimPrefix(username, usernamePrefix)
	digits, value, found := strings.Cut(rest, ":")
	if !found || digits == "" || digits[0] == '0' {
		return Principal{}, fmt.Errorf("invalid issuer length")
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return Principal{}, fmt.Errorf("invalid issuer length")
		}
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n > utf8.RuneCountInString(value) {
		return Principal{}, fmt.Errorf("invalid issuer length")
	}
	// The length separates the fields even when either contains colons.
	split := 0
	for range n {
		_, size := utf8.DecodeRuneInString(value[split:])
		split += size
	}
	p := Principal{Issuer: value[:split], Subject: value[split:]}
	if err := p.Validate(); err != nil {
		return Principal{}, fmt.Errorf("invalid canonical username: %w", err)
	}
	return p, nil
}

// FGAUser returns a fixed-length, domain-separated OpenFGA user identifier.
func (p Principal) FGAUser() (string, error) {
	username, err := p.Username()
	if err != nil {
		return "", err
	}
	// Hash the versioned, unambiguous username instead of replacing delimiters.
	digest := sha256.Sum256([]byte(username))
	return fmt.Sprintf("user:oidc_v1_%x", digest), nil
}
