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

// Package server wires the Access Virtual Workspace binary together:
// the shared access graph, the RBAC provider that populates it, and a
// virtual-workspace root apiserver (kcp virtual-workspace-framework)
// serving the access virtual workspace at /services/access behind
// kcp's front-proxy.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/accessprovider"
	generatedopenapi "go.platform-mesh.io/account-operator/virtual-workspace/pkg/generated/openapi"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/openfgaprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/rbacprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/virtual/scar"
	accessv1alpha1 "go.platform-mesh.io/account-operator/virtual-workspace/sdk/apis/access/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/union"
	openapinamer "k8s.io/apiserver/pkg/endpoints/openapi"
	genericapiserver "k8s.io/apiserver/pkg/server"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/rootapiserver"
)

const debugGraphPath = "/debug/graph"

func retargetHost(host, workspacePath string) string {
	if i := strings.Index(host, "/clusters/"); i >= 0 {
		host = host[:i]
	}
	return strings.TrimSuffix(host, "/") + "/clusters/" + workspacePath
}

// Run starts the provider and serves the Access virtual workspace until
// ctx is cancelled.
func Run(parent context.Context, o *Options) error {
	if err := o.Complete(); err != nil {
		return err
	}
	if err := o.Validate(); err != nil {
		return err
	}

	// The provider and the server end together: whichever returns first
	// cancels the other.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	restConfig, err := clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}

	// Where the answers come from: one access provider per --provider, merged
	// if there are several. debug is what /debug/graph shows, nil when no
	// provider keeps a graph; providerErr reports a provider that stopped, and
	// stays silent for one that runs nothing in the background.
	var (
		providers accessprovider.Union
		debug     func() any
		// Buffered for everything that reports: the RBAC provider and the two
		// export providers of the OpenFGA one. Only the first is read.
		providerErr = make(chan error, 3)
	)

	for _, name := range o.Providers() {
		switch name {
		case ProviderOpenFGA:
			resolver, err := newOpenFGAResolver(ctx, restConfig, o, providerErr)
			if err != nil {
				return err
			}
			providers = append(providers, resolver)

			klog.InfoS("openfgaprovider running", "openfga", o.OpenFGA.Addr, "relation", o.OpenFGA.Relation,
				"objectType", o.OpenFGA.ObjectType, "orgsWorkspace", o.OpenFGA.OrgsWorkspace,
				"accountsEndpointSlice", o.OpenFGA.AccountsEndpointSlice, "storesEndpointSlice", o.OpenFGA.StoresEndpointSlice)

		case ProviderRBAC:
			// A copy: the OpenFGA provider reads kcp with the configuration as loaded.
			rbacConfig := rest.CopyConfig(restConfig)
			if o.WorkspacePath != "" {
				rbacConfig.Host = retargetHost(rbacConfig.Host, o.WorkspacePath)
			}
			g := graph.New()

			populator := rbacprovider.New(o.EndpointBase)
			populator.RestConfig = rbacConfig
			populator.APIExportEndpointSlice = o.APIExportEndpointSlice

			go func() {
				providerErr <- populator.Start(ctx, g)
			}()

			mode := "single-shard"
			if o.APIExportEndpointSlice != "" {
				mode = "multi-shard (apiexport=" + o.APIExportEndpointSlice + ")"
			}
			klog.InfoS("rbacprovider running", "mode", mode, "kubeconfig", o.Kubeconfig, "host", rbacConfig.Host)

			providers = append(providers, accessprovider.GraphAdapter{Graph: g})
			debug = func() any {
				return graphDebug{
					Snapshot:               g.Snapshot(),
					EngagedClusters:        populator.EngagedClusters(),
					APIExportEndpointSlice: o.APIExportEndpointSlice,
				}
			}
		}
	}

	var provider accessprovider.AccessProvider = providers
	if len(providers) == 1 {
		provider = providers[0]
	}

	vws := []rootapiserver.NamedVirtualWorkspace{
		scar.NewVirtualWorkspace(provider),
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(accessv1alpha1.AddToScheme(scheme))
	codecs := serializer.NewCodecFactory(scheme)

	recommended := genericapiserver.NewRecommendedConfig(codecs)

	namer := openapinamer.NewDefinitionNamer(scheme)
	recommended.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(generatedopenapi.GetOpenAPIDefinitions, namer)
	recommended.OpenAPIConfig.Info.Title = "access-vw"
	recommended.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(generatedopenapi.GetOpenAPIDefinitions, namer)
	recommended.OpenAPIV3Config.Info.Title = "access-vw"

	rootCfg, err := rootapiserver.NewConfig(recommended)
	if err != nil {
		return fmt.Errorf("create root apiserver config: %w", err)
	}
	rootCfg.Extra.VirtualWorkspaces = vws

	if err := o.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{netutils.ParseIPSloppy("127.0.0.1")}); err != nil {
		return fmt.Errorf("default serving certs: %w", err)
	}
	if err := o.SecureServing.ApplyTo(&recommended.SecureServing); err != nil {
		return fmt.Errorf("apply secure serving: %w", err)
	}
	if err := o.Authentication.ApplyTo(ctx, &recommended.Authentication, recommended.SecureServing); err != nil {
		return fmt.Errorf("apply authentication: %w", err)
	}
	if err := o.Authorization.ApplyTo(&recommended.Config, func() []rootapiserver.NamedVirtualWorkspace { return vws }); err != nil {
		return fmt.Errorf("apply authorization: %w", err)
	}

	// The graph shows every subject with the clusters it is bound in, so the
	// debug endpoint is for the members of --debug-groups only, and only
	// there when a provider keeps a graph. It is outside the VW authorizers,
	// so granted explicitly.
	serveDebug := debug != nil && len(o.DebugGroups) > 0
	if serveDebug {
		recommended.Authorization.Authorizer = union.New(
			recommended.Authorization.Authorizer,
			pathScopedAuthorizer(debugGraphPath, groupAuthorizer(o.DebugGroups)),
		)
	}

	completed := rootCfg.Complete()
	rootServer, err := rootapiserver.NewServer(completed, genericapiserver.NewEmptyDelegate())
	if err != nil {
		return fmt.Errorf("create root apiserver: %w", err)
	}

	// Debug endpoint: pretty-printed snapshot of the access graph, for a
	// provider that keeps one.
	if serveDebug {
		rootServer.GenericAPIServer.Handler.NonGoRestfulMux.HandleFunc(debugGraphPath, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", http.MethodGet)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			if err := enc.Encode(debug()); err != nil {
				klog.ErrorS(err, "encoding access graph snapshot")
			}
		})
	}

	prepared := rootServer.GenericAPIServer.PrepareRun()

	klog.InfoS("access-vw serving",
		"scar", scar.RootPath+"/apis/"+accessv1alpha1.SchemeGroupVersion.Group+"/"+accessv1alpha1.SchemeGroupVersion.Version+"/selfclusteraccessreviews",
	)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- prepared.RunWithContext(ctx)
	}()

	select {
	case err := <-providerErr:
		// Without the provider nothing updates the graph: stop serving from
		// it. How the server shut down is of no interest.
		cancel()
		<-serveErr //nolint:errcheck
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("access provider failed: %w", err)
		}
		if parent.Err() == nil {
			return errors.New("access provider stopped")
		}
		return nil
	case err := <-serveErr:
		// Ends the provider, through the deferred cancel.
		return err
	}
}

