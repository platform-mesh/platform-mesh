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
	"time"

	pmshardingv1alpha1 "go.platform-mesh.io/apis/sharding/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	finalizerName = "sharding.platform-mesh.io/cleanup"

	ConditionReady              = "Ready"
	ConditionTargetNotFound     = "TargetNotFound"
	ConditionPermissionsMissing = "PermissionsMissing"
	ConditionConflict           = "Conflict"
	ConditionWebhookReady       = "WebhookReady"
)

type ResourceShardingReconciler struct {
	ctrlruntimeclient.Client
	Discovery          discovery.DiscoveryInterface
	Registry           *DynamicControllerRegistry
	Manager            ctrl.Manager
	WebhookNamespace   string
	WebhookServiceName string
}

func (r *ResourceShardingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var rs pmshardingv1alpha1.ResourceSharding
	if err := r.Get(ctx, req.NamespacedName, &rs); err != nil {
		return ctrl.Result{}, ctrlruntimeclient.IgnoreNotFound(err)
	}

	if !rs.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &rs)
	}

	if !controllerutil.ContainsFinalizer(&rs, finalizerName) {
		controllerutil.AddFinalizer(&rs, finalizerName)
		if err := r.Update(ctx, &rs); err != nil {
			return ctrl.Result{}, err
		}
	}

	gvr := schema.GroupVersionResource{
		Group:    rs.Spec.Target.Group,
		Version:  rs.Spec.Target.Version,
		Resource: rs.Spec.Target.Resource,
	}

	if err := r.validateTarget(gvr); err != nil {
		meta.SetStatusCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               ConditionTargetNotFound,
			Status:             metav1.ConditionTrue,
			Reason:             "TargetGVRNotFound",
			Message:            fmt.Sprintf("Target resource %s not found: %v", gvr.String(), err),
			ObservedGeneration: rs.Generation,
		})
		meta.SetStatusCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "TargetNotFound",
			Message:            "Target GVR does not exist in the cluster",
			ObservedGeneration: rs.Generation,
		})
		if updateErr := r.Status().Update(ctx, &rs); updateErr != nil {
			if apierrors.IsConflict(updateErr) {
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(updateErr, "failed to update status after target validation failure")
			return ctrl.Result{RequeueAfter: rs.Spec.Rebalance.Interval.Duration}, nil
		}
		return ctrl.Result{RequeueAfter: rs.Spec.Rebalance.Interval.Duration}, nil
	}
	meta.RemoveStatusCondition(&rs.Status.Conditions, ConditionTargetNotFound)

	if err := r.validateUniqueness(ctx, &rs, gvr); err != nil {
		meta.SetStatusCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               ConditionConflict,
			Status:             metav1.ConditionTrue,
			Reason:             "DuplicateTarget",
			Message:            err.Error(),
			ObservedGeneration: rs.Generation,
		})
		meta.SetStatusCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "Conflict",
			Message:            "Another ResourceSharding targets the same GVR",
			ObservedGeneration: rs.Generation,
		})
		if updateErr := r.Status().Update(ctx, &rs); updateErr != nil {
			if apierrors.IsConflict(updateErr) {
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(updateErr, "failed to update status after conflict detection")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
		}
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}
	meta.RemoveStatusCondition(&rs.Status.Conditions, ConditionConflict)

	if err := CheckTargetPermissions(ctx, r.Client, gvr); err != nil {
		meta.SetStatusCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               ConditionPermissionsMissing,
			Status:             metav1.ConditionTrue,
			Reason:             "InsufficientRBAC",
			Message:            fmt.Sprintf("Missing permissions on target: %v", err),
			ObservedGeneration: rs.Generation,
		})
		meta.SetStatusCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "PermissionsMissing",
			Message:            "Operator lacks required permissions on target GVR",
			ObservedGeneration: rs.Generation,
		})
		if updateErr := r.Status().Update(ctx, &rs); updateErr != nil {
			if apierrors.IsConflict(updateErr) {
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(updateErr, "failed to update status after permissions check failure")
			return ctrl.Result{RequeueAfter: rs.Spec.Rebalance.Interval.Duration}, nil
		}
		return ctrl.Result{RequeueAfter: rs.Spec.Rebalance.Interval.Duration}, nil
	}
	meta.RemoveStatusCondition(&rs.Status.Conditions, ConditionPermissionsMissing)

	if err := r.ensureDynamicController(ctx, &rs, gvr); err != nil {
		logger.Error(err, "failed to ensure dynamic controller")
		return ctrl.Result{}, err
	}

	if err := EnsureWebhookConfiguration(ctx, r.Client, &rs, gvr, r.WebhookNamespace, r.WebhookServiceName); err != nil {
		logger.Error(err, "failed to ensure webhook configuration")
		meta.SetStatusCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               ConditionWebhookReady,
			Status:             metav1.ConditionFalse,
			Reason:             "WebhookConfigurationFailed",
			Message:            fmt.Sprintf("Failed to ensure MutatingWebhookConfiguration: %v", err),
			ObservedGeneration: rs.Generation,
		})
		meta.SetStatusCondition(&rs.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "WebhookNotReady",
			Message:            "Webhook configuration could not be reconciled",
			ObservedGeneration: rs.Generation,
		})
		if updateErr := r.Status().Update(ctx, &rs); updateErr != nil {
			if apierrors.IsConflict(updateErr) {
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(updateErr, "failed to update status after webhook configuration failure")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	meta.RemoveStatusCondition(&rs.Status.Conditions, ConditionWebhookReady)

	// Resolve GVR → GVK for the rebalancer
	gvk, err := r.Manager.GetRESTMapper().KindFor(gvr)
	if err != nil {
		logger.Error(err, "failed to resolve GVK")
		return ctrl.Result{}, err
	}

	rebalancer := &Rebalancer{
		Client:               r.Client,
		LabelKey:             rs.Spec.ShardLabelKey,
		GVK:                  gvk,
		Shards:               shardNames(rs.Spec.Shards),
		Config:               rs.Spec.Rebalance,
		ResourceShardingName: rs.Name,
	}

	result, err := rebalancer.Run(ctx)
	if err != nil {
		logger.Error(err, "rebalance failed")
		return ctrl.Result{RequeueAfter: rs.Spec.Rebalance.Interval.Duration}, nil
	}

	rs.Status.Distribution = result.Distribution
	rs.Status.TotalShards = len(rs.Spec.Shards)
	rs.Status.ObservedGeneration = rs.Generation
	if result.Moved > 0 {
		now := metav1.Now()
		rs.Status.LastRebalanceTime = &now
	}

	meta.SetStatusCondition(&rs.Status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "ControllersRunning",
		Message:            fmt.Sprintf("Watching %s, %d shards configured", gvr.String(), len(rs.Spec.Shards)),
		ObservedGeneration: rs.Generation,
	})

	if err := r.Status().Update(ctx, &rs); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: rs.Spec.Rebalance.Interval.Duration}, nil
}

