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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/logicalcluster/v3"
)

var (
	logicalClusterGVR = schema.GroupVersionResource{Group: "core.kcp.io", Version: "v1alpha1", Resource: "logicalclusters"}
	workspaceGVR      = schema.GroupVersionResource{Group: "tenancy.kcp.io", Version: "v1alpha1", Resource: "workspaces"}
	storeGVR          = schema.GroupVersionResource{Group: "openfga.platform-mesh.io", Version: "v1alpha1", Resource: "stores"}
	accountGVR        = schema.GroupVersionResource{Group: "core.platform-mesh.io", Version: "v1alpha1", Resource: "accounts"}
)

// defaultCacheTTL is how long a lookup is remembered. Most of what is cached
// does not change while the object exists (the path of a logical cluster,
// the cluster of a workspace, the id of a store), so the TTL bounds how long
// an answer outlives a deleted object. The display name of an account can
// change; a review shows the new one after the TTL at the latest.
const defaultCacheTTL = time.Minute

// KCPDirectory is the Directory reading from kcp. Lookups are plain GETs
// against the workspace in question, remembered for a short time; nothing of
// what it looks up is watched, so it costs nothing while no reviews are asked.
//
// It reads in one of two ways:
//
//   - through the virtual workspaces of the APIExports that carry the
//     resources (NewExportDirectory). Its identity then needs nothing but
//     get on apiexports/content of the two exports, in the workspace that
//     holds them; what it can read is what the exports and their permission
//     claims expose, in the workspaces that bind them. This is what Platform
//     Mesh runs.
//   - directly, /clusters/<workspace> (NewKCPDirectory). Its identity then
//     needs get on logicalclusters, workspaces and accounts in every workspace
//     of an organization, and on stores and accounts in the organizations
//     workspace, which in kcp only an administrator has everywhere.
type KCPDirectory struct {
	// accounts serves LogicalClusters, Workspaces and Accounts; stores serves
	// Stores.
	accounts, stores clusterSource
	orgsWorkspace    string

	ttl   time.Duration
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	value   string
	expires time.Time
}

var _ Directory = &KCPDirectory{}

// clusterSource knows where a workspace is read.
type clusterSource interface {
	// clientFor returns the client configuration for the workspace (path or
	// logical cluster name), its host being the URL of the workspace. found
	// is false for a workspace the source does not serve.
	clientFor(ctx context.Context, cluster string) (config *rest.Config, httpClient *http.Client, found bool, err error)
	// ready reports whether the source can find the workspace yet.
	ready(ctx context.Context, cluster string) bool
}

// directSource reads a workspace at <base>/clusters/<workspace>.
type directSource struct {
	config     *rest.Config
	httpClient *http.Client
	baseURL    string
}

func (s *directSource) clientFor(_ context.Context, cluster string) (*rest.Config, *http.Client, bool, error) {
	config := rest.CopyConfig(s.config)
	config.Host = s.baseURL + "/clusters/" + cluster

	return config, s.httpClient, true, nil
}

func (s *directSource) ready(context.Context, string) bool {
	return true
}

// ClusterGetter returns a workspace by logical cluster name or path. The
// path-aware APIExport provider of github.com/kcp-dev/multicluster-provider
// implements it: its clusters are the workspaces binding an APIExport, read
// through the virtual workspace of that export.
type ClusterGetter interface {
	Get(ctx context.Context, clusterName multicluster.ClusterName) (cluster.Cluster, error)
}

// exportSource reads a workspace through the virtual workspace of an
// APIExport.
type exportSource struct {
	clusters ClusterGetter
}

func (s *exportSource) clientFor(ctx context.Context, cluster string) (*rest.Config, *http.Client, bool, error) {
	cl, err := s.clusters.Get(ctx, multicluster.ClusterName(cluster))
	if errors.Is(err, multicluster.ErrClusterNotFound) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}

	return cl.GetConfig(), cl.GetHTTPClient(), true, nil
}

func (s *exportSource) ready(ctx context.Context, cluster string) bool {
	_, _, found, err := s.clientFor(ctx, cluster)
	return found && err == nil
}

// NewKCPDirectory returns a directory reading the workspaces directly, with
// the given kcp client configuration; any /clusters/ path in its host is
// dropped. orgsWorkspace is the path of the workspace holding the
// organizations and their Stores.
func NewKCPDirectory(config *rest.Config, orgsWorkspace string) (*KCPDirectory, error) {
	config = rest.CopyConfig(config)
	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}

	base := config.Host
	if i := strings.Index(base, "/clusters/"); i >= 0 {
		base = base[:i]
	}

	source := &directSource{config: config, httpClient: httpClient, baseURL: strings.TrimSuffix(base, "/")}

	return newDirectory(source, source, orgsWorkspace), nil
}

// NewExportDirectory returns a directory reading through the virtual
// workspaces of two APIExports: accounts is the export of the Accounts, which
// also claims LogicalClusters and Workspaces (core.platform-mesh.io), stores
// the one of the Stores (openfga.platform-mesh.io). orgsWorkspace is the path
// of the workspace holding the organizations and their Stores.
func NewExportDirectory(accounts, stores ClusterGetter, orgsWorkspace string) *KCPDirectory {
	return newDirectory(&exportSource{clusters: accounts}, &exportSource{clusters: stores}, orgsWorkspace)
}

func newDirectory(accounts, stores clusterSource, orgsWorkspace string) *KCPDirectory {
	if orgsWorkspace == "" {
		orgsWorkspace = DefaultOrgsWorkspace
	}

	return &KCPDirectory{
		accounts:      accounts,
		stores:        stores,
		orgsWorkspace: orgsWorkspace,
		ttl:           defaultCacheTTL,
		now:           time.Now,
		cache:         map[string]cacheEntry{},
	}
}

