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

package access

import (
	"encoding/base64"
	"errors"
	"strings"
)

// TokenPrefix is the version prefix carried as the first segment of
// every access token. Clients split on '.' and verify this value.
const TokenPrefix = "pma1"

// ErrInvalidToken is returned by ParseToken for any malformed access
// token: missing prefix, wrong segment count, or empty tokenID / secret.
var ErrInvalidToken = errors.New("access: invalid access token")

// Token is the parsed form of an access token.
//
// TokenID is the public component (safe to log and surface in CRD
// status). Secret is the HMAC key material used to sign per-/access
// call credentials.
type Token struct {
	// TokenID identifies which AccessIdentity this token belongs to.
	// Not secret.
	TokenID string

	// Secret is the HMAC key material. Treat as sensitive.
	Secret []byte
}

// ParseToken parses an access token in the pma1.<tokenID>.<secret>
// form. Returns ErrInvalidToken on any shape error.
func ParseToken(s string) (Token, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Token{}, ErrInvalidToken
	}
	if parts[0] != TokenPrefix {
		return Token{}, ErrInvalidToken
	}
	if parts[1] == "" || parts[2] == "" {
		return Token{}, ErrInvalidToken
	}

	secret, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Token{}, ErrInvalidToken
	}

	return Token{
		TokenID: parts[1],
		Secret:  secret,
	}, nil
}

// String serializes a Token to the pma1.<tokenID>.<secret> access-token
// form. Secret is base64url-encoded without padding; TokenID is inlined
// as-is.
func (t Token) String() string {
	return strings.Join([]string{
		TokenPrefix,
		t.TokenID,
		base64.RawURLEncoding.EncodeToString(t.Secret),
	}, ".")
}
