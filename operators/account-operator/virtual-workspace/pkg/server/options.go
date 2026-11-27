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

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/pflag"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/openfgaprovider"

	"k8s.io/apiserver/pkg/authentication/user"
	genericoptions "k8s.io/apiserver/pkg/server/options"

	"github.com/kcp-dev/logicalcluster/v3"
	vwoptions "github.com/kcp-dev/virtual-workspace-framework/pkg/options"
)

// Options configures the access virtual workspace server.
type Options struct {
	// SecureServing configures TLS serving (bind address, port,
	// serving certs). The VW must serve TLS: behind kcp's front-proxy
	// the proxy verifies the VW's serving cert, and the VW verifies
	// the proxy's client cert via the requestheader CA.
	SecureServing *genericoptions.SecureServingOptions

	// Authentication configures how callers are identified: a JWT
	// authenticator (--authentication-config or the --oidc-* flags),
	// request header identity forwarded by kcp's front-proxy, and
	// client certificates. Its configuration must match kcp's, because
	// the graph compares usernames verbatim against RBAC subjects —
	// see authentication.go.
	Authentication *Authentication

	// Authorization is the virtual-workspace-framework authorizer setup:
	// always-allow paths (health endpoints) plus per-VW authorizers.
	Authorization *vwoptions.Authorization

	// Provider selects where the answer of a review comes from: ProviderRBAC
	// indexes the RBAC bindings of kcp into the access graph, ProviderOpenFGA
	// asks OpenFGA per review. Several, separated by commas, are merged.
	Provider string

	// OpenFGA configures ProviderOpenFGA.
	OpenFGA OpenFGAOptions

	// Kubeconfig is the path to the kubeconfig for the target kcp.
	// Used by the RBAC provider's informers and as the base identity
	// for impersonated per-workspace calls.
	Kubeconfig string

	// EndpointBase is the front-proxy URL prefix used to construct
	// per-cluster endpoints in SCAR responses.
	EndpointBase string

	// DebugGroups are the groups whose members may read /debug/graph. The
	// graph holds every subject and the clusters it is bound in, which is
	// more than a caller may learn from a review of itself. Empty disables
	// the endpoint.
	DebugGroups []string

	// APIExportEndpointSlice is the name of the APIExportEndpointSlice
	// for the access VW's system APIExport. When set, the RBAC provider
	// runs in multi-shard mode and only indexes workspaces bound to
	// that APIExport.
	APIExportEndpointSlice string

	// WorkspacePath is the workspace the kubeconfig is retargeted to,
	// e.g. "root:access:controllers". The APIExportEndpointSlice lookup
	// happens in the workspace the kubeconfig points at; operator-minted
	// admin kubeconfigs point at root, while the bootstrap assets live in
	// the controllers workspace. Empty means use the kubeconfig as-is.
	WorkspacePath string

	// shardExternalURL and cacheKubeconfig are accepted and ignored. kcp's
	// own virtual-workspace server takes them, so deployments templated for
	// that server pass them through; accepting them keeps this server from
	// exiting on an unknown flag. Endpoints here come from --endpoint-base,
	// and all reads go through --kubeconfig rather than kcp's cache server.
	shardExternalURL string
	cacheKubeconfig  string
}

// The providers of --provider.
const (
	ProviderRBAC    = "rbac"
	ProviderOpenFGA = "openfga"
)

// OpenFGAOptions configure the OpenFGA provider.
type OpenFGAOptions struct {
	// Addr is the gRPC address of OpenFGA, host:port.
	Addr string
	// Relation a user needs on an account for its workspace to be listed.
	Relation string
	// ObjectType is the OpenFGA type of an account.
	ObjectType string
	// OrgsWorkspace is the workspace holding the organizations, each with an
	// OpenFGA store of its own.
	OrgsWorkspace string

	// Kubeconfig is what the provider reads kcp with; empty means the
	// kubeconfig of the server.
	Kubeconfig string
	// AccountsEndpointSlice and StoresEndpointSlice name the
	// APIExportEndpointSlices of the exports of Accounts and of Stores, in the
	// workspace the kubeconfig points at. With them the provider reads kcp
	// through the virtual workspaces of the two exports, and its identity
	// needs get on apiexports/content of them only. Without them it reads
	// every workspace directly, which takes an identity allowed to: in kcp,
	// an administrator.
	AccountsEndpointSlice string
	StoresEndpointSlice   string
}

// ThroughExports reports whether kcp is read through APIExports.
func (o OpenFGAOptions) ThroughExports() bool {
	return o.AccountsEndpointSlice != "" || o.StoresEndpointSlice != ""
}

