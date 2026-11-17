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

package v1alpha1

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
)

func FuzzIdentityProviderConfigurationRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"registrationAllowed":true,"clients":[{"clientType":"confidential","clientName":"app","redirectURIs":["https://app/callback"]}]}}`))
	f.Add([]byte(`{"status":{"managedClients":{"app":{"clientID":"c1","registrationClientURI":"https://kc/clients/c1"}}}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &IdentityProviderConfiguration{}, &IdentityProviderConfiguration{})
	})
}

func FuzzInviteRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"email":"user@example.com"}}`))
	f.Add([]byte(`{"spec":{"email":""}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &Invite{}, &Invite{})
	})
}

func fuzzRoundTrip[T any](t *testing.T, data []byte, obj *T, obj2 *T) {
	t.Helper()

	if err := json.Unmarshal(data, obj); err != nil {
		return
	}

	roundtripped, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if err := json.Unmarshal(roundtripped, obj2); err != nil {
		t.Fatalf("failed to unmarshal roundtripped data: %v", err)
	}

	if !equality.Semantic.DeepEqual(obj, obj2) {
		t.Errorf("roundtrip mismatch for %T", obj)
	}
}
