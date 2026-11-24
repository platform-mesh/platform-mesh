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
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

// fakeKCP serves the objects registered by request path and counts requests.
type fakeKCP struct {
	mu      sync.Mutex
	objects map[string]string
	hits    map[string]int
}

func (f *fakeKCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.hits[r.URL.Path]++
	w.Header().Set("Content-Type", "application/json")

	body, ok := f.objects[r.URL.Path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		return
	}
	_, _ = fmt.Fprint(w, body)
}

func newTestDirectory(t *testing.T) (*KCPDirectory, *fakeKCP) {
	t.Helper()

	kcp := &fakeKCP{hits: map[string]int{}, objects: map[string]string{
		"/clusters/acme-id/apis/core.kcp.io/v1alpha1/logicalclusters/cluster": `{"apiVersion":"core.kcp.io/v1alpha1","kind":"LogicalCluster",
			"metadata":{"name":"cluster","annotations":{"kcp.io/path":"root:orgs:acme"}}}`,
		"/clusters/root:orgs/apis/openfga.platform-mesh.io/v1alpha1/stores/acme": `{"apiVersion":"openfga.platform-mesh.io/v1alpha1","kind":"Store",
			"metadata":{"name":"acme"},"status":{"storeId":"store-acme"}}`,
		"/clusters/root:orgs/apis/openfga.platform-mesh.io/v1alpha1/stores/pending": `{"apiVersion":"openfga.platform-mesh.io/v1alpha1","kind":"Store",
			"metadata":{"name":"pending"}}`,
		"/clusters/acme-id/apis/tenancy.kcp.io/v1alpha1/workspaces/team-a": `{"apiVersion":"tenancy.kcp.io/v1alpha1","kind":"Workspace",
			"metadata":{"name":"team-a"},"spec":{"cluster":"team-a-id"}}`,
		"/clusters/acme-id/apis/core.platform-mesh.io/v1alpha1/accounts/team-a": `{"apiVersion":"core.platform-mesh.io/v1alpha1","kind":"Account",
			"metadata":{"name":"team-a"},"spec":{"type":"account","displayName":"Team A"},
			"status":{"conditions":[{"type":"Other","status":"False"},{"type":"Ready","status":"True"}]}}`,
		"/clusters/acme-id/apis/core.platform-mesh.io/v1alpha1/accounts/new": `{"apiVersion":"core.platform-mesh.io/v1alpha1","kind":"Account",
			"metadata":{"name":"new"},"spec":{"type":"account","displayName":""},
			"status":{"conditions":[{"type":"Ready","status":"False"}]}}`,
	}}
	server := httptest.NewServer(kcp)
	t.Cleanup(server.Close)

	// The cluster of the configured host is not where lookups go.
	directory, err := NewKCPDirectory(&rest.Config{Host: server.URL + "/clusters/root:access:controllers"}, "")
	if err != nil {
		t.Fatalf("NewKCPDirectory: %v", err)
	}

	return directory, kcp
}

func TestKCPDirectory(t *testing.T) {
	t.Parallel()

	directory, kcp := newTestDirectory(t)
	ctx := context.Background()

	if path, found, err := directory.WorkspacePath(ctx, "acme-id"); err != nil || !found || path.String() != "root:orgs:acme" {
		t.Errorf("WorkspacePath = %q, %v, %v", path, found, err)
	}
	if _, found, err := directory.WorkspacePath(ctx, "unknown-id"); err != nil || found {
		t.Errorf("WorkspacePath of an unknown cluster = found %v, err %v", found, err)
	}

	if id, found, err := directory.StoreID(ctx, "acme"); err != nil || !found || id != "store-acme" {
		t.Errorf("StoreID = %q, %v, %v", id, found, err)
	}
	if _, found, err := directory.StoreID(ctx, "pending"); err != nil || found {
		t.Errorf("StoreID of a store without an id = found %v, err %v", found, err)
	}

	if cluster, found, err := directory.WorkspaceCluster(ctx, "acme-id", "team-a"); err != nil || !found || cluster != "team-a-id" {
		t.Errorf("WorkspaceCluster = %q, %v, %v", cluster, found, err)
	}
	if _, found, err := directory.WorkspaceCluster(ctx, "acme-id", "gone"); err != nil || found {
		t.Errorf("WorkspaceCluster of a missing workspace = found %v, err %v", found, err)
	}

	want := AccountDetails{DisplayName: "Team A", Type: "account", Ready: true}
	if details, found, err := directory.AccountDetails(ctx, "acme-id", "team-a"); err != nil || !found || details != want {
		t.Errorf("AccountDetails = %+v, %v, %v", details, found, err)
	}
	// Not ready, and asked again the next time.
	const newAccount = "/clusters/acme-id/apis/core.platform-mesh.io/v1alpha1/accounts/new"
	for range 2 {
		if details, found, err := directory.AccountDetails(ctx, "acme-id", "new"); err != nil || !found || details != (AccountDetails{Type: "account"}) {
			t.Errorf("AccountDetails of an account that is not ready = %+v, %v, %v", details, found, err)
		}
	}
	if kcp.hits[newAccount] != 2 {
		t.Errorf("an account that is not ready was read %d times, want 2", kcp.hits[newAccount])
	}
	if _, found, err := directory.AccountDetails(ctx, "acme-id", "gone"); err != nil || found {
		t.Errorf("AccountDetails of a missing account = found %v, err %v", found, err)
	}
}

func TestKCPDirectory_RemembersWhatItFound(t *testing.T) {
	t.Parallel()

	directory, kcp := newTestDirectory(t)
	now := time.Now()
	directory.now = func() time.Time { return now }
	ctx := context.Background()

	const found = "/clusters/root:orgs/apis/openfga.platform-mesh.io/v1alpha1/stores/acme"
	const missing = "/clusters/root:orgs/apis/openfga.platform-mesh.io/v1alpha1/stores/later"

	for range 3 {
		if _, _, err := directory.StoreID(ctx, "acme"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := directory.StoreID(ctx, "later"); err != nil {
			t.Fatal(err)
		}
	}
	if kcp.hits[found] != 1 {
		t.Errorf("a found store was read %d times, want 1", kcp.hits[found])
	}
	// A store that appears later must be seen right away.
	if kcp.hits[missing] != 3 {
		t.Errorf("a missing store was read %d times, want 3", kcp.hits[missing])
	}

	now = now.Add(defaultCacheTTL)
	if _, _, err := directory.StoreID(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if kcp.hits[found] != 2 {
		t.Errorf("after the TTL the store was read %d times, want 2", kcp.hits[found])
	}
}

func TestKCPDirectory_ReportsErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`)
	}))
	t.Cleanup(server.Close)

	directory, err := NewKCPDirectory(&rest.Config{Host: server.URL}, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := directory.StoreID(context.Background(), "acme"); err == nil {
		t.Error("a forbidden read must be an error, not a missing store")
	}
}
