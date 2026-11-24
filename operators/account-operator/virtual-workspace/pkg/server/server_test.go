/*
Copyright 2026 The kcp Authors.

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

package server

import "testing"

func TestRetargetHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		path string
		want string
	}{
		{
			name: "replaces existing cluster path",
			host: "https://kcp.example:6443/clusters/root",
			path: "root:access:controllers",
			want: "https://kcp.example:6443/clusters/root:access:controllers",
		},
		{
			name: "replaces nested cluster path",
			host: "https://kcp.example:6443/clusters/root:orgs:foo",
			path: "root:access",
			want: "https://kcp.example:6443/clusters/root:access",
		},
		{
			name: "appends when no cluster path present",
			host: "https://kcp.example:6443",
			path: "root:access:controllers",
			want: "https://kcp.example:6443/clusters/root:access:controllers",
		},
		{
			name: "handles trailing slash",
			host: "https://kcp.example:6443/",
			path: "root:access",
			want: "https://kcp.example:6443/clusters/root:access",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := retargetHost(tc.host, tc.path); got != tc.want {
				t.Errorf("retargetHost(%q, %q) = %q, want %q", tc.host, tc.path, got, tc.want)
			}
		})
	}
}

func TestValidateWorkspacePath(t *testing.T) {
	o := NewOptions()
	o.WorkspacePath = "not a valid path!"
	if err := o.Validate(); err == nil {
		t.Error("expected error for invalid workspace path")
	}
	o.WorkspacePath = "root:access:controllers"
	if err := o.Validate(); err != nil {
		t.Errorf("unexpected error for valid workspace path: %v", err)
	}
}

func TestValidateProvider(t *testing.T) {
	o := NewOptions()
	if o.Provider != ProviderRBAC {
		t.Errorf("default provider = %q, want %q", o.Provider, ProviderRBAC)
	}
	if err := o.Validate(); err != nil {
		t.Errorf("unexpected error for the defaults: %v", err)
	}

	o.Provider = "ldap"
	if err := o.Validate(); err == nil {
		t.Error("expected error for an unknown provider")
	}

	o.Provider = ProviderOpenFGA
	if err := o.Validate(); err == nil {
		t.Error("expected error for the OpenFGA provider without --openfga-addr")
	}
	o.OpenFGA.Addr = "openfga:8081"
	if err := o.Validate(); err != nil {
		t.Errorf("unexpected error for a configured OpenFGA provider: %v", err)
	}

	// Several providers are merged; each has to be valid and named once.
	for provider, valid := range map[string]bool{
		"rbac,openfga":  true,
		"openfga, rbac": true,
		"rbac,ldap":     false,
		"rbac,rbac":     false,
		"":              false,
		",":             false,
	} {
		o.Provider = provider
		if err := o.Validate(); (err == nil) != valid {
			t.Errorf("--provider=%q: valid = %v, error %v", provider, valid, err)
		}
	}
	o.Provider = "rbac,openfga"
	if got := o.Providers(); len(got) != 2 || got[0] != ProviderRBAC || got[1] != ProviderOpenFGA {
		t.Errorf("Providers() = %v", got)
	}

	o.OpenFGA.OrgsWorkspace = "not a valid path!"
	if err := o.Validate(); err == nil {
		t.Error("expected error for an invalid organizations workspace")
	}
}
