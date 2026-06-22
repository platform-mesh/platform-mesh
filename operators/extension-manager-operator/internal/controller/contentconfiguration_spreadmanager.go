package controller

import (
	"fmt"
	"math/rand/v2"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.platform-mesh.io/subroutines/spread"

	"go.platform-mesh.io/apis/ui/v1alpha1"
)

// contentConfigurationSpreadManager implements lifecycle.SpreadManager to
// reconcile ContentConfigurations every 12-24 hours for inline or 2,5-5 minutes
// for remote ContentConfigurations. Respects
// spread.RefreshLabel("platform-mesh.io/refresh-reconcile").
type contentConfigurationSpreadManager struct{}

const (
	inlineMaxReconcileDuration = 24 * time.Hour
	remoteMaxReconcileDuration = 5 * time.Minute
)

// nextReconcileDelay returns a random duration between max/2 and max
// (same algorithm as golang-commons spread.getNextReconcileTime).
func nextReconcileDelay(maxReconcileTime time.Duration) time.Duration {
	minimum := maxReconcileTime.Minutes() / 2 // At least every half of maximum
	jitter := rand.Int64N(int64(minimum))     // Add random jitter within the other half
	return time.Duration(jitter+int64(minimum)) * time.Minute
}

func (contentConfigurationSpreadManager) ReconcileRequired(obj client.Object) bool {
	cc := mustContentConfiguration(obj)

	if cc.GetGeneration() != cc.Status.ObservedGeneration {
		return true
	}

	labels := cc.GetLabels()
	if labels != nil {
		if _, has := labels[spread.RefreshLabel]; has {
			return true
		}
	}

	nrt := cc.Status.NextReconcileTime
	if nrt.IsZero() {
		return true
	}

	return time.Now().UTC().After(nrt.UTC())
}

func (contentConfigurationSpreadManager) RequeueDelay(obj client.Object) time.Duration {
	cc := mustContentConfiguration(obj)

	nrt := cc.Status.NextReconcileTime
	if nrt.IsZero() {
		return 0
	}

	remaining := time.Until(nrt.UTC())
	if remaining < 0 {
		return 0
	}

	return remaining
}

func (contentConfigurationSpreadManager) SetNextReconcileTime(obj client.Object) {
	cc := mustContentConfiguration(obj)

	border := inlineMaxReconcileDuration
	if cc.Spec.RemoteConfiguration != nil {
		border = remoteMaxReconcileDuration
	}

	delay := nextReconcileDelay(border)
	cc.Status.NextReconcileTime = metav1.NewTime(time.Now().Add(delay))
}

func (contentConfigurationSpreadManager) UpdateObservedGeneration(obj client.Object) {
	cc := mustContentConfiguration(obj)

	cc.Status.ObservedGeneration = cc.GetGeneration()
}

func (contentConfigurationSpreadManager) RemoveRefreshLabel(obj client.Object) bool {
	cc := mustContentConfiguration(obj)

	labels := cc.GetLabels()
	if labels == nil {
		return false
	}

	if _, ok := labels[spread.RefreshLabel]; !ok {
		return false
	}
	delete(labels, spread.RefreshLabel)
	cc.SetLabels(labels)

	return true
}

func mustContentConfiguration(obj client.Object) *v1alpha1.ContentConfiguration {
	cc, ok := obj.(*v1alpha1.ContentConfiguration)
	if !ok {
		panic(fmt.Sprintf("contentConfigurationSpread: expected ContentConfiguration, got %T", obj))
	}
	return cc
}
