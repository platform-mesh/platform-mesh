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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Default call-credential TTL per RFC 012.
const DefaultCallCredentialTTL = 60 * time.Second

// Default clock-skew tolerance for exp/iat checks per RFC 012.
const DefaultCallCredentialSkew = 30 * time.Second

// Sentinel errors returned by Verify / ParseUnverified. The user-side
// SDK maps these to the 403 reason codes defined in RFC 012
// (InvalidCredential / BodyMismatch).
var (
	ErrBadAlgorithm   = errors.New("access: call credential uses non-HS256 algorithm")
	ErrInvalidCred    = errors.New("access: invalid call credential")
	ErrCredExpired    = errors.New("access: call credential expired")
	ErrBodyMismatch   = errors.New("access: call credential body hash mismatch")
	ErrMissingTokenID = errors.New("access: call credential has no sub claim")
)

// CallClaims is the JWT claim set used for per-/access call credentials.
// sub carries the AccessIdentity's tokenID; body_sha256 binds the
// credential to the request body.
type CallClaims struct {
	jwt.RegisteredClaims
	BodySHA256 string `json:"body_sha256"`
}

// SignCallCredential mints a short-lived JWT HS256 call credential per
// RFC 012. tokenID is written to sub, the body hash to body_sha256, and
// a fresh 128-bit jti is generated per call.
func SignCallCredential(secret []byte, tokenID string, body []byte, ttl time.Duration) (string, error) {
	if tokenID == "" {
		return "", ErrMissingTokenID
	}
	if ttl <= 0 {
		ttl = DefaultCallCredentialTTL
	}

	jti, err := newJTI()
	if err != nil {
		return "", fmt.Errorf("access: generate jti: %w", err)
	}

	now := time.Now()
	claims := CallClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   tokenID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        jti,
		},
		BodySHA256: bodyHash(body),
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(secret)
}

// ParseUnverified decodes the JWT without verifying its signature and
// returns the claims. The user-side SDK needs sub (tokenID) to resolve
// which AccessIdentity's HMAC secret to verify against; call Verify
// afterwards with the resolved secret.
func ParseUnverified(token string) (CallClaims, error) {
	parser := jwt.NewParser()
	var claims CallClaims
	parsed, _, err := parser.ParseUnverified(token, &claims)
	if err != nil {
		return CallClaims{}, fmt.Errorf("%w: %v", ErrInvalidCred, err)
	}
	if parsed.Method == nil || parsed.Method.Alg() != jwt.SigningMethodHS256.Alg() {
		return CallClaims{}, ErrBadAlgorithm
	}
	if claims.Subject == "" {
		return CallClaims{}, ErrMissingTokenID
	}
	return claims, nil
}

// Verify validates a call credential against the given HMAC secret and
// request body. It enforces HS256, checks signature and exp/iat with
// the configured clock-skew tolerance, and recomputes the body hash.
// On success returns the parsed claims (containing jti and sub) so the
// caller can run the (tokenID, jti) replay check.
//
// Pass skew <= 0 to use DefaultCallCredentialSkew.
func Verify(secret []byte, token string, body []byte, skew time.Duration) (CallClaims, error) {
	if skew <= 0 {
		skew = DefaultCallCredentialSkew
	}

	var claims CallClaims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithLeeway(skew),
	)
	parsed, err := parser.ParseWithClaims(token, &claims, func(_ *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired), errors.Is(err, jwt.ErrTokenNotValidYet):
			return CallClaims{}, ErrCredExpired
		default:
			return CallClaims{}, fmt.Errorf("%w: %v", ErrInvalidCred, err)
		}
	}
	if !parsed.Valid {
		return CallClaims{}, ErrInvalidCred
	}
	if claims.Subject == "" {
		return CallClaims{}, ErrMissingTokenID
	}
	if claims.BodySHA256 != bodyHash(body) {
		return CallClaims{}, ErrBodyMismatch
	}
	return claims, nil
}

func newJTI() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func bodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
