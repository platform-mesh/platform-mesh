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

package rbacprovider

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/accessprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var _ accessprovider.GraphPopulator = (*Provider)(nil)

// Provider is the kcp-native RBAC AccessProvider. It watches
// ClusterRoleBindings and RoleBindings and projects them onto the access
// graph, treating any binding as "view" — rule-level evaluation against
// (Cluster)Roles is not implemented.
//
// Start picks a mode from the fields set: multi-shard when RestConfig and
// APIExportEndpointSlice are both set, single-shard with RestConfig alone, and
// a no-op stub when RestConfig is nil.
type Provider struct {
	// EndpointBaseURL is the front-proxy URL prefix; a cluster's endpoint is
	// this plus the cluster name.
	EndpointBaseURL string

	// RestConfig must reach the kcp root shard in multi-shard mode, so the
	// apiexport virtual workspace is addressable.
	RestConfig *rest.Config

	// APIExportEndpointSlice names the slice the provider follows to discover
	// workspaces.
	APIExportEndpointSlice string

	translator *Translator
	engaged    clusterSet
}

// New returns a configured Provider in stub mode.
func New(endpointBaseURL string) *Provider {
	return &Provider{EndpointBaseURL: endpointBaseURL}
}

// EngagedClusters returns how many logical clusters the provider is watching.
// Zero on a ready graph means discovery found nothing, which is distinct from
// finding clusters that hold no bindings.
func (p *Provider) EngagedClusters() int {
	return p.engaged.len()
}

type clusterSet struct {
	mu       sync.Mutex
	clusters map[graph.LogicalCluster]struct{}
}

func (s *clusterSet) add(c graph.LogicalCluster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clusters == nil {
		s.clusters = make(map[graph.LogicalCluster]struct{})
	}
	s.clusters[c] = struct{}{}
}

func (s *clusterSet) remove(c graph.LogicalCluster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clusters, c)
}

func (s *clusterSet) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clusters)
}

// Start implements accessprovider.GraphPopulator. It dispatches to
// one of three execution modes (multi-shard, single-shard, stub).
func (p *Provider) Start(ctx context.Context, g *graph.Graph) error {
	p.translator = NewTranslator(g)

	switch {
	case p.RestConfig != nil && p.APIExportEndpointSlice != "":
		return p.runMulticluster(ctx, p.RestConfig, g)

	case p.RestConfig != nil:
		client, err := kubernetes.NewForConfig(p.RestConfig)
		if err != nil {
			return fmt.Errorf("build kubernetes client: %w", err)
		}
		return p.runInformers(ctx, client, g)

	default:
		g.SetReady()
		<-ctx.Done()
		return nil
	}
}

func (p *Provider) endpointFor(c graph.LogicalCluster) string {
	base := p.EndpointBaseURL
	if base != "" && !strings.HasSuffix(base, "/") {
		base += "/"
	}

	return base + string(c)
}
