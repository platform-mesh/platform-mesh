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

package scar

import (
	"context"
	"errors"
	"strings"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/accessprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/virtual"
	accessv1alpha1 "go.platform-mesh.io/account-operator/virtual-workspace/sdk/apis/access/v1alpha1"

	restStorage "k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"

	"github.com/kcp-dev/virtual-workspace-framework/framework"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/fixedgvs"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/rootapiserver"
)

const VirtualWorkspaceName = "access"

// RootPath is the URL prefix the front-proxy routes to this VW.
// SCAR is served at RootPath + /apis/access.platform-mesh.io/v1alpha1/selfclusteraccessreviews,
// and scoped to a workspace at RootPath + /clusters/<cluster> + /apis/....
const RootPath = "/services/" + VirtualWorkspaceName

// clustersSegment separates RootPath from the workspace a request is scoped to.
const clustersSegment = "/clusters/"

type clusterKey struct{}

// WithCluster returns a context carrying the workspace a request was scoped
// to by its URL.
func WithCluster(ctx context.Context, cluster string) context.Context {
	return context.WithValue(ctx, clusterKey{}, cluster)
}

// ClusterFrom returns the workspace (path or logical cluster name) the
// request was scoped to, and "" for a request that was not scoped.
func ClusterFrom(ctx context.Context) string {
	cluster, _ := ctx.Value(clusterKey{}).(string)
	return cluster
}

// resolveRootPath accepts the URLs this VW serves and returns the prefix the
// framework strips before routing. For a scoped URL, the cluster is part of
// that prefix and is put into the context.
//
// The front-proxy authenticates a scoped request with the authenticators of
// that workspace, when its path mapping has the {cluster} placeholder; an
// unscoped request only with the global ones.
func resolveRootPath(urlPath string, ctx context.Context) (bool, string, context.Context) { //nolint:revive // signature of framework.RootPathResolverFunc
	if urlPath != RootPath && !strings.HasPrefix(urlPath, RootPath+"/") {
		return false, "", ctx
	}

	rest, scoped := strings.CutPrefix(urlPath, RootPath+clustersSegment)
	if !scoped {
		return true, RootPath, ctx
	}

	cluster, _, _ := strings.Cut(rest, "/")
	if cluster == "" {
		// /services/access/clusters/ names no cluster: nothing is served there.
		return false, "", ctx
	}

	return true, RootPath + clustersSegment + cluster, WithCluster(ctx, cluster)
}

// NewVirtualWorkspace builds the Access VW: a fixed-group-version
// delegated apiserver serving the access.platform-mesh.io/v1alpha1 API with the
// selfclusteraccessreviews REST storage, answered by the given provider.
func NewVirtualWorkspace(provider accessprovider.AccessProvider) rootapiserver.NamedVirtualWorkspace {
	vw := &fixedgvs.FixedGroupVersionsVirtualWorkspace{
		RootPathResolver: framework.RootPathResolverFunc(resolveRootPath),
		Authorizer:       virtual.AuthenticatedOnlyAuthorizer(),
		ReadyChecker: framework.ReadyFunc(func() error {
			if !provider.Ready() {
				return errors.New("access provider is not ready")
			}
			return nil
		}),
		GroupVersionAPISets: []fixedgvs.GroupVersionAPISet{
			{
				GroupVersion: accessv1alpha1.SchemeGroupVersion,
				AddToScheme:  accessv1alpha1.AddToScheme,
				BootstrapRestResources: func(_ genericapiserver.CompletedConfig) (map[string]fixedgvs.RestStorageBuilder, error) {
					return map[string]fixedgvs.RestStorageBuilder{
						"selfclusteraccessreviews": func(_ genericapiserver.CompletedConfig) (restStorage.Storage, error) {
							return NewREST(provider), nil
						},
					}, nil
				},
			},
		},
	}

	return rootapiserver.NamedVirtualWorkspace{
		Name:             VirtualWorkspaceName,
		VirtualWorkspace: &virtual.WithoutAdmission{CoreVirtualWorkspace: vw},
	}
}
