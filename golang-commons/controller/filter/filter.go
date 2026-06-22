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

package filter

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	DebugLabel = "debug.platform-mesh.io"
)

// DebugResourcesBehaviourPredicate returns whether a resource should be digested
// depending on whether the DebugLabel matches the compareValue.
// To match resources where the label is not set, provide an empty string.
// This should be the default production configuration which can be overwritten for local development.
func DebugResourcesBehaviourPredicate(labelValue string) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			val := e.Object.GetLabels()[DebugLabel]
			return val == labelValue
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			val := e.ObjectNew.GetLabels()[DebugLabel]
			return val == labelValue
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			val := e.Object.GetLabels()[DebugLabel]
			return val == labelValue
		},
		GenericFunc: func(e event.GenericEvent) bool {
			val := e.Object.GetLabels()[DebugLabel]
			return val == labelValue
		},
	}
}
