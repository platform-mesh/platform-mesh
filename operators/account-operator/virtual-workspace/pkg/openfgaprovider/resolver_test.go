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

package openfgaprovider

import (
	"context"
	"errors"
	"reflect"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/grpc"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/accessprovider"
	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/kcp-dev/logicalcluster/v3"
)

// fakeFGA answers ListObjects per store and user, and records the requests.
type fakeFGA struct {
	// store id -> user -> objects
	objects  map[string]map[string][]string
	err      error
	requests []*openfgav1.ListObjectsRequest
}

func (f *fakeFGA) ListObjects(_ context.Context, in *openfgav1.ListObjectsRequest, _ ...grpc.CallOption) (*openfgav1.ListObjectsResponse, error) {
	f.requests = append(f.requests, in)
	if f.err != nil {
		return nil, f.err
	}
	return &openfgav1.ListObjectsResponse{Objects: f.objects[in.GetStoreId()][in.GetUser()]}, nil
}

type fakeDirectory struct {
	paths      map[string]string         // cluster name -> path
	stores     map[string]string         // organization -> store id
	workspaces map[string]string         // parent/name -> cluster
	accounts   map[string]AccountDetails // parent/name -> details
	err        error
}

func (d *fakeDirectory) AccountDetails(_ context.Context, parent, name string) (AccountDetails, bool, error) {
	details, ok := d.accounts[parent+"/"+name]
	return details, ok, d.err
}

func (d *fakeDirectory) WorkspacePath(_ context.Context, cluster string) (logicalcluster.Path, bool, error) {
	path, ok := d.paths[cluster]
	return logicalcluster.NewPath(path), ok, d.err
}

func (d *fakeDirectory) StoreID(_ context.Context, org string) (string, bool, error) {
	id, ok := d.stores[org]
	return id, ok, d.err
}

func (d *fakeDirectory) WorkspaceCluster(_ context.Context, parent, name string) (string, bool, error) {
	cluster, ok := d.workspaces[parent+"/"+name]
	return cluster, ok, d.err
}

const account = DefaultObjectType + ":"

// An organization acme (cluster acme-id, in orgs-id) with the accounts team-a
// and team-b below it, and a second organization other.
func fixture() (*fakeFGA, *fakeDirectory) {
	fga := &fakeFGA{objects: map[string]map[string][]string{
		"store-acme": {
			"user:owner@acme.io":  {account + "orgs-id/acme", account + "acme-id/team-b", account + "acme-id/team-a"},
			"user:member@acme.io": {account + "acme-id/team-a"},
		},
		"store-other": {
			"user:owner@acme.io": {account + "orgs-id/other"},
		},
	}}
	directory := &fakeDirectory{
		paths: map[string]string{
			"orgs-id":   "root:orgs",
			"acme-id":   "root:orgs:acme",
			"team-a-id": "root:orgs:acme:team-a",
			"infra-id":  "root:platform-mesh-system",
		},
		stores: map[string]string{"acme": "store-acme", "other": "store-other"},
		workspaces: map[string]string{
			"orgs-id/acme":   "acme-id",
			"orgs-id/other":  "other-id",
			"acme-id/team-a": "team-a-id",
			"acme-id/team-b": "team-b-id",
		},
		// team-b has a workspace but its Account cannot be read.
		accounts: map[string]AccountDetails{
			"orgs-id/acme":   {DisplayName: "Acme Inc.", Type: "org", Ready: true},
			"acme-id/team-a": {DisplayName: "Team A", Type: "account"},
		},
	}
	return fga, directory
}

func names(clusters []graph.AccessEndpointSlice) []string {
	out := make([]string, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, c.ClusterName)
	}
	return out
}

func TestClustersFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		user    string
		cluster string
		want    []string
	}{
		{name: "owner sees the organization and its accounts", user: "owner@acme.io", cluster: "root:orgs:acme", want: []string{"acme-id", "team-a-id", "team-b-id"}},
		{name: "member of one account sees only that", user: "member@acme.io", cluster: "root:orgs:acme", want: []string{"team-a-id"}},
		{name: "stranger sees nothing", user: "nobody@example.com", cluster: "root:orgs:acme", want: []string{}},
		{name: "a workspace below the organization gives the same answer", user: "owner@acme.io", cluster: "root:orgs:acme:team-a", want: []string{"acme-id", "team-a-id", "team-b-id"}},
		{name: "scope by cluster name of the organization", user: "member@acme.io", cluster: "acme-id", want: []string{"team-a-id"}},
		{name: "scope by cluster name of an account", user: "member@acme.io", cluster: "team-a-id", want: []string{"team-a-id"}},
		{name: "the answer stays inside the organization asked for", user: "owner@acme.io", cluster: "root:orgs:other", want: []string{"other-id"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fga, directory := fixture()
			r := New(fga, directory, Options{EndpointBase: "https://kcp.example/clusters"})

			got, err := r.ClustersFor(context.Background(), accessprovider.Identity{Name: tc.user}, tc.cluster)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(names(got), tc.want) {
				t.Errorf("clusters = %v, want %v", names(got), tc.want)
			}
			for _, c := range got {
				if want := "https://kcp.example/clusters/" + c.ClusterName; c.Endpoint != want {
					t.Errorf("endpoint of %s = %q, want %q", c.ClusterName, c.Endpoint, want)
				}
			}
		})
	}
}

