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
	"sync"
	"testing"
	"time"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/accessprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"
	accessv1alpha1 "go.platform-mesh.io/account-operator/virtual-workspace/sdk/apis/access/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/watch"
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
		// Not a workspace path or logical cluster name: would rewrite the URL
		// of the lookups in kcp.
		{path: "/services/access/clusters/../apis", wantOK: false},
		{path: "/services/access/clusters/root?x=1/apis", wantOK: false},
		{path: "/services/access/clusters/root#frag/apis", wantOK: false},
		{path: "/services/access/clusters/Root:Orgs/apis", wantOK: false},
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

// changingProvider answers with whatever clusters it was last set to.
type changingProvider struct {
	mu       sync.Mutex
	clusters []graph.AccessEndpointSlice
	err      error
}

func (p *changingProvider) Ready() bool { return true }

func (p *changingProvider) ClustersFor(context.Context, accessprovider.Identity, string) ([]graph.AccessEndpointSlice, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clusters, p.err
}

func (p *changingProvider) set(err error, names ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
	p.clusters = nil
	for _, name := range names {
		p.clusters = append(p.clusters, graph.AccessEndpointSlice{ClusterName: name, Endpoint: "https://kcp.example/clusters/" + name})
	}
}

func nextEvent(t *testing.T, w watch.Interface) watch.Event {
	t.Helper()
	select {
	case event, ok := <-w.ResultChan():
		if !ok {
			t.Fatal("watch closed")
		}
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("no watch event")
	}
	return watch.Event{}
}

func clusterNames(t *testing.T, event watch.Event) []string {
	t.Helper()
	review, ok := event.Object.(*accessv1alpha1.SelfClusterAccessReview)
	if !ok {
		t.Fatalf("event %s carries %T", event.Type, event.Object)
	}
	names := make([]string, 0, len(review.Status.Clusters))
	for _, c := range review.Status.Clusters {
		names = append(names, c.ClusterName)
	}
	return names
}

func TestList_ReturnsTheCallersReview(t *testing.T) {
	t.Parallel()

	provider := &changingProvider{}
	provider.set(nil, "ws1", "ws2")
	r := NewREST(provider)
	ctx := genericapirequest.WithUser(context.Background(), &user.DefaultInfo{Name: "alice"})

	obj, err := r.List(ctx, &metainternalversion.ListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list := obj.(*accessv1alpha1.SelfClusterAccessReviewList)
	if len(list.Items) != 1 || list.Items[0].Name != SelfName || len(list.Items[0].Status.Clusters) != 2 {
		t.Fatalf("expected one review %q with 2 clusters, got %+v", SelfName, list.Items)
	}
	if list.ResourceVersion == "" || list.ResourceVersion != list.Items[0].ResourceVersion {
		t.Errorf("list version %q, item version %q", list.ResourceVersion, list.Items[0].ResourceVersion)
	}

	again, _ := r.List(ctx, &metainternalversion.ListOptions{})
	if got := again.(*accessv1alpha1.SelfClusterAccessReviewList).ResourceVersion; got != list.ResourceVersion {
		t.Errorf("version changed without a change: %q, then %q", list.ResourceVersion, got)
	}

	provider.set(nil, "ws1")
	changed, _ := r.List(ctx, &metainternalversion.ListOptions{})
	if got := changed.(*accessv1alpha1.SelfClusterAccessReviewList).ResourceVersion; got == list.ResourceVersion {
		t.Errorf("version %q did not change with the answer", got)
	}

	if _, err := r.List(context.Background(), &metainternalversion.ListOptions{}); !apierrors.IsUnauthorized(err) {
		t.Errorf("expected Unauthorized without a user, got %v", err)
	}
}

func TestWatch(t *testing.T) {
	t.Parallel()

	provider := &changingProvider{}
	provider.set(nil, "ws1")
	r := NewREST(provider)
	r.interval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(genericapirequest.WithUser(context.Background(), &user.DefaultInfo{Name: "alice"}))
	defer cancel()

	// Without a version the watch starts with the review.
	w, err := r.Watch(ctx, &metainternalversion.ListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	event := nextEvent(t, w)
	if event.Type != watch.Added || !reflect.DeepEqual(clusterNames(t, event), []string{"ws1"}) {
		t.Fatalf("expected ADDED with ws1, got %s %v", event.Type, clusterNames(t, event))
	}

	provider.set(nil, "ws1", "ws2")
	event = nextEvent(t, w)
	if event.Type != watch.Modified || !reflect.DeepEqual(clusterNames(t, event), []string{"ws1", "ws2"}) {
		t.Fatalf("expected MODIFIED with ws1 and ws2, got %s %v", event.Type, clusterNames(t, event))
	}
	version := event.Object.(*accessv1alpha1.SelfClusterAccessReview).ResourceVersion
	w.Stop()

	// From the current version the watch reports only what changes.
	w, err = r.Watch(ctx, &metainternalversion.ListOptions{ResourceVersion: version})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	provider.set(nil)
	event = nextEvent(t, w)
	if event.Type != watch.Modified || len(clusterNames(t, event)) != 0 {
		t.Fatalf("expected MODIFIED with no clusters, got %s %v", event.Type, clusterNames(t, event))
	}

	// A provider error ends the watch with an error event.
	provider.set(apierrors.NewServiceUnavailable("openfga is down"))
	event = nextEvent(t, w)
	if event.Type != watch.Error || !apierrors.IsServiceUnavailable(apierrors.FromObject(event.Object)) {
		t.Fatalf("expected the provider's error, got %s %+v", event.Type, event.Object)
	}
	if _, ok := <-w.ResultChan(); ok {
		t.Error("watch stayed open after an error")
	}

	// A version that is not the current answer expired.
	provider.set(nil, "ws3")
	if _, err := r.Watch(ctx, &metainternalversion.ListOptions{ResourceVersion: version}); !apierrors.IsResourceExpired(err) {
		t.Errorf("expected Expired for a stale version, got %v", err)
	}
}
