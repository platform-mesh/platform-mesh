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

func FuzzTerminalRoundTrip(f *testing.F) {
	f.Add([]byte(`{"apiVersion":"ui.platform-mesh.io/v1alpha1","kind":"Terminal","metadata":{"name":"my-terminal","namespace":"default"},"spec":{},"status":{"phase":"Ready","sessionId":"abc123","createdBy":"user@example.com","podName":"terminal-pod-xyz","workspacePath":"root:org:ws","observedGeneration":1}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"metadata":{"name":"t"},"status":{"phase":"Pending"}}`))
	f.Add([]byte(`{"status":{"phase":"Failed","sessionId":"","conditions":[{"type":"Ready","status":"False","reason":"PodFailed","message":"container exited","lastTransitionTime":"2024-01-01T00:00:00Z"}]}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &Terminal{}, &Terminal{})
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
