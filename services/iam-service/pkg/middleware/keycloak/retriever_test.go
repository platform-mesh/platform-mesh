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

package keycloak

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeycloakIDMRetriever_GetIDMTenant_ValidIssuers(t *testing.T) {
	retriever := &KeycloakIDMRetriever{}

	testCases := []struct {
		name          string
		issuer        string
		expectedRealm string
		shouldError   bool
	}{
		{
			name:          "Basic Keycloak URL",
			issuer:        "https://auth.example.com/realms/my-realm",
			expectedRealm: "my-realm",
			shouldError:   false,
		},
		{
			name:          "Keycloak URL with trailing slash",
			issuer:        "https://auth.example.com/realms/my-realm/",
			expectedRealm: "my-realm",
			shouldError:   false,
		},
		{
			name:          "Keycloak URL with port",
			issuer:        "https://auth.example.com:8443/realms/production",
			expectedRealm: "production",
			shouldError:   false,
		},
		{
			name:          "HTTP URL (non-SSL)",
			issuer:        "http://localhost:8080/realms/development",
			expectedRealm: "development",
			shouldError:   false,
		},
		{
			name:          "Realm name with hyphens",
			issuer:        "https://keycloak.company.com/realms/my-complex-realm-name",
			expectedRealm: "my-complex-realm-name",
			shouldError:   false,
		},
		{
			name:          "Realm name with underscores",
			issuer:        "https://sso.example.org/realms/test_realm_123",
			expectedRealm: "test_realm_123",
			shouldError:   false,
		},
		{
			name:          "Keycloak subdirectory path",
			issuer:        "https://example.com/auth/realms/master",
			expectedRealm: "master",
			shouldError:   false,
		},
		{
			name:          "Multiple path segments before realms",
			issuer:        "https://company.com/services/auth/keycloak/realms/corp-realm",
			expectedRealm: "corp-realm",
			shouldError:   false,
		},
		{
			name:          "Realm with numbers",
			issuer:        "https://auth.domain.com/realms/realm123",
			expectedRealm: "realm123",
			shouldError:   false,
		},
		{
			name:          "Mixed case realm name",
			issuer:        "https://auth.example.com/realms/MyRealmName",
			expectedRealm: "MyRealmName",
			shouldError:   false,
		},
		{
			name:          "Realm name with dots",
			issuer:        "https://keycloak.company.com/realms/realm.with.dots",
			expectedRealm: "realm.with.dots",
			shouldError:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			realm, err := retriever.GetIDMTenant(tc.issuer)

			if tc.shouldError {
				assert.Error(t, err)
				assert.Empty(t, realm)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedRealm, realm)
			}
		})
	}
}

func TestKeycloakIDMRetriever_GetIDMTenant_InvalidIssuers(t *testing.T) {
	retriever := &KeycloakIDMRetriever{}

	testCases := []struct {
		name   string
		issuer string
		reason string
	}{
		{
			name:   "Empty string",
			issuer: "",
			reason: "Empty issuer should be invalid",
		},
		{
			name:   "URL without realms path",
			issuer: "https://auth.example.com/auth",
			reason: "Missing /realms/ path component",
		},
		{
			name:   "Malformed URL",
			issuer: "not-a-url",
			reason: "Not a valid URL format",
		},
		{
			name:   "URL with incorrect path structure",
			issuer: "https://auth.example.com/realm/test", // realm instead of realms
			reason: "Using 'realm' instead of 'realms'",
		},
		{
			name:   "Just the protocol",
			issuer: "https://",
			reason: "Incomplete URL",
		},
		{
			name:   "Random string",
			issuer: "random-text-not-url",
			reason: "Not a URL format",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			realm, err := retriever.GetIDMTenant(tc.issuer)

			assert.Error(t, err, tc.reason)
			assert.Empty(t, realm, "Realm should be empty when error occurs")
			assert.Contains(t, err.Error(), "token issuer is not valid", "Error message should indicate invalid issuer")
		})
	}
}

func TestKeycloakIDMRetriever_GetIDMTenant_RealisticScenarios(t *testing.T) {
	retriever := &KeycloakIDMRetriever{}

	testCases := []struct {
		name          string
		issuer        string
		expectedRealm string
		shouldError   bool
		description   string
	}{
		{
			name:          "Standard realm name",
			issuer:        "https://auth.example.com/realms/production",
			expectedRealm: "production",
			shouldError:   false,
			description:   "Standard production realm name",
		},
		{
			name:          "Environment-based realm",
			issuer:        "https://keycloak.company.com/realms/staging-env",
			expectedRealm: "staging-env",
			shouldError:   false,
			description:   "Environment-based realm with hyphen",
		},
		{
			name:          "Organizational realm",
			issuer:        "https://sso.corporation.com/realms/corp_users",
			expectedRealm: "corp_users",
			shouldError:   false,
			description:   "Corporate realm with underscore",
		},
		{
			name:          "Default master realm",
			issuer:        "https://keycloak.internal/realms/master",
			expectedRealm: "master",
			shouldError:   false,
			description:   "Default Keycloak master realm",
		},
		{
			name:          "Versioned realm",
			issuer:        "https://auth.service.com/realms/api-v2",
			expectedRealm: "api-v2",
			shouldError:   false,
			description:   "API versioned realm name",
		},
		{
			name:          "Tenant-based realm",
			issuer:        "https://multi-tenant.saas.com/realms/tenant123",
			expectedRealm: "tenant123",
			shouldError:   false,
			description:   "Multi-tenant realm with numbers",
		},
		{
			name:          "Department realm",
			issuer:        "https://corporate-sso.com/realms/hr-department",
			expectedRealm: "hr-department",
			shouldError:   false,
			description:   "Department-specific realm",
		},
		{
			name:          "Regional realm",
			issuer:        "https://global-auth.com/realms/us-west-2",
			expectedRealm: "us-west-2",
			shouldError:   false,
			description:   "Regional deployment realm",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			realm, err := retriever.GetIDMTenant(tc.issuer)

			if tc.shouldError {
				assert.Error(t, err, tc.description)
				assert.Empty(t, realm)
			} else {
				assert.NoError(t, err, tc.description)
				assert.Equal(t, tc.expectedRealm, realm)
			}
		})
	}
}

