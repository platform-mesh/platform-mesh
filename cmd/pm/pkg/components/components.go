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

// Package components holds the Platform Mesh components "pm start" runs as
// goroutines of one process. Each one is wired here from the exported
// packages of its own module, without that module's cobra command: the
// commands own process-wide state (signal handlers, logging setup, exits).
package components

import (
	"context"
	"io/fs"

	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/bootstrap"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/oidc"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/openfga"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	"github.com/kcp-dev/logicalcluster/v3"
)

// Component is one Platform Mesh component.
type Component struct {
	Name string
	// Resources are the APIResourceSchemas and APIExports the component
	// serves; nil for none.
	Resources fs.FS
	// Bindings are the APIExports that have to be bound in
	// :root:platform-mesh-system for the component's provider to see an
	// endpoint.
	Bindings []string
	// Manifests are files of the embedded kcp manifests
	// (apis/static-assets/kcp) to apply in :root:platform-mesh-system once
	// the exports are bound there.
	Manifests []string
	// OpenFGA says the component makes kcp wait for OpenFGA: the manifests
	// are then rendered with it.
	OpenFGA bool
	// WorkspaceTypes are manifest files of WorkspaceTypes for :root, and
	// OrgsManifests manifest files to apply in :root:orgs.
	WorkspaceTypes []string
	OrgsManifests  []string
	// Objects are applied in :root:platform-mesh-system after the
	// bindings, on every start: they may hold the credentials of cfg, the
	// kcp admin config, which kcp renews when it starts.
	Objects func(cfg *rest.Config) ([]*unstructured.Unstructured, error)
	// Run blocks until ctx is done or the component fails.
	Run func(ctx context.Context, env Env) error
}

// Env is what a component gets from the process.
type Env struct {
	// Config is the kcp admin config; its host is the bare server URL.
	Config *rest.Config
	// Platform is the installed workspace tree.
	Platform *bootstrap.Platform
	// Kubeconfig is the path of the admin kubeconfig, for components that
	// only take a file.
	Kubeconfig string
	// DataDir is a directory the component may keep files in.
	DataDir string
	// LogLevel is a zerolog level name.
	LogLevel string
	// GraphQLPort is the localhost port of the GraphQL gateway.
	GraphQLPort int
	// OIDC is the mock issuer kcp trusts; nil without one.
	OIDC *oidc.Config
	// AccountWebhookPort is the localhost port of the account-operator's
	// admission webhooks and AccountWebhookCertDir holds their serving
	// certificate; the port is 0 without them.
	AccountWebhookPort    int
	AccountWebhookCertDir string
	// OpenFGA is the OpenFGA of the process; nil when nothing selected
	// needs it.
	OpenFGA *openfga.Server
	// AuthzWebhookPort is the localhost port of the authorization webhook.
	AuthzWebhookPort int
	// VirtualWorkspacesPort is the localhost port of the virtual workspace
	// server.
	VirtualWorkspacesPort int
	// PortalImage is the portal container's image. PortalPort is the
	// localhost port browsers open, which Ingress serves.
	PortalImage string
	PortalPort  int
	// IAMUIImage and MarketplaceUIImage are the images of the micro
	// frontends.
	IAMUIImage         string
	MarketplaceUIImage string
	// Ports are the localhost ports behind Ingress.
	Ports Ports
}

// ConfigFor returns a copy of the admin config pointing at the workspace at
// path, for the few places that do not take the workspace as an option.
func (e Env) ConfigFor(path logicalcluster.Path) *rest.Config {
	cfg := rest.CopyConfig(e.Config)
	cfg.Host += path.RequestPath()
	return cfg
}

// All returns every component, in start order.
func All() []Component {
	return []Component{
		AccountOperator(),
		GraphQLGateway(),
		OpenFGAOperator(),
		AuthzWebhook(),
		IAMService(),
		ExtensionManager(),
		VirtualWorkspaces(),
		Portal(),
		IAMUI(),
		MarketplaceUI(),
		Ingress(),
	}
}

// quietCommon is the configuration every Platform Mesh service shares, with
// the metrics and health listeners off: the components of one process would
// all bind the same ports.
func quietCommon() *platformmeshconfig.CommonServiceConfig {
	common := platformmeshconfig.NewDefaultConfig()
	common.Metrics.BindAddress = "0"
	common.HealthProbeBindAddress = "0"
	return common
}

func newLogger(name, level string) (*logger.Logger, error) {
	cfg := logger.DefaultConfig()
	cfg.Name = name
	cfg.Level = level
	cfg.NoJSON = true
	return logger.New(cfg)
}
