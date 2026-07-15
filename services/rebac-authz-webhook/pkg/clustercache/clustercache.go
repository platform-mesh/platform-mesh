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

package clustercache

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/logicalcluster/v3"
)

type ClusterInfo struct {
	StoreID         string
	RESTMapper      meta.RESTMapper
	AccountName     string
	ParentClusterID string
}

type Provider interface {
	mcmanager.Runnable
	Get(clusterName multicluster.ClusterName) (ClusterInfo, bool)
}

var defaultBackoff = wait.Backoff{
	Duration: time.Second,
	Factor:   2.0,
	Jitter:   0.1,
	Steps:    9,
	Cap:      5 * time.Minute,
}

type resolver struct {
	cl     cluster.Cluster
	cancel context.CancelFunc
}

type clusterCache struct {
	cacheLock sync.RWMutex
	cache     map[multicluster.ClusterName]ClusterInfo

	resolversLock sync.Mutex
	resolvers     map[multicluster.ClusterName]*resolver

	mgr     mcmanager.Manager
	backoff wait.Backoff
}

type Option func(*clusterCache)

func WithBackoff(b wait.Backoff) Option {
	return func(c *clusterCache) { c.backoff = b }
}

func New(mgr mcmanager.Manager, opts ...Option) (*clusterCache, error) {
	c := &clusterCache{
		cache:     make(map[multicluster.ClusterName]ClusterInfo),
		resolvers: make(map[multicluster.ClusterName]*resolver),
		mgr:       mgr,
		backoff:   defaultBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (c *clusterCache) Get(clusterName multicluster.ClusterName) (ClusterInfo, bool) {
	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()
	val, ok := c.cache[clusterName]
	return val, ok
}

func (c *clusterCache) Engage(ctx context.Context, name multicluster.ClusterName, cl cluster.Cluster) error {
	klog.V(5).InfoS("Engaging cluster", "clusterName", name)

	c.resolversLock.Lock()
	defer c.resolversLock.Unlock()

	if old, ok := c.resolvers[name]; ok {
		if old.cl == cl {
			return nil
		}
		old.cancel()
		delete(c.resolvers, name)
	}

	resolveCtx, cancel := context.WithCancel(ctx)
	r := &resolver{cl: cl, cancel: cancel}
	c.resolvers[name] = r

	// Always return nil: Clusters.Add removes the cluster if Engage errors, and
	// nothing re-drives it afterwards. Resolve in the background instead.
	go func() {
		defer func() {
			c.resolversLock.Lock()
			if cur, ok := c.resolvers[name]; ok && cur == r {
				delete(c.resolvers, name)
			}
			c.resolversLock.Unlock()
		}()
		c.resolve(resolveCtx, name, cl)
	}()

	return nil
}

// resolve retries until it succeeds or ctx is cancelled. It loops by hand rather
// than via wait.ExponentialBackoffWithContext, which stops after Backoff.Steps;
// DelayFunc caps at Backoff.Cap and keeps returning it.
func (c *clusterCache) resolve(ctx context.Context, name multicluster.ClusterName, cl cluster.Cluster) {
	delay := c.backoff.DelayFunc()
	for {
		done, err := c.tryResolve(ctx, name, cl)
		if done {
			return
		}
		if err != nil {
			klog.V(5).ErrorS(err, "Failed to resolve cluster, will retry", "clusterName", name)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay()):
		}
	}
}

// tryResolve returns done=true when the cluster was cached or intentionally
// skipped, and (false, err) when it should be retried.
func (c *clusterCache) tryResolve(ctx context.Context, name multicluster.ClusterName, cl cluster.Cluster) (bool, error) {
	lc := unstructured.Unstructured{}
	lc.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "core.kcp.io",
		Version: "v1alpha1",
		Kind:    "LogicalCluster",
	})
	if err := cl.GetClient().Get(ctx, types.NamespacedName{Name: "cluster"}, &lc); err != nil {
		return false, err
	}

	annotationPath := lc.GetAnnotations()["kcp.io/path"]
	klog.V(5).InfoS("Retrieved logical cluster path", "clusterName", name, "path", annotationPath)

	const orgsPrefix = "root:orgs:"
	if !strings.HasPrefix(annotationPath, orgsPrefix) {
		klog.V(5).InfoS("Cluster path does not have orgs prefix, skipping", "clusterName", name, "path", annotationPath)
		return true, nil
	}

	orgName, _, _ := strings.Cut(annotationPath[len(orgsPrefix):], ":")
	accountName := logicalcluster.NewPath(annotationPath).Base()

	parentClusterID, found, err := unstructured.NestedString(lc.Object, "spec", "owner", "cluster")
	if err != nil {
		return false, err
	}
	if !found {
		return false, errors.New("owner.cluster not found in LogicalCluster spec")
	}

	store := unstructured.Unstructured{}
	store.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "core.platform-mesh.io",
		Version: "v1alpha1",
		Kind:    "Store",
	})
	orgsCluster, err := c.mgr.GetCluster(ctx, "root:orgs")
	if err != nil {
		return false, err
	}
	if err := orgsCluster.GetClient().Get(ctx, types.NamespacedName{Name: orgName}, &store); err != nil {
		return false, err
	}

	storeID, found, err := unstructured.NestedString(store.Object, "status", "storeId")
	if err != nil {
		return false, err
	}
	if !found {
		return false, errors.New("storeId not found in Store status")
	}

	cfg := rest.CopyConfig(cl.GetConfig())

	parsed, err := url.Parse(cfg.Host)
	if err != nil {
		return false, err
	}

	path, err := url.JoinPath("clusters", name.String())
	if err != nil {
		return false, err
	}

	parsed.Path = path
	cfg.Host = parsed.String()

	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return false, err
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	if err != nil {
		return false, err
	}

	c.cacheLock.Lock()
	c.cache[name] = ClusterInfo{
		StoreID:         storeID,
		RESTMapper:      restMapper,
		AccountName:     accountName,
		ParentClusterID: parentClusterID,
	}
	c.cacheLock.Unlock()

	klog.V(5).InfoS("Cached cluster info",
		"clusterName", name,
		"storeId", storeID,
		"accountName", accountName,
		"parentClusterID", parentClusterID)

	return true, nil
}

func (c *clusterCache) Start(_ context.Context) error { // coverage-ignore
	return nil
}

var _ Provider = &clusterCache{}
