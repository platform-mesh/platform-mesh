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

package lifecycle

import (
	"time"

	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConditionManager manages per-subroutine and aggregate conditions.
type ConditionManager interface {
	InitUnknownConditions(obj client.Object, subroutineNames []string)
	SetSubroutineCondition(obj client.Object, name string, result subroutines.Result, err error, isFinalize bool)
	SetSkippedConditions(obj client.Object, names []string, ready bool, msg string)
	SetReadyCondition(obj client.Object, reason string)
}

// SpreadManager manages reconciliation spreading.
type SpreadManager interface {
	ReconcileRequired(obj client.Object) bool
	RequeueDelay(obj client.Object) time.Duration
	SetNextReconcileTime(obj client.Object)
	UpdateObservedGeneration(obj client.Object)
	RemoveRefreshLabel(obj client.Object) bool
}
