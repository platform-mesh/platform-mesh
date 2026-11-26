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

package kcpconfig

import (
	"testing"

	"k8s.io/client-go/rest"
)

func TestForWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name, host, workspace, want string
	}{
		{"bare server", "https://kcp:6443", "root:platform-mesh-system", "https://kcp:6443/clusters/root:platform-mesh-system"},
		{"replaces a workspace", "https://kcp:6443/clusters/root", "root:orgs", "https://kcp:6443/clusters/root:orgs"},
		{"replaces a virtual workspace path", "https://kcp:6443/services/apiexport/root/x", "root", "https://kcp:6443/clusters/root"},
		{"empty keeps the host", "https://kcp:6443/clusters/root:orgs", "", "https://kcp:6443/clusters/root:orgs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := &rest.Config{Host: tc.host, BearerToken: "t"}
			got, err := ForWorkspace(in, tc.workspace)
			if err != nil {
				t.Fatal(err)
			}
			if got.Host != tc.want {
				t.Errorf("host = %q, want %q", got.Host, tc.want)
			}
			if in.Host != tc.host {
				t.Errorf("the input was changed to %q", in.Host)
			}
			if got.BearerToken != "t" {
				t.Error("the credentials were not copied")
			}
		})
	}
}

func TestRoot(t *testing.T) {
	got, err := Root(&rest.Config{Host: "https://kcp:6443/clusters/root:orgs"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "https://kcp:6443" {
		t.Errorf("host = %q, want https://kcp:6443", got.Host)
	}
}

func TestInvalidHost(t *testing.T) {
	if _, err := ForWorkspace(&rest.Config{Host: "https://kcp:port"}, "root"); err == nil {
		t.Error("ForWorkspace accepted an invalid host")
	}
	if _, err := Root(&rest.Config{Host: "https://kcp:port"}); err == nil {
		t.Error("Root accepted an invalid host")
	}
}
