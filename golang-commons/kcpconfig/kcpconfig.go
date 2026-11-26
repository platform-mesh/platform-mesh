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

// Package kcpconfig points a kcp rest config at a workspace.
//
// A component is handed one kcp rest config and names the workspaces it
// works in with options. The config's own path does not matter: a kubeconfig
// mounted into a Pod points at a workspace, a process that embeds kcp has
// the bare server URL, and both work.
package kcpconfig

import (
	"fmt"
	"net/url"
	"path"

	"k8s.io/client-go/rest"
)

// ForWorkspace returns a copy of cfg whose host is the workspace at
// workspace (a path such as root:platform-mesh-system, or a logical cluster
// name), replacing whatever path the host of cfg has. An empty workspace
// returns the copy as it is: the workspace the config already points at.
func ForWorkspace(cfg *rest.Config, workspace string) (*rest.Config, error) {
	out := rest.CopyConfig(cfg)
	if workspace == "" {
		return out, nil
	}
	parsed, err := url.Parse(out.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing the host %q of the kcp config: %w", out.Host, err)
	}
	parsed.Path = path.Join("/clusters", workspace)
	out.Host = parsed.String()
	return out, nil
}

// Root returns a copy of cfg whose host is kcp itself, without a workspace.
func Root(cfg *rest.Config) (*rest.Config, error) {
	out := rest.CopyConfig(cfg)
	parsed, err := url.Parse(out.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing the host %q of the kcp config: %w", out.Host, err)
	}
	parsed.Path = ""
	out.Host = parsed.String()
	return out, nil
}
