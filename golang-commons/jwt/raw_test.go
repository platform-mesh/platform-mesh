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

	"github.com/stretchr/testify/assert"
)

func TestRawWebToken_GetAudiences(t *testing.T) {
	tests := []struct {
		name           string
		rawAudiences   any
		expectedResult []string
		expectedLen    int
	}{
		{
			name: "slice of interfaces with mixed types",
			rawAudiences: []any{
				"audience1",
				"audience2",
				1812, // wrong audience type
			},
			expectedResult: []string{"audience1", "audience2"},
			expectedLen:    2,
		},
		{
			name:           "single string audience",
			rawAudiences:   "audience1",
			expectedResult: []string{"audience1"},
			expectedLen:    1,
		},
		{
			name:           "nil audience",
			rawAudiences:   nil,
			expectedResult: nil,
			expectedLen:    0,
		},
		{
			name:           "empty slice",
			rawAudiences:   []any{},
			expectedResult: nil,
			expectedLen:    0,
		},
		{
			name: "slice with only invalid types",
			rawAudiences: []any{
				1812,
				true,
				42.5,
			},
			expectedResult: nil,
			expectedLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := rawWebToken{
				rawClaims: rawClaims{
					RawAudiences: tt.rawAudiences,
				},
			}

			parsedAudiences := token.getAudiences()

			assert.Equal(t, tt.expectedLen, len(parsedAudiences))
			for _, expected := range tt.expectedResult {
				assert.Contains(t, parsedAudiences, expected)
			}

			// Ensure invalid types are not included
			if tt.name == "slice of interfaces with mixed types" {
				assert.NotContains(t, parsedAudiences, 1812)
			}
		})
	}
}

func TestRawWebToken_GetFirstName(t *testing.T) {
	tests := []struct {
		name         string
		firstName    string
		rawGivenName string
		expected     string
	}{
		{
			name:         "prefer first_name over given_name",
			firstName:    "John",
			rawGivenName: "Jonathan",
			expected:     "John",
		},
		{
			name:         "fallback to given_name when first_name is empty",
			firstName:    "",
			rawGivenName: "Jonathan",
			expected:     "Jonathan",
		},
		{
			name:         "both empty",
			firstName:    "",
			rawGivenName: "",
			expected:     "",
		},
		{
			name:         "first_name with whitespace only",
			firstName:    "   ",
			rawGivenName: "Jonathan",
			expected:     "Jonathan",
		},
		{
			name:         "first_name with leading/trailing whitespace",
			firstName:    "  John  ",
			rawGivenName: "Jonathan",
			expected:     "John",
		},
		{
			name:         "only first_name provided",
			firstName:    "John",
			rawGivenName: "",
			expected:     "John",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := rawWebToken{
				UserAttributes: UserAttributes{
					FirstName: tt.firstName,
				},
				rawClaims: rawClaims{
					RawGivenName: tt.rawGivenName,
				},
			}

			firstName := token.getFirstName()
			assert.Equal(t, tt.expected, firstName)
		})
	}
}

func TestRawWebToken_GetLastName(t *testing.T) {
	tests := []struct {
		name          string
		lastName      string
		rawFamilyName string
		expected      string
	}{
		{
			name:          "prefer last_name over family_name",
			lastName:      "Doe",
			rawFamilyName: "Smith",
			expected:      "Doe",
		},
		{
			name:          "fallback to family_name when last_name is empty",
			lastName:      "",
			rawFamilyName: "Smith",
			expected:      "Smith",
		},
		{
			name:          "both empty",
			lastName:      "",
			rawFamilyName: "",
			expected:      "",
		},
		{
			name:          "last_name with whitespace only",
			lastName:      "   ",
			rawFamilyName: "Smith",
			expected:      "Smith",
		},
		{
			name:          "last_name with leading/trailing whitespace",
			lastName:      "  Doe  ",
			rawFamilyName: "Smith",
			expected:      "Doe",
		},
		{
			name:          "only last_name provided",
			lastName:      "Doe",
			rawFamilyName: "",
			expected:      "Doe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := rawWebToken{
				UserAttributes: UserAttributes{
					LastName: tt.lastName,
				},
				rawClaims: rawClaims{
					RawFamilyName: tt.rawFamilyName,
				},
			}

			lastName := token.getLastName()
			assert.Equal(t, tt.expected, lastName)
		})
	}
}