// NewOptions returns options with defaults suitable for running behind
// kcp's front-proxy.
func NewOptions() *Options {
	o := &Options{
		SecureServing:  genericoptions.NewSecureServingOptions(),
		Authentication: NewAuthentication(),
		Authorization:  vwoptions.NewAuthorization(),
		Provider:       ProviderRBAC,
		DebugGroups:    []string{user.SystemPrivilegedGroup, "system:kcp:admin"},
		OpenFGA: OpenFGAOptions{
			Relation:      openfgaprovider.DefaultRelation,
			ObjectType:    openfgaprovider.DefaultObjectType,
			OrgsWorkspace: openfgaprovider.DefaultOrgsWorkspace,
		},
	}

	o.SecureServing.BindPort = 9443
	o.SecureServing.ServerCert.PairName = "access-vw"

	return o
}

// AddFlags registers all flags on the given flag set.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	o.SecureServing.AddFlags(fs)
	o.Authentication.AddFlags(fs)
	o.Authorization.AddFlags(fs)

	fs.StringVar(&o.Provider, "provider", o.Provider,
		"Where the answer of a review comes from: "+ProviderRBAC+" indexes the RBAC bindings of kcp, "+
			ProviderOpenFGA+" asks OpenFGA per review. Several providers, separated by commas ("+
			ProviderRBAC+","+ProviderOpenFGA+"), are merged: a workspace any of them lists is listed, "+
			"and a review fails if one of them cannot answer. "+
			"With "+ProviderOpenFGA+", a review has to be addressed to a workspace of an organization, "+
			"/services/access/clusters/<workspace>/...; --apiexport-endpointslice and --workspace-path belong to "+ProviderRBAC+".")
	fs.StringVar(&o.OpenFGA.Addr, "openfga-addr", o.OpenFGA.Addr,
		"gRPC address of OpenFGA, host:port. Required with the "+ProviderOpenFGA+" provider.")
	fs.StringVar(&o.OpenFGA.Relation, "openfga-relation", o.OpenFGA.Relation,
		"Relation a user needs on an account for its workspace to be listed.")
	fs.StringVar(&o.OpenFGA.ObjectType, "openfga-object-type", o.OpenFGA.ObjectType,
		"OpenFGA type of an account.")
	fs.StringVar(&o.OpenFGA.OrgsWorkspace, "openfga-orgs-workspace", o.OpenFGA.OrgsWorkspace,
		"Workspace holding the organizations. Each organization has an OpenFGA store, "+
			"named by the Store object of the organization's name in this workspace.")

	fs.StringVar(&o.OpenFGA.Kubeconfig, "openfga-kubeconfig", o.OpenFGA.Kubeconfig,
		"Kubeconfig the "+ProviderOpenFGA+" provider reads kcp with; --kubeconfig if empty.")
	fs.StringVar(&o.OpenFGA.AccountsEndpointSlice, "openfga-accounts-endpointslice", o.OpenFGA.AccountsEndpointSlice,
		"Name of the APIExportEndpointSlice of the export of Accounts (core.platform-mesh.io), in the workspace "+
			"the kubeconfig of the provider points at. Set together with --openfga-stores-endpointslice, the "+ProviderOpenFGA+
			" provider reads kcp through the virtual workspaces of the two exports and needs get on apiexports/content "+
			"of them only. Without them it reads every workspace directly and needs an identity allowed to.")
	fs.StringVar(&o.OpenFGA.StoresEndpointSlice, "openfga-stores-endpointslice", o.OpenFGA.StoresEndpointSlice,
		"Name of the APIExportEndpointSlice of the export of Stores (openfga.platform-mesh.io); "+
			"see --openfga-accounts-endpointslice.")

	fs.StringVar(&o.EndpointBase, "endpoint-base", o.EndpointBase,
		"FrontProxy URL prefix for cluster endpoints returned in SCAR responses, "+
			"e.g. https://kcp.example.com/clusters/ (required).")
	fs.StringSliceVar(&o.DebugGroups, "debug-groups", o.DebugGroups,
		"Groups whose members may read "+debugGraphPath+", the access graph of the "+ProviderRBAC+
			" provider with every subject and the clusters it is bound in. Empty disables the endpoint.")
	fs.StringVar(&o.APIExportEndpointSlice, "apiexport-endpointslice", "",
		"Name of the APIExportEndpointSlice for the access VW's system APIExport. "+
			"When set, the RBAC provider runs in multi-shard mode via multicluster-runtime; "+
			"only workspaces with an APIBinding to that APIExport are indexed.")
	fs.StringVar(&o.WorkspacePath, "workspace-path", "",
		"Workspace path the kubeconfig is retargeted to, e.g. root:access:controllers. "+
			"Must be the workspace containing the APIExportEndpointSlice. "+
			"Empty means the kubeconfig's own cluster URL is used unchanged.")
	fs.StringVar(&o.shardExternalURL, "shard-external-url", "",
		"Accepted for compatibility with kcp's own virtual-workspace server and ignored. "+
			"Cluster endpoints are derived from --endpoint-base.")
	fs.StringVar(&o.cacheKubeconfig, "cache-kubeconfig", "",
		"Accepted for compatibility with kcp's own virtual-workspace server and ignored. "+
			"All reads go through --kubeconfig.")
	if fs.Lookup("kubeconfig") == nil {
		fs.StringVar(&o.Kubeconfig, "kubeconfig", "", "Path to the kubeconfig for the target kcp (required).")
	}
}

