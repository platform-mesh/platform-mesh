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

// Package start is "pm start": kcp, the workspace bootstrap and the
// components, in that order, in one process.
package start

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/bootstrap"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/certs"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/components"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/kcp"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/oidc"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/openfga"

	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Options are the options of "pm start".
type Options struct {
	RootDirectory string
	// Components are the names of the components to run.
	Components []string
	// LogLevel is the zerolog level of the components.
	LogLevel string
	// GraphQLPort is the localhost port of the GraphQL gateway.
	GraphQLPort int
	// OIDC is the mock issuer; OIDCEnabled switches it off.
	OIDCEnabled bool
	OIDC        oidc.Options
	// PortalImage is the portal container's image, PortalPort the localhost
	// port browsers open.
	PortalImage string
	PortalPort  int
	// IAMUIImage and MarketplaceUIImage are the images of the micro
	// frontends.
	IAMUIImage         string
	MarketplaceUIImage string
	// AccountWebhookPort is the localhost port of the account-operator's
	// admission webhooks; 0 runs without them.
	AccountWebhookPort int
	// VirtualWorkspacesPort is the localhost port of the virtual workspace
	// server.
	VirtualWorkspacesPort int
	// OpenFGAPort and AuthzWebhookPort are the localhost ports of OpenFGA's
	// gRPC API and of the authorization webhook kcp calls.
	OpenFGAPort      int
	AuthzWebhookPort int

	kcp *kcp.Server
	all []components.Component
}

// NewOptions returns the defaults for a process keeping its state in rootDir.
func NewOptions(rootDir string) *Options {
	o := &Options{
		RootDirectory: rootDir,
		LogLevel:      "info",
		GraphQLPort:   8080,
		OIDCEnabled:   true,
		PortalImage:   "ghcr.io/platform-mesh/portal:v0.27.46",
		PortalPort:    3000,

		IAMUIImage:         "ghcr.io/platform-mesh/iam-ui:v0.12.34",
		MarketplaceUIImage: "ghcr.io/platform-mesh/marketplace-ui:v0.15.19",

		AccountWebhookPort:    9443,
		VirtualWorkspacesPort: 6445,
		OpenFGAPort:           8081,
		AuthzWebhookPort:      9444,
		OIDC: oidc.Options{
			Port:        5556,
			BrowserPort: 5557,
			Email:       "admin@platform-mesh.local",
		},
		kcp: kcp.New(rootDir),
		all: components.All(),
	}
	for _, c := range o.all {
		o.Components = append(o.Components, c.Name)
	}
	return o
}

// AddFlags adds the flags of "pm start" and of the embedded kcp to fss.
func (o *Options) AddFlags(fss *cliflag.NamedFlagSets) {
	fs := fss.FlagSet("Platform Mesh")
	fs.StringSliceVar(&o.Components, "components", o.Components, "Components to run in this process.")
	fs.StringVar(&o.LogLevel, "component-log-level", o.LogLevel, "Log level of the components (debug, info, warn, error).")
	fs.IntVar(&o.GraphQLPort, "graphql-port", o.GraphQLPort, "Localhost port the GraphQL gateway serves on.")
	fs.BoolVar(&o.OIDCEnabled, "mock-oidc", o.OIDCEnabled, "Run a mock OIDC issuer in this process and have kcp trust it. Development only: it logs anyone in without a password. Ignored when --oidc-issuer-url is set.")
	fs.IntVar(&o.OIDC.Port, "mock-oidc-port", o.OIDC.Port, "Localhost port of the mock OIDC issuer.")
	fs.IntVar(&o.OIDC.BrowserPort, "mock-oidc-browser-port", o.OIDC.BrowserPort, "Localhost port where the mock OIDC issuer takes browser logins without TLS.")
	fs.StringVar(&o.OIDC.Email, "mock-oidc-email", o.OIDC.Email, "Who a login at the mock OIDC issuer is, unless the request has a login_hint.")
	fs.StringSliceVar(&o.OIDC.Groups, "mock-oidc-groups", o.OIDC.Groups, "Groups of every user of the mock OIDC issuer. Default: none with the authorization webhook, which grants users what OpenFGA says; kcp's admin group (system:kcp:admin) without it.")
	fs.IntVar(&o.OpenFGAPort, "openfga-port", o.OpenFGAPort, "Localhost port of the gRPC API of the OpenFGA of this process.")
	fs.IntVar(&o.AuthzWebhookPort, "authz-webhook-port", o.AuthzWebhookPort, "Localhost port of the authorization webhook, which kcp asks about the requests its own RBAC does not allow.")
	fs.IntVar(&o.AccountWebhookPort, "account-webhook-port", o.AccountWebhookPort, "Localhost port of the account-operator's admission webhooks, which kcp calls for every Account; 0 runs without them.")
	fs.IntVar(&o.VirtualWorkspacesPort, "virtual-workspaces-port", o.VirtualWorkspacesPort, "Localhost port of the virtual workspace server, which kcp forwards the /services/... paths of Platform Mesh to.")
	fs.StringVar(&o.PortalImage, "portal-image", o.PortalImage, "Image of the portal, run as a docker container on the host network for as long as this process runs.")
	fs.StringVar(&o.IAMUIImage, "iam-ui-image", o.IAMUIImage, "Image of the IAM micro frontend, run as a docker container.")
	fs.StringVar(&o.MarketplaceUIImage, "marketplace-ui-image", o.MarketplaceUIImage, "Image of the marketplace micro frontend, run as a docker container.")
	fs.IntVar(&o.PortalPort, "portal-port", o.PortalPort, "Localhost port browsers open: http://localhost:<port> and http://<org>.localhost:<port>.")
	// --root-directory itself is a kcp flag.
	o.kcp.AddFlags(fss)
}

