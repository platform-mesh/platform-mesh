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
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/virtual"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/virtual/scar"
	accessv1alpha1 "go.platform-mesh.io/account-operator/virtual-workspace/sdk/apis/access/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/union"
	openapinamer "k8s.io/apiserver/pkg/endpoints/openapi"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"

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
func Run(ctx context.Context, o *Options) error {
	if err := o.Complete(); err != nil {
		return err
	}
	if err := o.Validate(); err != nil {
		return err
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}

	// Where the answers come from: one access provider per --provider, merged
	// if there are several. debug is what /debug/graph shows, nil when no
	// provider keeps a graph; providerErr reports a provider that stopped, and
	// stays silent for one that runs nothing in the background.
	var (
		providers   accessprovider.Union
		debug       func() any
		providerErr = make(chan error, 1)
	)

	for _, name := range o.Providers() {
		switch name {
		case ProviderOpenFGA:
			resolver, err := newOpenFGAResolver(restConfig, o)
			if err != nil {
				return err
			}
			providers = append(providers, resolver)

			klog.InfoS("openfgaprovider running", "openfga", o.OpenFGA.Addr, "relation", o.OpenFGA.Relation,
				"objectType", o.OpenFGA.ObjectType, "orgsWorkspace", o.OpenFGA.OrgsWorkspace, "kubeconfig", o.Kubeconfig)

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

	// Outside the VW authorizers, so granted explicitly.
	recommended.Authorization.Authorizer = union.New(
		recommended.Authorization.Authorizer,
		pathScopedAuthorizer(debugGraphPath, virtual.AuthenticatedOnlyAuthorizer()),
	)

	completed := rootCfg.Complete()
	rootServer, err := rootapiserver.NewServer(completed, genericapiserver.NewEmptyDelegate())
	if err != nil {
		return fmt.Errorf("create root apiserver: %w", err)
	}

	// Debug endpoint: pretty-printed snapshot of the access graph, for a
	// provider that keeps one.
	if debug != nil {
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
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("access provider failed: %w", err)
		}
		// The provider is gone, which ends the server; how it shut down is of no interest.
		<-serveErr //nolint:errcheck
		return nil
	case err := <-serveErr:
		return err
	}
}

// newOpenFGAResolver connects to OpenFGA and returns the resolver asking it.
//
// The connection is not encrypted: OpenFGA is expected next to this server,
// inside the cluster network.
func newOpenFGAResolver(restConfig *rest.Config, o *Options) (accessprovider.AccessProvider, error) {
	conn, err := grpc.NewClient(o.OpenFGA.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to OpenFGA at %s: %w", o.OpenFGA.Addr, err)
	}

	directory, err := openfgaprovider.NewKCPDirectory(restConfig, o.OpenFGA.OrgsWorkspace)
	if err != nil {
		return nil, fmt.Errorf("build kcp directory: %w", err)
	}

	return openfgaprovider.New(openfgav1.NewOpenFGAServiceClient(conn), directory, openfgaprovider.Options{
		Relation:      o.OpenFGA.Relation,
		ObjectType:    o.OpenFGA.ObjectType,
		OrgsWorkspace: o.OpenFGA.OrgsWorkspace,
		EndpointBase:  o.EndpointBase,
	}), nil
}

func pathScopedAuthorizer(path string, delegate authorizer.Authorizer) authorizer.Authorizer {
	return authorizer.AuthorizerFunc(func(ctx context.Context, attrs authorizer.Attributes) (authorizer.Decision, string, error) {
		if !attrs.IsResourceRequest() && attrs.GetPath() == path {
			return delegate.Authorize(ctx, attrs)
		}
		return authorizer.DecisionNoOpinion, "", nil
	})
}

type graphDebug struct {
	graph.Snapshot
	EngagedClusters        int    `json:"engagedClusters"`
	APIExportEndpointSlice string `json:"apiExportEndpointSlice,omitempty"`
}
