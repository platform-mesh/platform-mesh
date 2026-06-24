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

package conditions

import (
	"fmt"

	"go.platform-mesh.io/subroutines"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ReadyCondition = "Ready"

	ReasonComplete = "Complete"
	ReasonPending  = "Pending"
	ReasonStopped  = "Stopped"
	ReasonSkipped  = "Skipped"
	ReasonError    = "Error"
	ReasonUnknown  = "Unknown"
)

// Manager manages per-subroutine and aggregate Ready conditions on objects
// that implement ConditionAccessor.
type Manager struct{}

// NewManager creates a new condition Manager.
func NewManager() *Manager {
	return &Manager{}
}

// InitUnknownConditions sets per-subroutine and Ready conditions to Unknown
// if they are not already present.
func (m *Manager) InitUnknownConditions(obj ctrlruntimeclient.Object, subroutineNames []string) {
	accessor, ok := obj.(ConditionAccessor)
	if !ok {
		return
	}

	generation := obj.GetGeneration()
	for _, name := range subroutineNames {
		m.ensureCondition(accessor, name, generation)
	}
	m.ensureCondition(accessor, ReadyCondition, generation)
}

// SetSubroutineCondition maps a subroutine result/error to a condition on the object.
// The action determines the condition name suffix (finalize/terminate actions append "Finalize").
func (m *Manager) SetSubroutineCondition(obj ctrlruntimeclient.Object, name string, result subroutines.Result, err error, isFinalize bool) {
	accessor, ok := obj.(ConditionAccessor)
	if !ok {
		return
	}

	condName := name
	if isFinalize {
		condName = name + "Finalize"
	}

	cond := metav1.Condition{
		Type:               condName,
		ObservedGeneration: obj.GetGeneration(),
	}

	switch {
	case err != nil:
		cond.Status = metav1.ConditionFalse
		cond.Reason = ReasonError
		cond.Message = err.Error()
	case result.IsPending():
		cond.Status = metav1.ConditionUnknown
		cond.Reason = ReasonPending
		cond.Message = result.Message()
	case result.IsStopWithRequeue() || result.IsStop():
		cond.Status = metav1.ConditionFalse
		cond.Reason = ReasonStopped
		cond.Message = result.Message()
	case result.IsSkip():
		cond.Status = metav1.ConditionTrue
		cond.Reason = ReasonSkipped
		cond.Message = result.Message()
	default:
		cond.Status = metav1.ConditionTrue
		cond.Reason = ReasonComplete
		cond.Message = result.Message()
	}

	conditions := accessor.GetConditions()
	meta.SetStatusCondition(&conditions, cond)
	accessor.SetConditions(conditions)
}

// SetSkippedConditions sets conditions for the given subroutine names to Skipped.
// When ready is true, condition status is True; when false, condition status is False.
func (m *Manager) SetSkippedConditions(obj ctrlruntimeclient.Object, names []string, ready bool, msg string) {
	accessor, ok := obj.(ConditionAccessor)
	if !ok {
		return
	}

	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}

	conditions := accessor.GetConditions()
	for _, name := range names {
		meta.SetStatusCondition(&conditions, metav1.Condition{
			Type:               name,
			Status:             status,
			Reason:             ReasonSkipped,
			Message:            msg,
			ObservedGeneration: obj.GetGeneration(),
		})
	}
	accessor.SetConditions(conditions)
}

// SetReadyCondition sets the aggregate Ready condition based on the given reason.
// The reason must be one of ReasonComplete, ReasonError, ReasonPending, or ReasonStopped.
func (m *Manager) SetReadyCondition(obj ctrlruntimeclient.Object, reason string) {
	accessor, ok := obj.(ConditionAccessor)
	if !ok {
		return
	}

	cond := metav1.Condition{
		Type:               ReadyCondition,
		ObservedGeneration: obj.GetGeneration(),
		Reason:             reason,
	}

	switch reason {
	case ReasonError:
		cond.Status = metav1.ConditionFalse
		cond.Message = "one or more subroutines encountered an error"
	case ReasonStopped:
		cond.Status = metav1.ConditionFalse
		cond.Message = "one or more subroutines stopped the chain"
	case ReasonPending:
		cond.Status = metav1.ConditionUnknown
		cond.Message = "one or more subroutines are pending"
	default:
		cond.Status = metav1.ConditionTrue
		cond.Message = "all subroutines completed successfully"
	}

	conditions := accessor.GetConditions()
	meta.SetStatusCondition(&conditions, cond)
	accessor.SetConditions(conditions)
}

func (m *Manager) ensureCondition(accessor ConditionAccessor, name string, generation int64) {
	conditions := accessor.GetConditions()
	if meta.FindStatusCondition(conditions, name) != nil {
		return
	}

	meta.SetStatusCondition(&conditions, metav1.Condition{
		Type:               name,
		Status:             metav1.ConditionUnknown,
		Reason:             ReasonUnknown,
		Message:            fmt.Sprintf("awaiting first reconciliation for %s", name),
		ObservedGeneration: generation,
	})
	accessor.SetConditions(conditions)
}