func (o *Options) selected() ([]components.Component, error) {
	var out []components.Component
	for _, name := range o.Components {
		i := slices.IndexFunc(o.all, func(c components.Component) bool { return c.Name == name })
		if i < 0 {
			known := make([]string, 0, len(o.all))
			for _, c := range o.all {
				known = append(known, c.Name)
			}
			return nil, fmt.Errorf("unknown component %q (known: %s)", name, strings.Join(known, ", "))
		}
		out = append(out, o.all[i])
	}
	return out, nil
}

// Run starts everything and blocks until ctx is done or something fails.
func Run(ctx context.Context, o *Options) error {
	selected, err := o.selected()
	if err != nil {
		return err
	}
	dataDir, err := filepath.Abs(o.RootDirectory)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	has := func(name string) bool {
		return slices.ContainsFunc(selected, func(c components.Component) bool { return c.Name == name })
	}
	if has(components.VirtualWorkspacesName) {
		backends := map[string]string{}
		for _, path := range components.VirtualWorkspacePaths {
			backends[path] = fmt.Sprintf("https://127.0.0.1:%d", o.VirtualWorkspacesPort)
		}
		if err := o.kcp.ProxyPaths(backends); err != nil {
			return err
		}
	}

	// Before kcp, which reads the webhook's CA when it starts.
	if has(components.AuthzWebhookName) {
		ca, _, err := certs.Ensure(components.AuthzWebhookCertDir(dataDir))
		if err != nil {
			return fmt.Errorf("authorization webhook certificate: %w", err)
		}
		if err := o.kcp.AuthorizeWith(fmt.Sprintf("https://127.0.0.1:%d%s", o.AuthzWebhookPort, components.AuthzWebhookPath), ca); err != nil {
			return err
		}
	} else if o.OIDC.Groups == nil {
		// Nothing to grant users anything finer.
		o.OIDC.Groups = []string{"system:kcp:admin"}
	}

	var fga *openfga.Server
	if has(components.OpenFGAOperatorName) || has(components.AuthzWebhookName) || has(components.IAMServiceName) {
		fga, err = openfga.Start(ctx, openfga.Options{Port: o.OpenFGAPort, Dir: filepath.Join(dataDir, "openfga")})
		if err != nil {
			return fmt.Errorf("openfga: %w", err)
		}
	}

	// Before kcp, which reads the issuer's CA file when it starts.
	var issuer *oidc.Config
	if o.OIDCEnabled && !o.kcp.HasOIDC() {
		o.OIDC.Dir = filepath.Join(dataDir, "oidc")
		issuer, err = oidc.Start(ctx, o.OIDC)
		if err != nil {
			return fmt.Errorf("mock oidc: %w", err)
		}
		o.kcp.TrustOIDC(issuer.IssuerURL, issuer.ClientID, issuer.CAFile)
	}
	g.Go(func() error {
		if err := o.kcp.Run(ctx); err != nil {
			return fmt.Errorf("kcp: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		cfg, err := o.kcp.WaitReady(ctx, 2*time.Minute)
		if err != nil {
			return err
		}
		// kcp applied the logging configuration by now.
		log := klog.Background().WithName("pm")
		ctrl.SetLogger(klog.Background().WithName("controller-runtime"))
		ctx := klog.NewContext(ctx, log)

		install := bootstrap.Options{PublicHost: fmt.Sprintf("%s:%d", components.PortalBaseDomain, o.PortalPort)}
		env := components.Env{Config: cfg, Platform: nil, DataDir: dataDir, LogLevel: o.LogLevel, GraphQLPort: o.GraphQLPort, OIDC: issuer, PortalImage: o.PortalImage, PortalPort: o.PortalPort, IAMUIImage: o.IAMUIImage, MarketplaceUIImage: o.MarketplaceUIImage, VirtualWorkspacesPort: o.VirtualWorkspacesPort, OpenFGA: fga, AuthzWebhookPort: o.AuthzWebhookPort}
		if env.Ports, err = freePorts(); err != nil {
			return err
		}
		if o.AccountWebhookPort != 0 && slices.ContainsFunc(selected, func(c components.Component) bool { return c.Name == "account-operator" }) {
			env.AccountWebhookPort = o.AccountWebhookPort
			env.AccountWebhookCertDir = filepath.Join(dataDir, "account-webhook")
			if err := os.MkdirAll(env.AccountWebhookCertDir, 0o700); err != nil {
				return err
			}
			ca, _, err := certs.Ensure(env.AccountWebhookCertDir)
			if err != nil {
				return fmt.Errorf("account webhook certificate: %w", err)
			}
			install.AccountWebhook = &bootstrap.Webhook{URL: fmt.Sprintf("https://127.0.0.1:%d", o.AccountWebhookPort), CAFile: ca}
		}
		for _, c := range selected {
			if c.Resources != nil {
				install.Resources = append(install.Resources, c.Resources)
			}
			install.Bindings = append(install.Bindings, c.Bindings...)
			install.Manifests = append(install.Manifests, c.Manifests...)
			install.WorkspaceTypes = append(install.WorkspaceTypes, c.WorkspaceTypes...)
			install.OrgsManifests = append(install.OrgsManifests, c.OrgsManifests...)
			install.OpenFGA = install.OpenFGA || c.OpenFGA
			if c.Objects != nil {
				objs, err := c.Objects(cfg)
				if err != nil {
					return fmt.Errorf("%s: %w", c.Name, err)
				}
				install.Objects = append(install.Objects, objs...)
			}
		}
		platform, err := bootstrap.Install(ctx, cfg, install)
		if err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}

		kubeconfig, err := filepath.Abs(o.kcp.AdminKubeconfig())
		if err != nil {
			return err
		}
		env.Kubeconfig, env.Platform = kubeconfig, platform
		for _, c := range selected {
			log.Info("starting component", "component", c.Name)
			g.Go(func() error {
				if err := c.Run(ctx, env); err != nil {
					return fmt.Errorf("%s: %w", c.Name, err)
				}
				return nil
			})
		}
		log.Info("Platform Mesh is running",
			"kubeconfig", kubeconfig,
			"orgs", platform.Orgs,
			"graphql", fmt.Sprintf("http://127.0.0.1:%d/api/clusters/<workspace path>/graphql", o.GraphQLPort))
		if issuer != nil {
			if slices.ContainsFunc(selected, func(c components.Component) bool { return c.Name == "portal" }) {
				log.Info("portal", "url", components.PortalURL(o.PortalPort), "org", fmt.Sprintf("http://<org>.%s:%d", components.PortalBaseDomain, o.PortalPort))
			}
			log.Info("mock OIDC issuer is running: anyone can log in as anyone", "issuer", issuer.IssuerURL, "clientID", issuer.ClientID, "user", o.OIDC.Email)
		}
		return nil
	})
	return g.Wait()
}

// freePorts picks the ports of what only Ingress talks to.
func freePorts() (components.Ports, error) {
	var ports components.Ports
	for _, p := range []*int{&ports.Portal, &ports.IAMUI, &ports.MarketplaceUI, &ports.IAMService} {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return ports, err
		}
		*p = ln.Addr().(*net.TCPAddr).Port
		// Closed right away: the port is free until its component binds
		// it, which is good enough on a development machine.
		_ = ln.Close()
	}
	return ports, nil
}
