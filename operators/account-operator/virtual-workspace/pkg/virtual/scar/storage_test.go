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
	"reflect"
	"testing"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/accessprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"
	accessv1alpha1 "go.platform-mesh.io/account-operator/virtual-workspace/sdk/apis/access/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apiserver/pkg/authentication/user"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func TestCreate_GraphNotReady(t *testing.T) {
	t.Parallel()

	g := graph.New() // not ready
	r := NewREST(accessprovider.GraphAdapter{Graph: g})

	ctx := genericapirequest.WithUser(context.Background(), &user.DefaultInfo{Name: "alice"})
	_, err := r.Create(ctx, &accessv1alpha1.SelfClusterAccessReview{}, nil, nil)
	if !apierrors.IsServiceUnavailable(err) {
		t.Fatalf("expected ServiceUnavailable, got %v", err)
	}
}

func TestCreate_NoUser(t *testing.T) {
	t.Parallel()

	g := graph.New()
	g.SetReady()
	r := NewREST(accessprovider.GraphAdapter{Graph: g})

	_, err := r.Create(context.Background(), &accessv1alpha1.SelfClusterAccessReview{}, nil, nil)
	if !apierrors.IsUnauthorized(err) {
		t.Fatalf("expected Unauthorized, got %v", err)
	}
}

func TestCreate_ReturnsCallerClusters(t *testing.T) {
	t.Parallel()

	g := graph.New()
	g.Grant(graph.User("alice"), graph.LogicalCluster("ws1"), "https://kcp.example/clusters/ws1")
	g.Grant(graph.Group("team-a"), graph.LogicalCluster("ws2"), "https://kcp.example/clusters/ws2")
	g.Grant(graph.User("bob"), graph.LogicalCluster("ws3"), "https://kcp.example/clusters/ws3")
	g.SetReady()
	r := NewREST(accessprovider.GraphAdapter{Graph: g})

	ctx := genericapirequest.WithUser(context.Background(), &user.DefaultInfo{
		Name:   "alice",
		Groups: []string{"team-a"},
	})
	obj, err := r.Create(ctx, &accessv1alpha1.SelfClusterAccessReview{}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	review := obj.(*accessv1alpha1.SelfClusterAccessReview)
	if got := len(review.Status.Clusters); got != 2 {
		t.Fatalf("expected 2 clusters (user + group grant), got %d: %+v", got, review.Status.Clusters)
	}
	names := map[string]bool{}
	for _, c := range review.Status.Clusters {
		names[c.ClusterName] = true
	}
	if !names["ws1"] || !names["ws2"] {
		t.Errorf("expected ws1 and ws2, got %+v", review.Status.Clusters)
	}
	if names["ws3"] {
		t.Errorf("bob's ws3 leaked into alice's result: %+v", review.Status.Clusters)
	}
}

// scopedProvider records the scope and the identity it was asked for.
type scopedProvider struct {
	scope    string
	identity accessprovider.Identity
	err      error
}

func (r *scopedProvider) Ready() bool { return true }

func (r *scopedProvider) ClustersFor(_ context.Context, id accessprovider.Identity, cluster string) ([]graph.AccessEndpointSlice, error) {
	r.scope = cluster
	r.identity = id
	return nil, r.err
}

func TestCreate_PassesScopeAndIdentityToProvider(t *testing.T) {
	t.Parallel()

	provider := &scopedProvider{}
	r := NewREST(provider)

	ctx := genericapirequest.WithUser(context.Background(), &user.DefaultInfo{
		Name:   "alice",
		Groups: []string{"devs"},
		Extra:  map[string][]string{"tenant": {"acme"}},
	})
	ctx = WithCluster(ctx, "root:orgs:acme")
	if _, err := r.Create(ctx, &accessv1alpha1.SelfClusterAccessReview{}, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.scope != "root:orgs:acme" {
		t.Errorf("provider got scope %q, want root:orgs:acme", provider.scope)
	}
	want := accessprovider.Identity{Name: "alice", Groups: []string{"devs"}, Extra: map[string][]string{"tenant": {"acme"}}}
	if !reflect.DeepEqual(provider.identity, want) {
		t.Errorf("provider got identity %+v, want %+v", provider.identity, want)
	}
}

func TestCreate_ReturnsProviderError(t *testing.T) {
	t.Parallel()

	r := NewREST(&scopedProvider{err: apierrors.NewBadRequest("no scope")})

	ctx := genericapirequest.WithUser(context.Background(), &user.DefaultInfo{Name: "alice"})
	_, err := r.Create(ctx, &accessv1alpha1.SelfClusterAccessReview{}, nil, nil)
	if !apierrors.IsBadRequest(err) {
		t.Fatalf("expected the provider's BadRequest, got %v", err)
	}
}

func TestResolveRootPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path        string
		wantOK      bool
		wantPrefix  string
		wantCluster string
	}{
		{path: "/services/access", wantOK: true, wantPrefix: "/services/access"},
		{path: "/services/access/apis/access.platform-mesh.io/v1alpha1", wantOK: true, wantPrefix: "/services/access"},
		{
			path:        "/services/access/clusters/root:orgs:acme/apis/access.platform-mesh.io/v1alpha1/selfclusteraccessreviews",
			wantOK:      true,
			wantPrefix:  "/services/access/clusters/root:orgs:acme",
			wantCluster: "root:orgs:acme",
		},
		{path: "/services/access/clusters/2la74aji095sye06", wantOK: true, wantPrefix: "/services/access/clusters/2la74aji095sye06", wantCluster: "2la74aji095sye06"},
		{path: "/services/access/clusters/", wantOK: false},
		{path: "/services/accessible", wantOK: false},
		{path: "/services/other/clusters/root", wantOK: false},
	}
	for _, tc := range tests {
		ok, prefix, ctx := resolveRootPath(tc.path, context.Background())
		if ok != tc.wantOK || prefix != tc.wantPrefix {
			t.Errorf("resolveRootPath(%q) = %v, %q; want %v, %q", tc.path, ok, prefix, tc.wantOK, tc.wantPrefix)
		}
		if got := ClusterFrom(ctx); got != tc.wantCluster {
			t.Errorf("resolveRootPath(%q) cluster = %q, want %q", tc.path, got, tc.wantCluster)
		}
	}
}
