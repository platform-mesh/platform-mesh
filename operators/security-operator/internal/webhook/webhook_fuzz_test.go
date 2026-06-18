package webhook

import (
	"context"
	"testing"

	"github.com/platform-mesh/platform-mesh/apis/core/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func FuzzIdentityProviderConfigurationValidateCreate(f *testing.F) {
	f.Add("my-realm", "admin,master,system")
	f.Add("master", "")
	f.Add("", "")
	f.Add("   ", "blocked")
	f.Add("valid-realm", "org1,org2")

	f.Fuzz(func(t *testing.T, name, denyListCSV string) {
		var denyList []string
		if denyListCSV != "" {
			denyList = splitCSV(denyListCSV)
		}

		idp := &v1alpha1.IdentityProviderConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: name},
		}
		v := &identityProviderConfigurationValidator{
			keycloakClient: fakeRealmChecker{exists: false},
			realmDenyList:  denyList,
		}

		// Must not panic — validation errors are expected
		_, _ = v.ValidateCreate(context.Background(), idp)
	})
}

func splitCSV(s string) []string {
	var result []string
	start := 0
	for i := range len(s) {
		if s[i] == ',' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
