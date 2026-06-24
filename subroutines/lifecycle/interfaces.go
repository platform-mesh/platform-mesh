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

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ConditionManager manages per-subroutine and aggregate conditions.
type ConditionManager interface {
	InitUnknownConditions(obj ctrlruntimeclient.Object, subroutineNames []string)
	SetSubroutineCondition(obj ctrlruntimeclient.Object, name string, result subroutines.Result, err error, isFinalize bool)
	SetSkippedConditions(obj ctrlruntimeclient.Object, names []string, ready bool, msg string)
	SetReadyCondition(obj ctrlruntimeclient.Object, reason string)
}

// SpreadManager manages reconciliation spreading.
type SpreadManager interface {
	ReconcileRequired(obj ctrlruntimeclient.Object) bool
	RequeueDelay(obj ctrlruntimeclient.Object) time.Duration
	SetNextReconcileTime(obj ctrlruntimeclient.Object)
	UpdateObservedGeneration(obj ctrlruntimeclient.Object)
	RemoveRefreshLabel(obj ctrlruntimeclient.Object) bool
}
