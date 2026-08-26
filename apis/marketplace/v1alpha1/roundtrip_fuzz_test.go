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
	"bytes"
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
)

var marketplaceEntrySeeds = []struct {
	name string
	json string
}{
	{"fullSpec", `{
		"apiVersion": "marketplace.platform-mesh.io/v1alpha1",
		"kind": "MarketplaceEntry",
		"metadata": {"name": "my-extension", "labels": {"app": "test"}},
		"spec": {
			"apiBindingName": "my-extension-foo12",
			"providerMetadata": {
				"metadata": {"name": "foo-provider"},
				"spec": {
					"displayName": "My Extension",
					"description": "An example marketplace extension",
					"tags": ["networking", "security"],
					"contacts": [{"displayName": "Support", "email": "support@example.com", "role": ["maintainer"]}],
					"documentation": [{"displayName": "Docs", "url": "https://example.com/docs"}],
					"links": [{"displayName": "Homepage", "url": "https://example.com"}],
					"icon": {"light": {"url": "https://example.com/l.png"}, "dark": {"url": "https://example.com/d.png"}},
					"preferredSupportChannels": [{"displayName": "Slack", "url": "https://example.com/slack"}]
				}
			},
			"apiExport": {
				"metadata": {"name": "test-api-export", "annotations": {"kcp.io/path": "root:providers:foo"}},
				"spec": {
					"resources": [{"group": "foo.io", "name": "widgets",
					 "schema": "v260623-482e10b2.widgets.foo.io", "storage": {"crd": {}}}],
					"identity": {"secretRef": {"name": "widgets-identity", "namespace": "kcp-system"}},
					"maximalPermissionPolicy": {"local": {}},
					"permissionClaims": [
						{"group": "", "resource": "secrets", "verbs": ["*"], "identityHash": "abc123"},
						{"group": "apps", "resource": "deployments", "verbs": ["get", "list"],
						 "defaultSelector": {"matchAll": true}}
					]
				}
			}
		}
	}`},
	{"emptyObj", `{}`},
}

func FuzzMarketplaceEntryRoundTrip(f *testing.F) {
	for _, seed := range marketplaceEntrySeeds {
		f.Add([]byte(seed.json))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &MarketplaceEntry{}, &MarketplaceEntry{})
	})
}

func TestSeedsDecodeStrictly(t *testing.T) {
	for _, seed := range marketplaceEntrySeeds {
		t.Run(seed.name, func(t *testing.T) {
			var entry MarketplaceEntry
			if err := strictUnmarshal([]byte(seed.json), &entry); err != nil {
				t.Errorf("seed does not match MarketplaceEntry: %v", err)
			}
		})
	}
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

	if err := strictUnmarshal(roundtripped, obj2); err != nil {
		t.Fatalf("failed to unmarshal roundtripped data: %v", err)
	}

	if !equality.Semantic.DeepEqual(obj, obj2) {
		t.Errorf("roundtrip mismatch for %T", obj)
	}
}

// strictUnmarshal rejects unknown fields so these  fail the tests.
// Fuzz output not strict on purpose.
func strictUnmarshal(data []byte, obj any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(obj)
}
