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

package sync

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	metadataKey = "metadata"
	statusKey   = "status"
)

// makeCond constructs a metav1.Condition. The status parameter is a boolean
// where true maps to metav1.ConditionTrue and false to metav1.ConditionFalse.
func makeCond(t ConditionType, ok bool, reason, msg string) metav1.Condition {
	s := metav1.ConditionFalse
	if ok {
		s = metav1.ConditionTrue
	}
	return metav1.Condition{
		Type:               t.String(),
		Status:             s,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	}
}

// StripClusterMetadata returns a deep copy of the provided Unstructured with
// cluster-specific fields removed so the object is safe to write in another
// cluster.
func StripClusterMetadata(obj *unstructured.Unstructured) *unstructured.Unstructured {
	c := obj.DeepCopy()
	delete(c.Object, statusKey)
	if m, ok := c.Object[metadataKey].(map[string]any); ok {
		delete(m, "resourceVersion")
		delete(m, "uid")
		delete(m, "creationTimestamp")
		delete(m, "managedFields")
		delete(m, "generation")
		delete(m, "ownerReferences")
		delete(m, "finalizers")
		delete(m, "annotations")
		delete(m, "labels")
	}
	return c
}

// EqualObjects returns true if the two unstructured objects are
// equal after removing cluster-specific metadata and status.
func EqualObjects(a, b *unstructured.Unstructured) bool {
	return cmp.Equal(
		StripClusterMetadata(a).Object,
		StripClusterMetadata(b).Object,
		cmpopts.EquateEmpty(),
	)
}

// Spec copies a resource from source to target without touching the status.
// The sourceName and targetName parameters allow the resource to have
// different names in the source and target clusters.
func Spec(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	sourceName, targetName types.NamespacedName,
	source, target ctrlruntimeclient.Client,
) (metav1.Condition, error) {
	log := logr.FromContextOrDiscard(ctx).WithValues("sourceName", sourceName, "targetName", targetName)

	sourceObj := &unstructured.Unstructured{}
	sourceObj.SetGroupVersionKind(gvk)

	if err := source.Get(ctx, sourceName, sourceObj); err != nil {
		return makeCond(ConditionResourceCopied, false, "GetSourceFailed", err.Error()), err
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(gvk)
	if err := target.Get(ctx, targetName, existing); err != nil {
		if apierrors.IsNotFound(err) {
			targetObj := StripClusterMetadata(sourceObj)
			targetObj.SetName(targetName.Name)
			targetObj.SetNamespace(targetName.Namespace)
			if err := target.Create(ctx, targetObj); err != nil {
				return makeCond(ConditionResourceCopied, false, "CreateFailed", err.Error()), err
			}
			return makeCond(ConditionResourceCopied, true, "Created", "Resource created on destination"), nil
		}
		return makeCond(ConditionResourceCopied, false, "GetTargetFailed", err.Error()), err
	}

	if !EqualObjects(sourceObj, existing) {
		log.V(2).Info("Objects not equal, updating target")
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Re-fetch the target object to get the latest resourceVersion
			if err := target.Get(ctx, targetName, existing); err != nil {
				return err
			}
			toUpdate := existing.DeepCopy()
			for k := range toUpdate.Object {
				if k == metadataKey || k == statusKey {
					continue
				}
				if _, ok := sourceObj.Object[k]; !ok {
					delete(toUpdate.Object, k)
				}
			}
			for k, v := range sourceObj.Object {
				if k == metadataKey || k == statusKey {
					continue
				}
				toUpdate.Object[k] = v
			}
			return target.Update(ctx, toUpdate)
		})
		if err != nil {
			return makeCond(ConditionResourceCopied, false, "UpdateFailed", err.Error()), err
		}
	}

	return makeCond(ConditionResourceCopied, true, "Copied", "Resource copied to destination"), nil
}

// Status reflects the status of the target resource back to the source
// resource. A target without a status is a no-op.
func Status(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	sourceName, targetName types.NamespacedName,
	source, target ctrlruntimeclient.Client,
) (metav1.Condition, error) {
	log := logr.FromContextOrDiscard(ctx).WithValues("sourceName", sourceName, "targetName", targetName)

	targetObj := &unstructured.Unstructured{}
	targetObj.SetGroupVersionKind(gvk)
	if err := target.Get(ctx, targetName, targetObj); err != nil {
		return makeCond(ConditionStatusSynced, false, "GetTargetFailed", err.Error()), err
	}

	status, ok := targetObj.Object[statusKey]
	if !ok {
		log.V(2).Info("Target has no status to sync")
		return makeCond(ConditionStatusSynced, true, "StatusSynced", "Target has no status to sync"), nil
	}

	sourceObj := &unstructured.Unstructured{}
	sourceObj.SetGroupVersionKind(gvk)
	if err := source.Get(ctx, sourceName, sourceObj); err != nil {
		return makeCond(ConditionStatusSynced, false, "GetSourceFailed", err.Error()), err
	}

	if !cmp.Equal(sourceObj.Object[statusKey], status, cmpopts.EquateEmpty()) {
		log.V(2).Info("Syncing status from target to source")
		sourceObj.Object[statusKey] = status
		if err := source.Status().Update(ctx, sourceObj); err != nil {
			return makeCond(ConditionStatusSynced, false, "StatusUpdateFailed", err.Error()), err
		}
	}

	return makeCond(ConditionStatusSynced, true, "StatusSynced", "Status copied back to source"), nil
}

// Resource copies a resource from source to target and reflects the status
// back, combining [Spec] and [Status].
func Resource(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	sourceName, targetName types.NamespacedName,
	source, target ctrlruntimeclient.Client,
) (metav1.Condition, error) {
	if cond, err := Spec(ctx, gvk, sourceName, targetName, source, target); err != nil {
		return cond, err
	}

	return Status(ctx, gvk, sourceName, targetName, source, target)
}
