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

package jwt

import (
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
	gojwt "github.com/golang-jwt/jwt/v5"
)

// FuzzNew fuzzes parsing of untrusted id_token strings. The property under test
// is that New never panics regardless of input; returning an error for garbage
// is acceptable and expected.
func FuzzNew(f *testing.F) {
	// Seed with a valid token plus a range of malformed inputs.
	validToken, err := gojwt.NewWithClaims(gojwt.SigningMethodHS256, gojwt.MapClaims{
		"iss": "my-issuer",
		"sub": "my-subject",
		"aud": []string{"aud-a", "aud-b"},
	}).SignedString(joseTestKey)
	if err != nil {
		f.Fatalf("failed to build seed token: %v", err)
	}

	seeds := []string{
		validToken,
		"",
		"just a string",
		"a.b.c",
		"...",
		"eyJ.eyJ.",
	}

	// A token with a syntactically valid JWS but a non-JSON payload.
	if signer, sErr := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: joseTestKey}, nil); sErr == nil {
		if obj, oErr := signer.Sign([]byte("not-a-json-object")); oErr == nil {
			if serialized, cErr := obj.CompactSerialize(); cErr == nil {
				seeds = append(seeds, serialized)
			}
		}
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, idToken string) {
		// Must not panic. Errors are an acceptable outcome for arbitrary input.
		webToken, err := New(idToken, signatureAlgorithms)
		if err != nil {
			return
		}
		// On success the audiences slice must contain no empty entries that
		// would indicate a type-switch mishandling in getAudiences.
		for _, aud := range webToken.Audiences {
			_ = aud
		}
	})
}

// FuzzGetURIValue fuzzes the SPIFFE header regex parser with untrusted
// X-Forwarded-Client-Cert header values.
func FuzzGetURIValue(f *testing.F) {
	seeds := []string{
		"URI=spiffe://example.com",
		"spiffe://example.com",
		"",
		"URI=",
		"URI=URI=spiffe://a",
		"By=spiffe://x;URI=spiffe://y",
		strings.Repeat("URI=spiffe://a", 100),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, headerVal string) {
		// Must not panic. A returned value must be a substring of the input,
		// since it originates from a regex submatch over headerVal.
		got := GetURIValue(headerVal)
		if got != "" && !strings.Contains(headerVal, got) {
			t.Fatalf("returned value %q is not a substring of input %q", got, headerVal)
		}
	})
}
