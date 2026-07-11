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

package brokeredresource

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"go.platform-mesh.io/resource-broker/pkg/sync"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

// RelatedResourcesFinalizer is placed on consumer objects to clean up
// related resource copies on deletion.
const RelatedResourcesFinalizer = "broker.platform-mesh.io/related-resources"

// relatedResourcesSubroutine copies the related resources published in the
// staging copy's status into the consumer cluster.
type relatedResourcesSubroutine struct {
	opts Options
}

var (
	_ subroutines.Processor = &relatedResourcesSubroutine{}
	_ subroutines.Finalizer = &relatedResourcesSubroutine{}
)

func (s *relatedResourcesSubroutine) GetName() string {
	return "RelatedResources"
}

func (s *relatedResourcesSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{RelatedResourcesFinalizer}
}

func (s *relatedResourcesSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Unstructured, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}

	consumerClient, err := subroutines.ClientFromContext(ctx)
	if err != nil {
		return subroutines.Result{}, err
	}

	stagingClient, result, err := stagingClient(ctx, s.opts, cluster.String(), u)
	if stagingClient == nil {
		return result, err
	}

	nn := types.NamespacedName{Namespace: u.GetNamespace(), Name: u.GetName()}
	related, err := sync.CollectRelatedResources(ctx, stagingClient, s.opts.GVK, nn)
	switch {
	case apierrors.IsNotFound(err):
		return subroutines.Pending(s.opts.RequeueInterval, "waiting for staging copy"), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("collecting related resources: %w", err)
	}

	for _, key := range slices.Sorted(maps.Keys(related)) {
		rr := related[key]
		rrName := types.NamespacedName{Namespace: rr.Namespace, Name: rr.Name}
		if _, err := sync.CopyResource(ctx, rr.SchemaGVK(), rrName, rrName, stagingClient, consumerClient); err != nil {
			return subroutines.Result{}, fmt.Errorf("copying related resource %q: %w", key, err)
		}
	}

	return subroutines.OK(), nil
}

// Finalize deletes the related resource copies from the consumer cluster and
// waits for them to be gone.
func (s *relatedResourcesSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("expected Unstructured, got %T", obj)
	}

	cluster, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.Result{}, fmt.Errorf("no cluster name in context")
	}

	consumerClient, err := subroutines.ClientFromContext(ctx)
	if err != nil {
		return subroutines.Result{}, err
	}

	stagingClient, result, err := stagingClient(ctx, s.opts, cluster.String(), u)
	if stagingClient == nil {
		if err != nil {
			return result, err
		}
		// No bound assignment, nothing copied.
		return subroutines.OK(), nil
	}

	nn := types.NamespacedName{Namespace: u.GetNamespace(), Name: u.GetName()}
	related, err := sync.CollectRelatedResources(ctx, stagingClient, s.opts.GVK, nn)
	switch {
	case apierrors.IsNotFound(err):
		// Staging copy gone, nothing left to clean up.
		return subroutines.OK(), nil
	case err != nil:
		return subroutines.Result{}, fmt.Errorf("collecting related resources: %w", err)
	}

	remaining := false
	for _, key := range slices.Sorted(maps.Keys(related)) {
		rr := related[key]

		rrObj := &unstructured.Unstructured{}
		rrObj.SetGroupVersionKind(rr.SchemaGVK())
		rrName := types.NamespacedName{Namespace: rr.Namespace, Name: rr.Name}
		err := consumerClient.Get(ctx, rrName, rrObj)
		switch {
		case apierrors.IsNotFound(err):
			continue
		case err != nil:
			return subroutines.Result{}, fmt.Errorf("getting related resource %q: %w", key, err)
		}

		if rrObj.GetDeletionTimestamp().IsZero() {
			if err := consumerClient.Delete(ctx, rrObj); ctrlruntimeclient.IgnoreNotFound(err) != nil {
				return subroutines.Result{}, fmt.Errorf("deleting related resource %q: %w", key, err)
			}
		}
		remaining = true
	}

	if remaining {
		return subroutines.StopWithRequeue(s.opts.RequeueInterval, "waiting for related resources to be deleted"), nil
	}
	return subroutines.OK(), nil
}
