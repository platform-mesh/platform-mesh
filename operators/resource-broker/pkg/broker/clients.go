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

package broker

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/controller/brokeredresource"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
	mcpclient "github.com/kcp-dev/multicluster-provider/client"
	mcpcache "github.com/kcp-dev/multicluster-provider/pkg/cache"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// workspaceClientFn returns a client scoped to the workspace with the given
// path.
type workspaceClientFn func(path string) (ctrlruntimeclient.Client, error)

// newScheme returns the scheme shared by all broker managers and clients.
func newScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		scheme.AddToScheme,
		kcptenancyv1alpha1.AddToScheme,
		kcpapisv1alpha1.AddToScheme,
		kcpapisv1alpha2.AddToScheme,
		pmbrokerv1alpha1.AddToScheme,
		pmcoordbrokerv1alpha1.AddToScheme,
	} {
		if err := add(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// baseConfig returns a copy of cfg with any /clusters/<path> suffix stripped
// from the host.
func baseConfig(cfg *rest.Config) (*rest.Config, error) {
	base := rest.CopyConfig(cfg)
	u, err := url.Parse(base.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing host %q: %w", base.Host, err)
	}
	if idx := strings.Index(u.Path, "/clusters/"); idx >= 0 {
		u.Path = u.Path[:idx]
		base.Host = u.String()
	}
	return base, nil
}

// configForClusterPath returns a copy of cfg with the host pointing at the
// workspace with the given path.
func configForClusterPath(cfg *rest.Config, path string) (*rest.Config, error) {
	base, err := baseConfig(cfg)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(base.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing host %q: %w", base.Host, err)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/clusters/" + path
	base.Host = u.String()
	return base, nil
}

// workspaceClientFunc returns a factory for workspace-scoped clients based on
// cfg.
func workspaceClientFunc(cfg *rest.Config, s *runtime.Scheme) (workspaceClientFn, error) {
	base, err := baseConfig(cfg)
	if err != nil {
		return nil, err
	}
	clusterClient, err := mcpclient.New(base, ctrlruntimeclient.Options{Scheme: s})
	if err != nil {
		return nil, fmt.Errorf("building cluster client: %w", err)
	}
	return func(path string) (ctrlruntimeclient.Client, error) {
		return clusterClient.Cluster(logicalcluster.NewPath(path)), nil
	}, nil
}

// listAcceptAPIs returns a func listing all AcceptAPIs known to the given
// lister together with their provider clusters.
func listAcceptAPIs(lister mcpcache.Lister) func(ctx context.Context) ([]brokeredresource.AcceptAPIRef, error) {
	return func(ctx context.Context) ([]brokeredresource.AcceptAPIRef, error) {
		list := &pmbrokerv1alpha1.AcceptAPIList{}
		if err := lister.List(ctx, list); err != nil {
			return nil, fmt.Errorf("listing AcceptAPIs: %w", err)
		}
		refs := make([]brokeredresource.AcceptAPIRef, 0, len(list.Items))
		for i := range list.Items {
			item := &list.Items[i]
			refs = append(refs, brokeredresource.AcceptAPIRef{
				Cluster:   logicalcluster.From(item).String(),
				AcceptAPI: item,
			})
		}
		return refs, nil
	}
}
