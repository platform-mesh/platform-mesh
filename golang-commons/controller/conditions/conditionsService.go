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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type conditionsService struct {
	conditionType string
}

func NewConditionsService(conditionType string) ConditionsService {
	return &conditionsService{conditionType: conditionType}
}

// Functions
func setCondition(objectMeta metav1.ObjectMeta, conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string) {
	workflowCondition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: objectMeta.GetGeneration(),
		LastTransitionTime: metav1.Time{Time: time.Now()},
		Reason:             reason,
		Message:            message,
	}
	meta.SetStatusCondition(conditions, workflowCondition)
}

func (c *conditionsService) SetTrue(objectMeta metav1.ObjectMeta, conditions *[]metav1.Condition, reason, message string) {
	setCondition(objectMeta, conditions, c.conditionType, metav1.ConditionTrue, reason, message)
}

func (c *conditionsService) SetFalse(objectMeta metav1.ObjectMeta, conditions *[]metav1.Condition, reason, message string) {
	setCondition(objectMeta, conditions, c.conditionType, metav1.ConditionFalse, reason, message)
}

func (c *conditionsService) GetStatus(conditions []metav1.Condition) *metav1.Condition {
	return meta.FindStatusCondition(conditions, c.conditionType)
}

func (c *conditionsService) IsStatusTrue(conditions []metav1.Condition) bool {
	cond := c.GetStatus(conditions)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		return true
	}
	return false
}