func (r *ResourceShardingReconciler) handleDeletion(ctx context.Context, rs *pmshardingv1alpha1.ResourceSharding) (ctrl.Result, error) {
	r.Registry.Deregister(rs.UID)
	_ = DeleteWebhookConfiguration(ctx, r.Client, rs)

	controllerutil.RemoveFinalizer(rs, finalizerName)
	if err := r.Update(ctx, rs); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ResourceShardingReconciler) validateTarget(gvr schema.GroupVersionResource) error {
	_, err := r.Discovery.ServerResourcesForGroupVersion(gvr.GroupVersion().String())
	return err
}

func (r *ResourceShardingReconciler) validateUniqueness(ctx context.Context, rs *pmshardingv1alpha1.ResourceSharding, gvr schema.GroupVersionResource) error {
	var list pmshardingv1alpha1.ResourceShardingList
	if err := r.List(ctx, &list); err != nil {
		return err
	}
	for i := range list.Items {
		other := &list.Items[i]
		if other.UID == rs.UID {
			continue
		}
		if !other.DeletionTimestamp.IsZero() {
			continue // skip RSes that are being deleted; they relinquish ownership on deletion
		}
		otherGVR := schema.GroupVersionResource{
			Group:    other.Spec.Target.Group,
			Version:  other.Spec.Target.Version,
			Resource: other.Spec.Target.Resource,
		}
		if otherGVR == gvr {
			return fmt.Errorf("ResourceSharding %q already targets %s", other.Name, gvr.String())
		}
	}
	return nil
}

func (r *ResourceShardingReconciler) ensureDynamicController(ctx context.Context, rs *pmshardingv1alpha1.ResourceSharding, gvr schema.GroupVersionResource) error {
	if existing, exists := r.Registry.Get(rs.UID); exists {
		existing.Assigner.UpdateShards(shardNames(rs.Spec.Shards))
		return nil
	}

	running, err := StartDynamicController(ctx, r.Manager, rs, gvr)
	if err != nil {
		return err
	}

	r.Registry.Register(rs.UID, running)
	return nil
}

func shardNames(refs []pmshardingv1alpha1.ShardRef) []string {
	names := make([]string, len(refs))
	for i, ref := range refs {
		names[i] = ref.Name
	}
	return names
}

// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete

func (r *ResourceShardingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pmshardingv1alpha1.ResourceSharding{}).
		Complete(r)
}
