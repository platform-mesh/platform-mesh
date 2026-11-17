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

func FuzzStoreRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"coreModule":"module","tuples":[{"object":"doc:1","relation":"viewer","user":"user:anne"}]}}`))
	f.Add([]byte(`{"status":{"storeID":"s1","authorizationModelID":"am1","managedTuples":[{"object":"o","relation":"r","user":"u"}]}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &Store{}, &Store{})
	})
}

func FuzzAuthorizationModelRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"storeRef":{"name":"store","cluster":"cl1"},"model":"model openfga/v1","tuples":[{"object":"doc:1","relation":"viewer","user":"user:anne"}]}}`))
	f.Add([]byte(`{"status":{"managedTuples":[{"object":"o","relation":"r","user":"u"}]}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &AuthorizationModel{}, &AuthorizationModel{})
	})
}

func FuzzAPIExportPolicyRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"apiExportRef":{"name":"export","clusterPath":"root:org"},"allowPathExpressions":["root:org:*"]}}`))
	f.Add([]byte(`{"status":{"managedAllowExpressions":["root:org:ws1"]}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &APIExportPolicy{}, &APIExportPolicy{})
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
