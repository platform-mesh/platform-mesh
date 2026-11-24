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
// against the workspace in question, remembered for a short time; nothing is
// watched, so it costs nothing while no reviews are asked.
//
// Its identity needs get on logicalclusters, workspaces and accounts in the
// workspaces of the organizations, and on stores and accounts in the
// organizations workspace.
type KCPDirectory struct {
	config        *rest.Config
	httpClient    *http.Client
	baseURL       string
	orgsWorkspace string

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

// NewKCPDirectory returns a directory reading with the given kcp client
// configuration; any /clusters/ path in its host is dropped. orgsWorkspace is
// the path of the workspace holding the organizations and their Stores.
func NewKCPDirectory(config *rest.Config, orgsWorkspace string) (*KCPDirectory, error) {
	if orgsWorkspace == "" {
		orgsWorkspace = DefaultOrgsWorkspace
	}

	config = rest.CopyConfig(config)
	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}

	base := config.Host
	if i := strings.Index(base, "/clusters/"); i >= 0 {
		base = base[:i]
	}

	return &KCPDirectory{
		config:        config,
		httpClient:    httpClient,
		baseURL:       strings.TrimSuffix(base, "/"),
		orgsWorkspace: orgsWorkspace,
		ttl:           defaultCacheTTL,
		now:           time.Now,
		cache:         map[string]cacheEntry{},
	}, nil
}

// WorkspacePath returns the kcp.io/path of the logical cluster.
func (d *KCPDirectory) WorkspacePath(ctx context.Context, cluster string) (logicalcluster.Path, bool, error) {
	path, found, err := d.lookup(ctx, "path/"+cluster, cluster, logicalClusterGVR, "cluster", func(obj *unstructured.Unstructured) string {
		return obj.GetAnnotations()["kcp.io/path"]
	})

	return logicalcluster.NewPath(path), found, err
}

// StoreID returns status.storeId of the Store named like the organization.
func (d *KCPDirectory) StoreID(ctx context.Context, org string) (string, bool, error) {
	return d.lookup(ctx, "store/"+org, d.orgsWorkspace, storeGVR, org, func(obj *unstructured.Unstructured) string {
		id, _, _ := unstructured.NestedString(obj.Object, "status", "storeId")
		return id
	})
}

// WorkspaceCluster returns spec.cluster of the Workspace name in parent.
func (d *KCPDirectory) WorkspaceCluster(ctx context.Context, parent, name string) (string, bool, error) {
	return d.lookup(ctx, "workspace/"+parent+"/"+name, parent, workspaceGVR, name, func(obj *unstructured.Unstructured) string {
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
	value, found, err := d.lookup(ctx, key, parent, accountGVR, name, func(obj *unstructured.Unstructured) string {
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
// A missing object, or an empty value (the object is not that far yet), is
// "not found" and is not remembered.
func (d *KCPDirectory) lookup(
	ctx context.Context,
	key, cluster string,
	gvr schema.GroupVersionResource,
	name string,
	extract func(*unstructured.Unstructured) string,
) (string, bool, error) {
	if value, ok := d.cached(key); ok {
		return value, true, nil
	}

	config := rest.CopyConfig(d.config)
	config.Host = d.baseURL + "/clusters/" + cluster

	client, err := dynamic.NewForConfigAndClient(config, d.httpClient)
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
