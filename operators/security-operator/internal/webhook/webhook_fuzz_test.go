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

package webhook

import (
	"context"
	"testing"

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

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

		idp := &corev1alpha1.IdentityProviderConfiguration{
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
