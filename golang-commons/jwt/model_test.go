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
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

var signatureAlgorithms = []jose.SignatureAlgorithm{jose.HS256}
var joseTestKey = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

func TestNew_Success(t *testing.T) {
	tests := []struct {
		name       string
		claims     map[string]any
		expectedWT WebToken
	}{
		{
			name: "basic issuer claim",
			claims: map[string]any{
				"iss": "my-issuer",
			},
			expectedWT: WebToken{
				IssuerAttributes: IssuerAttributes{
					Issuer: "my-issuer",
				},
			},
		},
		{
			name: "with first_name and last_name",
			claims: map[string]any{
				"iss":        "test-issuer",
				"sub":        "test-subject",
				"first_name": "John",
				"last_name":  "Doe",
			},
			expectedWT: WebToken{
				IssuerAttributes: IssuerAttributes{
					Issuer:  "test-issuer",
					Subject: "test-subject",
				},
				UserAttributes: UserAttributes{
					FirstName: "John",
					LastName:  "Doe",
				},
			},
		},
		{
			name: "with given_name and family_name",
			claims: map[string]any{
				"iss":         "test-issuer",
				"sub":         "test-subject",
				"given_name":  "Jonathan",
				"family_name": "Smith",
			},
			expectedWT: WebToken{
				IssuerAttributes: IssuerAttributes{
					Issuer:  "test-issuer",
					Subject: "test-subject",
				},
				UserAttributes: UserAttributes{
					FirstName: "Jonathan",
					LastName:  "Smith",
				},
			},
		},
		{
			name: "prefer first_name/last_name over given_name/family_name",
			claims: map[string]any{
				"iss":         "test-issuer",
				"sub":         "test-subject",
				"first_name":  "John",
				"last_name":   "Doe",
				"given_name":  "Jonathan",
				"family_name": "Smith",
			},
			expectedWT: WebToken{
				IssuerAttributes: IssuerAttributes{
					Issuer:  "test-issuer",
					Subject: "test-subject",
				},
				UserAttributes: UserAttributes{
					FirstName: "John",
					LastName:  "Doe",
				},
			},
		},
		{
			name: "fallback to given_name/family_name when first_name/last_name are empty",
			claims: map[string]any{
				"iss":         "test-issuer",
				"sub":         "test-subject",
				"first_name":  "",
				"last_name":   "",
				"given_name":  "Jonathan",
				"family_name": "Smith",
			},
			expectedWT: WebToken{
				IssuerAttributes: IssuerAttributes{
					Issuer:  "test-issuer",
					Subject: "test-subject",
				},
				UserAttributes: UserAttributes{
					FirstName: "Jonathan",
					LastName:  "Smith",
				},
			},
		},
		{
			name: "partial fallback - first_name present, last_name empty",
			claims: map[string]any{
				"iss":         "test-issuer",
				"sub":         "test-subject",
				"first_name":  "John",
				"last_name":   "",
				"given_name":  "Jonathan",
				"family_name": "Smith",
			},
			expectedWT: WebToken{
				IssuerAttributes: IssuerAttributes{
					Issuer:  "test-issuer",
					Subject: "test-subject",
				},
				UserAttributes: UserAttributes{
					FirstName: "John",
					LastName:  "Smith",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(tt.claims))
			tokenString, err := token.SignedString(joseTestKey)
			assert.NoError(t, err)

			webToken, err := New(tokenString, signatureAlgorithms)
			assert.NoError(t, err)
			assert.NotNil(t, webToken)
			assert.Equal(t, tt.expectedWT.Issuer, webToken.Issuer)
			assert.Equal(t, tt.expectedWT.Subject, webToken.Subject)
			assert.Equal(t, tt.expectedWT.FirstName, webToken.FirstName)
			assert.Equal(t, tt.expectedWT.LastName, webToken.LastName)
		})
	}
}

func TestNew_Errors(t *testing.T) {
	tests := []struct {
		name          string
		tokenString   string
		setupToken    func() (string, error)
		expectedError string
	}{
		{
			name:          "invalid token string",
			tokenString:   "just a string",
			expectedError: "unable to parse id_token",
		},
		{
			name: "deserialization error with invalid payload",
			setupToken: func() (string, error) {
				// Create a valid JWT header and signature, but with a payload that is not valid JSON
				invalidPayload := "not-a-json-object"
				signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: joseTestKey}, nil)
				if err != nil {
					return "", err
				}

				object, err := signer.Sign([]byte(invalidPayload))
				if err != nil {
					return "", err
				}

				return object.CompactSerialize()
			},
			expectedError: "unable to deserialize claims",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tokenString string
			var err error

			if tt.setupToken != nil {
				tokenString, err = tt.setupToken()
				assert.NoError(t, err)
			} else {
				tokenString = tt.tokenString
			}

			_, err = New(tokenString, signatureAlgorithms)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}