func TestClustersFor_DescribesTheAccounts(t *testing.T) {
	t.Parallel()

	fga, directory := fixture()
	r := New(fga, directory, Options{})

	got, err := r.ClustersFor(context.Background(), accessprovider.Identity{Name: "owner@acme.io"}, "root:orgs:acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]graph.Account{
		"acme-id":   {Name: "acme", Path: "root:orgs:acme", DisplayName: "Acme Inc.", Type: "org", Ready: true},
		"team-a-id": {Name: "team-a", Path: "root:orgs:acme:team-a", DisplayName: "Team A", Type: "account"},
		// Listed by name and path where the Account is not there.
		"team-b-id": {Name: "team-b", Path: "root:orgs:acme:team-b"},
	}
	if len(got) != len(want) {
		t.Fatalf("clusters = %v, want %d", names(got), len(want))
	}
	for _, c := range got {
		if c.Account == nil || *c.Account != want[c.ClusterName] {
			t.Errorf("account of %s = %+v, want %+v", c.ClusterName, c.Account, want[c.ClusterName])
		}
	}
}

func TestClustersFor_AsksOneStoreWithConfiguredRelation(t *testing.T) {
	t.Parallel()

	fga, directory := fixture()
	r := New(fga, directory, Options{Relation: "update", ObjectType: DefaultObjectType})

	if _, err := r.ClustersFor(context.Background(), accessprovider.Identity{Name: "system:serviceaccount:ns:sa", Groups: []string{"team"}}, "root:orgs:acme"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fga.requests) != 1 {
		t.Fatalf("expected one ListObjects call, got %d", len(fga.requests))
	}
	req := fga.requests[0]
	if req.GetStoreId() != "store-acme" || req.GetRelation() != "update" || req.GetType() != DefaultObjectType {
		t.Errorf("unexpected request: %v", req)
	}
	// One colon is all OpenFGA allows in a user; the tuples are written the same way.
	if want := "user:system.serviceaccount.ns.sa"; req.GetUser() != want {
		t.Errorf("user = %q, want %q", req.GetUser(), want)
	}
}

func TestClustersFor_OutsideAnOrganization(t *testing.T) {
	t.Parallel()

	for _, cluster := range []string{"root", "root:orgs", "root:platform-mesh-system", "infra-id", "unknown-id", "root:orgs:no-store"} {
		fga, directory := fixture()
		r := New(fga, directory, Options{})

		got, err := r.ClustersFor(context.Background(), accessprovider.Identity{Name: "owner@acme.io"}, cluster)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", cluster, err)
		}
		if len(got) != 0 {
			t.Errorf("%s: expected nothing, got %v", cluster, names(got))
		}
		if len(fga.requests) != 0 {
			t.Errorf("%s: OpenFGA must not be asked, got %d calls", cluster, len(fga.requests))
		}
	}
}

func TestClustersFor_NeedsAScope(t *testing.T) {
	t.Parallel()

	for _, cluster := range []string{"", "*"} {
		fga, directory := fixture()
		r := New(fga, directory, Options{})

		_, err := r.ClustersFor(context.Background(), accessprovider.Identity{Name: "owner@acme.io"}, cluster)
		if !apierrors.IsBadRequest(err) {
			t.Errorf("scope %q: expected BadRequest, got %v", cluster, err)
		}
	}
}

func TestClustersFor_SkipsAccountsWithoutWorkspace(t *testing.T) {
	t.Parallel()

	fga, directory := fixture()
	delete(directory.workspaces, "acme-id/team-b")
	fga.objects["store-acme"]["user:owner@acme.io"] = append(fga.objects["store-acme"]["user:owner@acme.io"], "role:something", account+"malformed")
	r := New(fga, directory, Options{})

	got, err := r.ClustersFor(context.Background(), accessprovider.Identity{Name: "owner@acme.io"}, "root:orgs:acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"acme-id", "team-a-id"}; !reflect.DeepEqual(names(got), want) {
		t.Errorf("clusters = %v, want %v", names(got), want)
	}
}

func TestClustersFor_FailsRatherThanAnswerPartially(t *testing.T) {
	t.Parallel()

	t.Run("OpenFGA fails", func(t *testing.T) {
		t.Parallel()

		fga, directory := fixture()
		fga.err = errors.New("unavailable")
		r := New(fga, directory, Options{})

		_, err := r.ClustersFor(context.Background(), accessprovider.Identity{Name: "owner@acme.io"}, "root:orgs:acme")
		if !apierrors.IsServiceUnavailable(err) {
			t.Errorf("expected ServiceUnavailable, got %v", err)
		}
	})

	t.Run("kcp fails", func(t *testing.T) {
		t.Parallel()

		fga, directory := fixture()
		directory.err = errors.New("forbidden")
		r := New(fga, directory, Options{})

		_, err := r.ClustersFor(context.Background(), accessprovider.Identity{Name: "owner@acme.io"}, "root:orgs:acme")
		if !apierrors.IsServiceUnavailable(err) {
			t.Errorf("expected ServiceUnavailable, got %v", err)
		}
	})
}

func TestParseAccount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		object     string
		wantParent string
		wantName   string
		wantOK     bool
	}{
		{object: account + "2l1fa72w238uue15/bob", wantParent: "2l1fa72w238uue15", wantName: "bob", wantOK: true},
		{object: account + "parent/a%3Ab", wantParent: "parent", wantName: "a:b", wantOK: true},
		{object: account + "noslash"},
		{object: account + "/name"},
		{object: account + "parent/"},
		{object: "role:parent/name"},
	}
	for _, tc := range tests {
		parent, name, ok := parseAccount(DefaultObjectType, tc.object)
		if ok != tc.wantOK || parent != tc.wantParent || name != tc.wantName {
			t.Errorf("parseAccount(%q) = %q, %q, %v; want %q, %q, %v", tc.object, parent, name, ok, tc.wantParent, tc.wantName, tc.wantOK)
		}
	}
}
