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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type ConditionsService interface {
	ConditionsSetter
	ConditionsGetter
}

type ConditionsSetter interface {
	SetTrue(objectMeta metav1.ObjectMeta, conditions *[]metav1.Condition, reason, message string)
	SetFalse(objectMeta metav1.ObjectMeta, conditions *[]metav1.Condition, reason, message string)
}

type ConditionsGetter interface {
	GetStatus(conditions []metav1.Condition) *metav1.Condition
	IsStatusTrue(conditions []metav1.Condition) bool
}