// Ready reports whether the organizations workspace can be read. Read through
// APIExports, that takes until the workspaces binding them are known; before
// that every workspace would look as if it did not exist, and a review would
// be empty instead of failing.
func (d *KCPDirectory) Ready() bool {
	ctx := context.Background()
	return d.accounts.ready(ctx, d.orgsWorkspace) && d.stores.ready(ctx, d.orgsWorkspace)
}

// WorkspacePath returns the kcp.io/path of the logical cluster.
func (d *KCPDirectory) WorkspacePath(ctx context.Context, cluster string) (logicalcluster.Path, bool, error) {
	path, found, err := d.lookup(ctx, "path/"+cluster, d.accounts, cluster, logicalClusterGVR, "cluster", func(obj *unstructured.Unstructured) string {
		return obj.GetAnnotations()["kcp.io/path"]
	})

	return logicalcluster.NewPath(path), found, err
}

// StoreID returns status.storeId of the Store named like the organization.
func (d *KCPDirectory) StoreID(ctx context.Context, org string) (string, bool, error) {
	return d.lookup(ctx, "store/"+org, d.stores, d.orgsWorkspace, storeGVR, org, func(obj *unstructured.Unstructured) string {
		id, _, _ := unstructured.NestedString(obj.Object, "status", "storeId")
		return id
	})
}

// WorkspaceCluster returns spec.cluster of the Workspace name in parent.
func (d *KCPDirectory) WorkspaceCluster(ctx context.Context, parent, name string) (string, bool, error) {
	return d.lookup(ctx, "workspace/"+parent+"/"+name, d.accounts, parent, workspaceGVR, name, func(obj *unstructured.Unstructured) string {
		cluster, _, _ := unstructured.NestedString(obj.Object, "spec", "cluster")
		return cluster
	})
}

// AccountDetails returns spec.displayName, spec.type and the readiness of the
// Account name in parent.
//
// An account that is not ready is not remembered: it is about to become
// ready, and a list should show that on the next review.
func (d *KCPDirectory) AccountDetails(ctx context.Context, parent, name string) (AccountDetails, bool, error) {
	// The values are remembered as one string; type and readiness have no
	// separator in them, the display name comes last.
	const sep = "\x00"

	key := "account/" + parent + "/" + name
	value, found, err := d.lookup(ctx, key, d.accounts, parent, accountGVR, name, func(obj *unstructured.Unstructured) string {
		accountType, _, _ := unstructured.NestedString(obj.Object, "spec", "type")
		displayName, _, _ := unstructured.NestedString(obj.Object, "spec", "displayName")
		return strings.Join([]string{accountType, strconv.FormatBool(isReady(obj)), displayName}, sep)
	})
	if err != nil || !found {
		return AccountDetails{}, false, err
	}

	parts := strings.SplitN(value, sep, 3)
	details := AccountDetails{Type: parts[0], Ready: parts[1] == "true", DisplayName: parts[2]}
	if !details.Ready {
		d.forget(key)
	}

	return details, true, nil
}

// isReady reports whether the object has the condition Ready with status True.
func isReady(obj *unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		condition, ok := c.(map[string]any)
		if ok && condition["type"] == "Ready" {
			return condition["status"] == "True"
		}
	}

	return false
}

// lookup gets one cluster-scoped object in a workspace and extracts a value.
// A missing object or workspace, or an empty value (the object is not that
// far yet), is "not found" and is not remembered.
func (d *KCPDirectory) lookup(
	ctx context.Context,
	key string,
	source clusterSource,
	cluster string,
	gvr schema.GroupVersionResource,
	name string,
	extract func(*unstructured.Unstructured) string,
) (string, bool, error) {
	// The cluster becomes part of the URL asked with the identity of this
	// server. It comes from the URL of a review or from an OpenFGA object, so
	// anything that is not a workspace path or logical cluster name names
	// nothing, rather than another URL.
	if p := logicalcluster.NewPath(cluster); cluster == "" || p == logicalcluster.Wildcard || !p.IsValid() {
		return "", false, nil
	}

	if value, ok := d.cached(key); ok {
		return value, true, nil
	}

	config, httpClient, found, err := source.clientFor(ctx, cluster)
	if err != nil {
		return "", false, fmt.Errorf("find %s: %w", cluster, err)
	}
	if !found {
		return "", false, nil
	}

	client, err := dynamic.NewForConfigAndClient(config, httpClient)
	if err != nil {
		return "", false, fmt.Errorf("build client for %s: %w", cluster, err)
	}

	obj, err := client.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get %s %s in %s: %w", gvr.Resource, name, cluster, err)
	}

	value := extract(obj)
	if value == "" {
		return "", false, nil
	}

	d.remember(key, value)

	return value, true, nil
}

func (d *KCPDirectory) cached(key string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	entry, ok := d.cache[key]
	if !ok {
		return "", false
	}
	if !d.now().Before(entry.expires) {
		delete(d.cache, key)
		return "", false
	}

	return entry.value, true
}

func (d *KCPDirectory) forget(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.cache, key)
}

func (d *KCPDirectory) remember(key, value string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	// Entries are only dropped when asked for again; sweep before growing.
	if len(d.cache) >= 4096 {
		for k, entry := range d.cache {
			if !now.Before(entry.expires) {
				delete(d.cache, k)
			}
		}
	}

	d.cache[key] = cacheEntry{value: value, expires: now.Add(d.ttl)}
}
