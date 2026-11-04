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
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := []byte("hmac-secret")
	body := []byte(`{"kcpURL":"https://kcp.example"}`)

	tok, err := SignCallCredential(secret, "id-123", body, 0)
	if err != nil {
		t.Fatalf("SignCallCredential: %v", err)
	}

	claims, err := Verify(secret, tok, body, 0)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "id-123" {
		t.Errorf("Subject = %q, want id-123", claims.Subject)
	}
	if claims.ID == "" {
		t.Error("expected non-empty jti")
	}
	if claims.BodySHA256 == "" {
		t.Error("expected non-empty body_sha256")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	body := []byte("body")
	tok, err := SignCallCredential([]byte("right"), "id", body, 0)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := Verify([]byte("wrong"), tok, body, 0); !errors.Is(err, ErrInvalidCred) {
		t.Errorf("want ErrInvalidCred, got %v", err)
	}
}

func TestVerifyRejectsBodyTampering(t *testing.T) {
	secret := []byte("s")
	tok, err := SignCallCredential(secret, "id", []byte("original"), 0)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := Verify(secret, tok, []byte("tampered"), 0); !errors.Is(err, ErrBodyMismatch) {
		t.Errorf("want ErrBodyMismatch, got %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	secret := []byte("s")
	body := []byte("b")

	// Craft an already-expired token by hand (TTL < -skew so leeway can't save it).
	claims := CallClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "id",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			ID:        "jti-1",
		},
		BodySHA256: bodyHash(body),
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("manual sign: %v", err)
	}
	if _, err := Verify(secret, raw, body, 0); !errors.Is(err, ErrCredExpired) {
		t.Errorf("want ErrCredExpired, got %v", err)
	}
}

func TestVerifyRejectsNonHS256(t *testing.T) {
	// Craft a `none`-algorithm JWT; v5's UnsafeAllowNoneSignatureType lets
	// us produce one for the test. Verify must refuse it.
	claims := CallClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "id"}}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	raw, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("craft none-jwt: %v", err)
	}
	if _, err := Verify([]byte("s"), raw, nil, 0); !errors.Is(err, ErrBadAlgorithm) && !errors.Is(err, ErrInvalidCred) {
		t.Errorf("want ErrBadAlgorithm or ErrInvalidCred, got %v", err)
	}
}

func TestParseUnverifiedExposesSub(t *testing.T) {
	secret := []byte("s")
	tok, err := SignCallCredential(secret, "id-xyz", []byte("b"), 0)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	claims, err := ParseUnverified(tok)
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	if claims.Subject != "id-xyz" {
		t.Errorf("Subject = %q, want id-xyz", claims.Subject)
	}
}

func TestSignGeneratesFreshJTI(t *testing.T) {
	secret := []byte("s")
	body := []byte("b")
	t1, err := SignCallCredential(secret, "id", body, 0)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := SignCallCredential(secret, "id", body, 0)
	if err != nil {
		t.Fatal(err)
	}
	c1, err := Verify(secret, t1, body, 0)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := Verify(secret, t2, body, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c1.ID == c2.ID {
		t.Error("jti collided across two issuances")
	}
}
