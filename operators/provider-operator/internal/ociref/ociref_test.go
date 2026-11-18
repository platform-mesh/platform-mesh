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

package ociref_test

import (
	"testing"

	"go.platform-mesh.io/provider-operator/internal/ociref"
)

func TestStripScheme(t *testing.T) {
	cases := []struct{ input, want string }{
		{"oci://ghcr.io/org/repo", "ghcr.io/org/repo"},
		{"http://kind-registry:5000/org/repo", "kind-registry:5000/org/repo"},
		{"ghcr.io/org/repo", "ghcr.io/org/repo"},
		{"http://", ""},
	}
	for _, tc := range cases {
		if got := ociref.StripScheme(tc.input); got != tc.want {
			t.Errorf("StripScheme(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSplitRegistry(t *testing.T) {
	cases := []struct{ input, wantBase, wantSub string }{
		{"ghcr.io/platform-mesh", "ghcr.io", "platform-mesh"},
		{"ghcr.io/platform-mesh/provider-quickstart/charts", "ghcr.io", "platform-mesh/provider-quickstart/charts"},
		{"ghcr.io", "ghcr.io", ""},
		{"http://kind-registry:5000/ghcr.io/platform-mesh/provider-quickstart/wildwest-operator", "http://kind-registry:5000", "ghcr.io/platform-mesh/provider-quickstart/wildwest-operator"},
		{"http://", "http://", ""},
		{"http:///", "http://", ""},
	}
	for _, tc := range cases {
		base, sub := ociref.SplitRegistry(tc.input)
		if base != tc.wantBase || sub != tc.wantSub {
			t.Errorf("SplitRegistry(%q) = (%q, %q), want (%q, %q)", tc.input, base, sub, tc.wantBase, tc.wantSub)
		}
	}
}

func TestNormalizeOCIURL(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cases := []struct{ name, imageRef, version, want string }{
		{"tag form", "oci://ghcr.io/platform-mesh/charts/wildwest:1.2.3", "1.2.3", "oci://ghcr.io/platform-mesh/charts/wildwest"},
		{"no scheme", "ghcr.io/platform-mesh/charts/wildwest:1.2.3", "1.2.3", "oci://ghcr.io/platform-mesh/charts/wildwest"},
		{"digest form", "oci://ghcr.io/platform-mesh/charts/wildwest@" + digest, "1.2.3", "oci://ghcr.io/platform-mesh/charts/wildwest"},
		{"http scheme with digest", "http://kind-registry:5000/org/chart:1.2.3@" + digest, "1.2.3", "oci://kind-registry:5000/org/chart"},
	}
	for _, tc := range cases {
		got, err := ociref.NormalizeOCIURL(tc.imageRef, tc.version)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: NormalizeOCIURL(%q, %q) = %q, want %q", tc.name, tc.imageRef, tc.version, got, tc.want)
		}
	}
}

func TestExtractRepoURL(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"oci://registry.example.com/charts/mychart:1.0.0@sha256:abc", "registry.example.com/charts", false},
		{"registry.example.com/org/charts/app:v2.0", "registry.example.com/org/charts", false},
		{"noslash", "", true},
	}
	for _, tc := range cases {
		got, err := ociref.ExtractRepoURL(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ExtractRepoURL(%q) expected error, got %q", tc.input, got)
			}
		} else {
			if err != nil {
				t.Fatalf("ExtractRepoURL(%q) error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ExtractRepoURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		}
	}
}
