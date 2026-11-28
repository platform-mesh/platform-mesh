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

package config

import (
	"github.com/spf13/pflag"
)

// KCPConfig carries the operator's view of PM's kcp admin plane.
//
// The operator loads Kubeconfig from a file mounted into the Pod; the
// Secret is provisioned and renewed by platform-mesh-operator's
// ProvidersecretSubroutine (see DefaultProviderConnections, the
// "access-operator-kubeconfig" entry).
//
// ExternalUrl and CABundlePath come from a different source — they are
// not the operator's own credential, but values propagated into the
// /access handshake body so the user-side cluster can trust PM's kcp.
type KCPConfig struct {
	// Kubeconfig is the on-disk path to the operator's kcp kubeconfig.
	Kubeconfig string

	// APIExportEndpointSliceName is the name of the
	// APIExportEndpointSlice the operator engages via its
	// apiexport-provider. Must match the APIExport the kubeconfig is
	// scoped to.
	APIExportEndpointSliceName string

	// ExternalUrl is the publicly-reachable kcp URL propagated to
	// user-side clusters in the /access handshake body. Required for
	// the handshake; the operator refuses to dial if unset.
	ExternalUrl string

	// CABundlePath is the path to a PEM-encoded CA bundle mounted into
	// the operator's filesystem. Used as kcpCABundle in the /access
	// handshake body so the user-side cluster can trust kcp.
	CABundlePath string
}

// OperatorConfig is the access-operator top-level config.
type OperatorConfig struct {
	KCP KCPConfig
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		KCP: KCPConfig{
			APIExportEndpointSliceName: "access.platform-mesh.io",
		},
	}
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.KCP.Kubeconfig, "kcp-kubeconfig", c.KCP.Kubeconfig, "Path to the operator's kcp kubeconfig, mounted from a Secret provisioned by platform-mesh-operator")
	fs.StringVar(&c.KCP.APIExportEndpointSliceName, "kcp-api-export-endpoint-slice-name", c.KCP.APIExportEndpointSliceName, "Name of the APIExportEndpointSlice the apiexport-provider engages")
	fs.StringVar(&c.KCP.ExternalUrl, "kcp-external-url", c.KCP.ExternalUrl, "Publicly-reachable kcp URL propagated into /access handshake body")
	fs.StringVar(&c.KCP.CABundlePath, "kcp-ca-bundle-path", c.KCP.CABundlePath, "Path to a PEM-encoded kcp CA bundle, mounted into the operator and propagated into /access handshake body")
}
