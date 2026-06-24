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
	"fmt"
	"math/rand/v2"
	"time"

	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/subroutines/spread"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
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

func (contentConfigurationSpreadManager) ReconcileRequired(obj ctrlruntimeclient.Object) bool {
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

func (contentConfigurationSpreadManager) RequeueDelay(obj ctrlruntimeclient.Object) time.Duration {
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

func (contentConfigurationSpreadManager) SetNextReconcileTime(obj ctrlruntimeclient.Object) {
	cc := mustContentConfiguration(obj)

	border := inlineMaxReconcileDuration
	if cc.Spec.RemoteConfiguration != nil {
		border = remoteMaxReconcileDuration
	}

	delay := nextReconcileDelay(border)
	cc.Status.NextReconcileTime = metav1.NewTime(time.Now().Add(delay))
}

func (contentConfigurationSpreadManager) UpdateObservedGeneration(obj ctrlruntimeclient.Object) {
	cc := mustContentConfiguration(obj)

	cc.Status.ObservedGeneration = cc.GetGeneration()
}

func (contentConfigurationSpreadManager) RemoveRefreshLabel(obj ctrlruntimeclient.Object) bool {
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

func mustContentConfiguration(obj ctrlruntimeclient.Object) *pmuiv1alpha1.ContentConfiguration {
	cc, ok := obj.(*pmuiv1alpha1.ContentConfiguration)
	if !ok {
		panic(fmt.Sprintf("contentConfigurationSpread: expected ContentConfiguration, got %T", obj))
	}
	return cc
}