// newOpenFGAResolver connects to OpenFGA and returns the resolver asking it.
//
// The connection is not encrypted: OpenFGA is expected next to this server,
// inside the cluster network.
//
// With the endpoint slices of the two exports the resolver reads kcp through
// their virtual workspaces. The providers finding the workspaces of the
// exports run until ctx is done and report to stopped if they end before.
func newOpenFGAResolver(ctx context.Context, restConfig *rest.Config, o *Options, stopped chan<- error) (accessprovider.AccessProvider, error) {
	conn, err := grpc.NewClient(o.OpenFGA.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to OpenFGA at %s: %w", o.OpenFGA.Addr, err)
	}

	if o.OpenFGA.Kubeconfig != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", o.OpenFGA.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("load --openfga-kubeconfig: %w", err)
		}
	}

	var directory openfgaprovider.Directory
	if o.OpenFGA.ThroughExports() {
		accounts, err := startExportProvider(ctx, restConfig, o.OpenFGA.AccountsEndpointSlice, stopped)
		if err != nil {
			return nil, err
		}
		stores, err := startExportProvider(ctx, restConfig, o.OpenFGA.StoresEndpointSlice, stopped)
		if err != nil {
			return nil, err
		}
		directory = openfgaprovider.NewExportDirectory(accounts, stores, o.OpenFGA.OrgsWorkspace)
	} else {
		klog.InfoS("openfgaprovider reads every workspace directly, which takes an administrator of kcp; " +
			"set --openfga-accounts-endpointslice and --openfga-stores-endpointslice to read through the APIExports instead")

		directory, err = openfgaprovider.NewKCPDirectory(restConfig, o.OpenFGA.OrgsWorkspace)
		if err != nil {
			return nil, fmt.Errorf("build kcp directory: %w", err)
		}
	}

	return openfgaprovider.New(openfgav1.NewOpenFGAServiceClient(conn), directory, openfgaprovider.Options{
		Relation:      o.OpenFGA.Relation,
		ObjectType:    o.OpenFGA.ObjectType,
		OrgsWorkspace: o.OpenFGA.OrgsWorkspace,
		EndpointBase:  o.EndpointBase,
	}), nil
}

