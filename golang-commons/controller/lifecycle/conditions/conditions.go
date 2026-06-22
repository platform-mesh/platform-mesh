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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/logger"
)

const (
	ConditionReady = "Ready"

	messageResourceReady      = "The resource is ready"
	messageResourceNotReady   = "The resource is not ready"
	messageResourceProcessing = "The resource is processing"

	reasonComplete   = "Complete"
	reasonProcessing = "Processing"
	reasonError      = "Error"

	subroutineReadyConditionFormatString    = "%s_Ready"
	subroutineFinalizeConditionFormatString = "%s_Finalize"

	subroutineMessageProcessingFormatString = "The %s is processing"
	subroutineMessageCompleteFormatString   = "The %s is complete"
	subroutineMessageErrorFormatString      = "The %s has an error: %s"
)

type ConditionManager struct{}

func NewConditionManager() *ConditionManager {
	return &ConditionManager{}
}

// Set the Condition of the instance to be ready
func (c *ConditionManager) SetInstanceConditionReady(conditions *[]metav1.Condition, observedGeneration int64, status metav1.ConditionStatus) bool {
	var msg string
	switch status {
	case metav1.ConditionTrue:
		msg = messageResourceReady
	case metav1.ConditionFalse:
		msg = messageResourceNotReady
	default:
		msg = messageResourceProcessing
	}
	return meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             status,
		Message:            msg,
		Reason:             reasonComplete,
		ObservedGeneration: observedGeneration,
	})
}

// Set the Condition to be Unknown in case it is not set yet
func (c *ConditionManager) SetInstanceConditionUnknownIfNotSet(conditions *[]metav1.Condition, observedGeneration int64) bool {
	existingCondition := meta.FindStatusCondition(*conditions, ConditionReady)
	if existingCondition == nil {
		return c.SetInstanceConditionReady(conditions, observedGeneration, metav1.ConditionUnknown)
	}
	return false
}

func (c *ConditionManager) SetSubroutineConditionToUnknownIfNotSet(conditions *[]metav1.Condition, observedGeneration int64, subroutine subroutine.Subroutine, isFinalize bool, log *logger.Logger) bool {
	conditionName, conditionMessage := getConditionNameAndMessage(subroutine, isFinalize)

	existingCondition := meta.FindStatusCondition(*conditions, conditionName)
	if existingCondition == nil {
		changed := meta.SetStatusCondition(conditions,
			metav1.Condition{Type: conditionName, Status: metav1.ConditionUnknown, Message: fmt.Sprintf(subroutineMessageProcessingFormatString, conditionMessage), Reason: reasonProcessing, ObservedGeneration: observedGeneration})
		if changed {
			log.Info().Str("type", conditionName).Msg("updated condition")
		}
		return changed
	}
	return false
}

func getConditionNameAndMessage(subroutine subroutine.Subroutine, isFinalize bool) (string, string) {
	conditionName := fmt.Sprintf(subroutineReadyConditionFormatString, subroutine.GetName())
	conditionMessage := "subroutine"
	if isFinalize {
		conditionName = fmt.Sprintf(subroutineFinalizeConditionFormatString, subroutine.GetName())
		conditionMessage = "subroutine finalization"
	}
	return conditionName, conditionMessage
}

// Set Subroutines Conditions
func (c *ConditionManager) SetSubroutineCondition(conditions *[]metav1.Condition, observedGeneration int64, subroutine subroutine.Subroutine, subroutineResult ctrl.Result, subroutineErr error, isFinalize bool, log *logger.Logger) bool {
	conditionName, conditionMessage := getConditionNameAndMessage(subroutine, isFinalize)

	// processing complete
	if subroutineErr == nil && subroutineResult.RequeueAfter == 0 {
		return meta.SetStatusCondition(conditions,
			metav1.Condition{Type: conditionName, Status: metav1.ConditionTrue, Message: fmt.Sprintf(subroutineMessageCompleteFormatString, conditionMessage), Reason: reasonComplete, ObservedGeneration: observedGeneration})
	}
	// processing is still processing
	if subroutineErr == nil && subroutineResult.RequeueAfter > 0 {
		return meta.SetStatusCondition(conditions,
			metav1.Condition{Type: conditionName, Status: metav1.ConditionUnknown, Message: fmt.Sprintf(subroutineMessageProcessingFormatString, conditionMessage), Reason: reasonProcessing, ObservedGeneration: observedGeneration})
	}
	// processing failed
	var sErr error
	if subroutineErr != nil {
		sErr = subroutineErr
	}
	changed := meta.SetStatusCondition(conditions,
		metav1.Condition{Type: conditionName, Status: metav1.ConditionFalse, Message: fmt.Sprintf(subroutineMessageErrorFormatString, conditionMessage, sErr), Reason: reasonError, ObservedGeneration: observedGeneration})
	if changed {
		log.Info().Str("type", conditionName).Msg("updated condition")
	}
	return changed
}
