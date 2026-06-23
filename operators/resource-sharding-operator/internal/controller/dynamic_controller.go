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

package controller

import (
	"context"
	"fmt"
	"sync"

	"go.platform-mesh.io/apis/sharding/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

func StartDynamicController(ctx context.Context, mgr ctrl.Manager, rs *v1alpha1.ResourceSharding, gvr schema.GroupVersionResource) (*RunningController, error) {
	labelKey := rs.Spec.ShardLabelKey
	if labelKey == "" {
		labelKey = "sharding.platform-mesh.io/shard"
	}

	selector, err := labels.Parse("!" + labelKey)
	if err != nil {
		return nil, fmt.Errorf("parsing label selector: %w", err)
	}

	mapper := mgr.GetRESTMapper()
	gvk, err := mapper.KindFor(gvr)
	if err != nil {
		return nil, fmt.Errorf("resolving GVR %s to GVK: %w", gvr.String(), err)
	}

	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(gvk)

	informerCache, err := cache.New(mgr.GetConfig(), cache.Options{
		Scheme: mgr.GetScheme(),
		ByObject: map[client.Object]cache.ByObject{
			obj: {Label: selector},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating cache: %w", err)
	}

	// Snapshot immutable fields before spawning goroutines — the caller's *rs may be mutated concurrently.
	rsName := rs.Name
	assigner := NewShardAssigner(shardNames(rs.Spec.Shards))
	ctrlCtx, cancel := context.WithCancel(ctx)
	logger := ctrl.Log.WithName("shard-assign").WithValues("gvk", gvk.String(), "resourcesharding", rsName)

	// Get the informer BEFORE starting the cache
	informer, err := informerCache.GetInformer(ctrlCtx, obj)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("getting informer: %w", err)
	}

	// Create a work queue
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())

	// Register event handler BEFORE cache starts — ensures initial LIST items are delivered
	_, err = informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			meta, ok := obj.(*metav1.PartialObjectMetadata)
			if !ok {
				return
			}
			queue.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      meta.Name,
					Namespace: meta.Namespace,
				},
			})
		},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("adding event handler: %w", err)
	}

	// Now start the cache — initial LIST will deliver to our registered handler
	go func() {
		_ = informerCache.Start(ctrlCtx)
	}()

	if !informerCache.WaitForCacheSync(ctrlCtx) {
		cancel()
		return nil, fmt.Errorf("cache sync failed for %s", gvr.String())
	}
	logger.Info("dynamic controller started", "gvr", gvr.String(), "gvk", gvk.String())

	var wg sync.WaitGroup

	// Process the work queue
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			item, shutdown := queue.Get()
			if shutdown {
				return
			}

			req := item
			func() {
				defer queue.Done(req)

				patchObj := &metav1.PartialObjectMetadata{}
				patchObj.SetGroupVersionKind(gvk)

				if getErr := mgr.GetClient().Get(ctrlCtx, req.NamespacedName, patchObj); getErr != nil {
					queue.Forget(req)
					return
				}

				if _, exists := patchObj.Labels[labelKey]; exists {
					queue.Forget(req)
					return
				}

				shard := assigner.Next()
				if shard == "" {
					queue.Forget(req)
					return
				}
				patch := client.MergeFrom(patchObj.DeepCopy())
				if patchObj.Labels == nil {
					patchObj.Labels = make(map[string]string)
				}
				patchObj.Labels[labelKey] = shard

				if patchErr := mgr.GetClient().Patch(ctrlCtx, patchObj, patch); patchErr != nil {
					logger.Error(patchErr, "failed to assign shard", "resource", req.NamespacedName)
					queue.AddRateLimited(req)
					return
				}

				assignmentsTotal.WithLabelValues(rsName, shard).Inc()
				logger.Info("assigned shard", "resource", req.NamespacedName, "shard", shard)
				queue.Forget(req)
			}()
		}
	}()

	// Shutdown queue when context is cancelled; wait for worker to drain.
	go func() {
		<-ctrlCtx.Done()
		queue.ShutDown()
		wg.Wait()
	}()

	return &RunningController{
		Cancel:   cancel,
		GVR:      gvr,
		LabelKey: labelKey,
		Assigner: assigner,
	}, nil
}