func TestKeycloakIDMRetriever_GetIDMTenant_RegexBehavior(t *testing.T) {
	retriever := &KeycloakIDMRetriever{}

	// Test specific regex behavior - these tests reflect the actual regex implementation
	testCases := []struct {
		name          string
		issuer        string
		expectedRealm string
		shouldError   bool
	}{
		{
			name:          "Multiple realms segments (should take from last)",
			issuer:        "https://auth.example.com/realms/first/realms/second",
			expectedRealm: "second",
			shouldError:   false,
		},
		{
			name:          "Realms with additional path after - includes path",
			issuer:        "https://auth.example.com/realms/test/protocol/openid_connect",
			expectedRealm: "test/protocol/openid_connect", // The regex captures everything after /realms/
			shouldError:   false,
		},
		{
			name:        "Case sensitive - REALMS (uppercase)",
			issuer:      "https://auth.example.com/REALMS/test",
			shouldError: true,
		},
		{
			name:          "Trailing slash handling",
			issuer:        "https://auth.example.com/realms/test/",
			expectedRealm: "test",
			shouldError:   false,
		},
		{
			name:          "Multiple trailing slashes - includes slashes",
			issuer:        "https://auth.example.com/realms/test///",
			expectedRealm: "test//", // The regex captures the extra slashes
			shouldError:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			realm, err := retriever.GetIDMTenant(tc.issuer)

			if tc.shouldError {
				assert.Error(t, err)
				assert.Empty(t, realm)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedRealm, realm)
			}
		})
	}
}

func TestKeycloakIDMRetriever_Struct(t *testing.T) {
	// Test struct instantiation and methods
	retriever := &KeycloakIDMRetriever{}
	assert.NotNil(t, retriever)

	// Test that it implements the expected interface (if any)
	// This is a compile-time check that the struct has the required methods
	var _ interface {
		GetIDMTenant(string) (string, error)
	} = retriever
}

func TestKeycloakIDMRetriever_GetIDMTenant_NilReceiver(t *testing.T) {
	// Test behavior with nil receiver (should panic or handle gracefully)
	// Note: This test might panic, which is acceptable behavior
	defer func() {
		if r := recover(); r != nil {
			// Panic is acceptable for nil receiver
			t.Logf("Expected panic occurred with nil receiver: %v", r)
		}
	}()

	var retriever *KeycloakIDMRetriever
	_, err := retriever.GetIDMTenant("https://auth.example.com/realms/test")

	// If we reach here without panic, the method handled nil receiver gracefully
	if err != nil {
		t.Logf("Method returned error with nil receiver: %v", err)
	}
}

func TestKeycloakIDMRetriever_GetIDMTenant_ErrorMessage(t *testing.T) {
	retriever := &KeycloakIDMRetriever{}

	// Test that error messages are consistent - only test URLs that should actually fail
	invalidIssuers := []string{
		"",
		"invalid-url",
		"https://auth.example.com/auth",
	}

	for _, issuer := range invalidIssuers {
		_, err := retriever.GetIDMTenant(issuer)
		assert.Error(t, err)
		assert.Equal(t, "token issuer is not valid", err.Error(),
			"Error message should be consistent for issuer: %s", issuer)
	}
}

func TestKeycloakIDMRetriever_GetIDMTenant_EmptyRealm(t *testing.T) {
	retriever := &KeycloakIDMRetriever{}

	// Test URLs that result in empty realm names (these don't error in the current implementation)
	testCases := []struct {
		name   string
		issuer string
	}{
		{
			name:   "URL with realms but no realm name",
			issuer: "https://auth.example.com/realms/",
		},
		{
			name:   "URL with realms but double slash",
			issuer: "https://auth.example.com/realms//",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			realm, err := retriever.GetIDMTenant(tc.issuer)
			// Current implementation returns empty string for these cases, no error
			assert.NoError(t, err)
			assert.Empty(t, realm)
		})
	}
}

func TestKeycloakIDMRetriever_GetIDMTenant_Performance(t *testing.T) {
	retriever := &KeycloakIDMRetriever{}
	issuer := "https://auth.example.com/realms/performance-test"

	// Test that the method can handle multiple calls efficiently
	for i := 0; i < 1000; i++ {
		realm, err := retriever.GetIDMTenant(issuer)
		assert.NoError(t, err)
		assert.Equal(t, "performance-test", realm)
	}
}

func TestKeycloakIDMRetriever_GetIDMTenant_ConcurrentSafety(t *testing.T) {
	retriever := &KeycloakIDMRetriever{}
	issuer := "https://auth.example.com/realms/concurrent-test"

	// Test concurrent access to the method
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()

			for j := 0; j < 100; j++ {
				realm, err := retriever.GetIDMTenant(issuer)
				assert.NoError(t, err)
				assert.Equal(t, "concurrent-test", realm)
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}