// Complete fills in derived defaults.
func (o *Options) Complete() error {
	if o.Kubeconfig == "" {
		return fmt.Errorf("--kubeconfig is required")
	}

	if !o.Authentication.OIDCEnabled() && !o.Authentication.RequestHeaderEnabled() {
		return fmt.Errorf("no authentication method configured: set --authentication-config or --oidc-issuer-url " +
			"for direct callers, and/or --requestheader-client-ca-file when running behind kcp's front-proxy")
	}

	return nil
}

// Validate checks flag consistency, reporting every problem it finds
// rather than only the first.
func (o *Options) Validate() error {
	secureServingErrs := o.SecureServing.Validate()
	authenticationErrs := o.Authentication.Validate()
	authorizationErrs := o.Authorization.Validate()

	errs := make([]error, 0, len(secureServingErrs)+len(authenticationErrs)+len(authorizationErrs))
	errs = append(errs, secureServingErrs...)
	errs = append(errs, authenticationErrs...)
	errs = append(errs, authorizationErrs...)

	// No default: a made-up one would be returned in every review as the
	// endpoint of the caller's workspaces.
	if u, err := url.Parse(o.EndpointBase); o.EndpointBase == "" {
		errs = append(errs, errors.New("--endpoint-base is required"))
	} else if err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("--endpoint-base %q is not an absolute URL", o.EndpointBase))
	}

	if o.WorkspacePath != "" {
		if p := logicalcluster.NewPath(o.WorkspacePath); !p.IsValid() {
			errs = append(errs, fmt.Errorf("--workspace-path %q is not a valid workspace path", o.WorkspacePath))
		}
	}

	providers := o.Providers()
	if len(providers) == 0 {
		errs = append(errs, fmt.Errorf("--provider must name at least one of %s, %s", ProviderRBAC, ProviderOpenFGA))
	}
	seen := map[string]bool{}
	for _, provider := range providers {
		if seen[provider] {
			errs = append(errs, fmt.Errorf("--provider names %q more than once", provider))
			continue
		}
		seen[provider] = true

		switch provider {
		case ProviderRBAC:
		case ProviderOpenFGA:
			if o.OpenFGA.Addr == "" {
				errs = append(errs, fmt.Errorf("--openfga-addr is required with the %s provider", ProviderOpenFGA))
			}
			if o.OpenFGA.Relation == "" {
				errs = append(errs, errors.New("--openfga-relation must not be empty"))
			}
			if o.OpenFGA.ObjectType == "" {
				errs = append(errs, errors.New("--openfga-object-type must not be empty"))
			}
			if (o.OpenFGA.AccountsEndpointSlice == "") != (o.OpenFGA.StoresEndpointSlice == "") {
				errs = append(errs, errors.New("--openfga-accounts-endpointslice and --openfga-stores-endpointslice have to be set together"))
			}
			if p := logicalcluster.NewPath(o.OpenFGA.OrgsWorkspace); !p.IsValid() {
				errs = append(errs, fmt.Errorf("--openfga-orgs-workspace %q is not a valid workspace path", o.OpenFGA.OrgsWorkspace))
			}
		default:
			errs = append(errs, fmt.Errorf("--provider %q is unknown, use %s, %s or both separated by a comma", provider, ProviderRBAC, ProviderOpenFGA))
		}
	}

	return errors.Join(errs...)
}

// Providers returns the providers named by --provider, in the order given.
func (o *Options) Providers() []string {
	var providers []string
	for _, provider := range strings.Split(o.Provider, ",") {
		if provider = strings.TrimSpace(provider); provider != "" {
			providers = append(providers, provider)
		}
	}

	return providers
}
