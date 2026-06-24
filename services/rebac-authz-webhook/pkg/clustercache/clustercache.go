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

type clusterCache struct {
	lock  sync.RWMutex
	cache map[multicluster.ClusterName]ClusterInfo
	mgr   mcmanager.Manager
}

func New(mgr mcmanager.Manager) (*clusterCache, error) {

	return &clusterCache{
		cache: make(map[multicluster.ClusterName]ClusterInfo),
		mgr:   mgr,
	}, nil
}

func (c *clusterCache) Get(clusterName multicluster.ClusterName) (ClusterInfo, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	val, ok := c.cache[clusterName]
	return val, ok
}

func (c *clusterCache) Engage(ctx context.Context, name multicluster.ClusterName, cl cluster.Cluster) error {
	klog.V(5).InfoS("Engaging cluster", "clusterName", name)

	var lc unstructured.Unstructured
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		lc = unstructured.Unstructured{}
		lc.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "core.kcp.io",
			Version: "v1alpha1",
			Kind:    "LogicalCluster",
		})
		if err := cl.GetClient().Get(ctx, types.NamespacedName{Name: "cluster"}, &lc); err != nil {
			klog.V(5).ErrorS(err, "Failed to get LogicalCluster, will retry", "clusterName", name)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		klog.ErrorS(err, "Failed to get LogicalCluster, context cancelled", "clusterName", name)
		return err
	}

	annotationPath := lc.GetAnnotations()["kcp.io/path"]
	klog.V(5).InfoS("Retrieved logical cluster path", "clusterName", name, "path", annotationPath)

	const orgsPrefix = "root:orgs:"
	if !strings.HasPrefix(annotationPath, orgsPrefix) {
		klog.V(5).InfoS("Cluster path does not have orgs prefix, skipping", "clusterName", name, "path", annotationPath)
		return nil
	}

	orgName, _, _ := strings.Cut(annotationPath[len(orgsPrefix):], ":")
	accountName := logicalcluster.NewPath(annotationPath).Base()

	parentClusterID, found, err := unstructured.NestedString(lc.Object, "spec", "owner", "cluster")
	if err != nil {
		klog.ErrorS(err, "Failed to get owner.cluster from LogicalCluster spec", "clusterName", name)
		return err
	}
	if !found {
		klog.Error("No owner.cluster found in LogicalCluster spec", "clusterName", name)
		return errors.New("owner.cluster not found in LogicalCluster spec")
	}

	var store unstructured.Unstructured
	err = wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		store = unstructured.Unstructured{}
		store.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "core.platform-mesh.io",
			Version: "v1alpha1",
			Kind:    "Store",
		})

		orgsCluster, err := c.mgr.GetCluster(ctx, "root:orgs")
		if err != nil {
			return false, err
		}
		orgsClient := orgsCluster.GetClient()

		if err := orgsClient.Get(ctx, types.NamespacedName{Name: orgName}, &store); err != nil {
			klog.V(5).ErrorS(err, "Failed to get Store for org, will retry", "clusterName", name, "orgName", orgName)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		klog.ErrorS(err, "Failed to get Store for org", "clusterName", name, "orgName", orgName)
		return err
	}

	storeID, found, err := unstructured.NestedString(store.Object, "status", "storeId")
	if err != nil {
		klog.ErrorS(err, "Failed to get storeId from Store status", "clusterName", name, "orgName", orgName)
		return err
	}
	if !found {
		klog.V(5).InfoS("storeId not found in Store status", "clusterName", name, "orgName", orgName)
		return errors.New("storeId not found in Store status")
	}

	cfg := rest.CopyConfig(cl.GetConfig())

	parsed, err := url.Parse(cfg.Host)
	if err != nil {
		return err
	}

	path, err := url.JoinPath("clusters", name.String())
	if err != nil {
		return err
	}

	parsed.Path = path
	cfg.Host = parsed.String()

	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return err
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	if err != nil {
		return err
	}

	c.lock.Lock()
	c.cache[name] = ClusterInfo{
		StoreID:         storeID,
		RESTMapper:      restMapper,
		AccountName:     accountName,
		ParentClusterID: parentClusterID,
	}
	c.lock.Unlock()

	klog.V(5).InfoS("Cached cluster info",
		"clusterName", name,
		"storeId", storeID,
		"accountName", accountName,
		"parentClusterID", parentClusterID)

	return nil
}

func (c *clusterCache) Start(_ context.Context) error { // coverage-ignore
	return nil
}

var _ Provider = &clusterCache{}