// startExportProvider starts a provider of the workspaces that bind the
// APIExport of the endpoint slice, found by logical cluster name or path. It
// watches the APIBindings of the export through its virtual workspace and
// nothing else.
func startExportProvider(ctx context.Context, restConfig *rest.Config, endpointSlice string, stopped chan<- error) (*pathaware.Provider, error) {
	sch := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(sch))

	logger := klog.FromContext(ctx).WithName("openfgaprovider").WithValues("apiExportEndpointSlice", endpointSlice)

	provider, err := pathaware.New(rest.CopyConfig(restConfig), endpointSlice, apiexport.Options{Scheme: sch, Log: &logger})
	if err != nil {
		return nil, fmt.Errorf("construct provider of APIExportEndpointSlice %s: %w", endpointSlice, err)
	}

	go func() {
		err := provider.Start(ctx, nil)
		if err != nil {
			err = fmt.Errorf("provider of APIExportEndpointSlice %s: %w", endpointSlice, err)
		}
		stopped <- err
	}()

	return provider, nil
}

func pathScopedAuthorizer(path string, delegate authorizer.Authorizer) authorizer.Authorizer {
	return authorizer.AuthorizerFunc(func(ctx context.Context, attrs authorizer.Attributes) (authorizer.Decision, string, error) {
		if !attrs.IsResourceRequest() && attrs.GetPath() == path {
			return delegate.Authorize(ctx, attrs)
		}
		return authorizer.DecisionNoOpinion, "", nil
	})
}

// groupAuthorizer allows the members of one of the groups and has no opinion
// on anybody else.
func groupAuthorizer(groups []string) authorizer.Authorizer {
	allowed := sets.New(groups...)
	return authorizer.AuthorizerFunc(func(_ context.Context, attrs authorizer.Attributes) (authorizer.Decision, string, error) {
		if u := attrs.GetUser(); u != nil && allowed.HasAny(u.GetGroups()...) {
			return authorizer.DecisionAllow, "member of a debug group", nil
		}
		return authorizer.DecisionNoOpinion, "", nil
	})
}

type graphDebug struct {
	graph.Snapshot
	EngagedClusters        int    `json:"engagedClusters"`
	APIExportEndpointSlice string `json:"apiExportEndpointSlice,omitempty"`
}
