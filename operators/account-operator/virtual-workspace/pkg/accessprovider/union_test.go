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

package accessprovider

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"
)

type fakeProvider struct {
	notReady bool
	clusters []graph.AccessEndpointSlice
	err      error
	asked    []Identity
}

func (p *fakeProvider) Ready() bool { return !p.notReady }

func (p *fakeProvider) ClustersFor(_ context.Context, id Identity, _ string) ([]graph.AccessEndpointSlice, error) {
	p.asked = append(p.asked, id)
	return p.clusters, p.err
}

func TestUnion_MergesAndDedupes(t *testing.T) {
	t.Parallel()

	account := &graph.Account{Name: "team-a", Path: "root:orgs:acme:team-a"}
	rbac := &fakeProvider{clusters: []graph.AccessEndpointSlice{
		{ClusterName: "team-a-id", Endpoint: "https://kcp/clusters/team-a-id"},
		{ClusterName: "z-id", Endpoint: "https://kcp/clusters/z-id"},
	}}
	fga := &fakeProvider{clusters: []graph.AccessEndpointSlice{
		{ClusterName: "acme-id", Endpoint: "https://kcp/clusters/acme-id"},
		{ClusterName: "team-a-id", Endpoint: "https://kcp/clusters/team-a-id", Account: account},
	}}

	id := Identity{Name: "alice", Groups: []string{"devs"}, Extra: map[string][]string{"tenant": {"acme"}}}
	got, err := Union{rbac, fga}.ClustersFor(context.Background(), id, "root:orgs:acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []graph.AccessEndpointSlice{
		{ClusterName: "acme-id", Endpoint: "https://kcp/clusters/acme-id"},
		{ClusterName: "team-a-id", Endpoint: "https://kcp/clusters/team-a-id", Account: account},
		{ClusterName: "z-id", Endpoint: "https://kcp/clusters/z-id"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("clusters = %+v, want %+v", got, want)
	}
	for _, p := range []*fakeProvider{rbac, fga} {
		if len(p.asked) != 1 || !reflect.DeepEqual(p.asked[0], id) {
			t.Errorf("provider was asked for %+v, want the identity once", p.asked)
		}
	}
}

func TestUnion_FailsClosed(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	ok := &fakeProvider{clusters: []graph.AccessEndpointSlice{{ClusterName: "a"}}}

	got, err := Union{ok, &fakeProvider{err: boom}}.ClustersFor(context.Background(), Identity{Name: "alice"}, "")
	if !errors.Is(err, boom) || got != nil {
		t.Errorf("got %v, %v; want no clusters and the provider's error", got, err)
	}

	if (Union{ok, &fakeProvider{notReady: true}}).Ready() {
		t.Error("a union with a provider that is not ready must not be ready")
	}
	if !(Union{ok, ok}).Ready() {
		t.Error("a union of ready providers must be ready")
	}
}
