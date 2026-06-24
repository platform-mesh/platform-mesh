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

package api

import (
	"context"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Lifecycle interface {
	Config() Config
	Log() *logger.Logger
	Spreader() SpreadManager
	ConditionsManager() ConditionManager
	PrepareContextFunc() PrepareContextFunc
	Subroutines() []subroutine.Subroutine
}

type InitializingLifecycle interface {
	Initializer() string
}

type TerminatingLifecycle interface {
	Terminator() string
}

type PrepareContextFunc func(ctx context.Context, instance runtimeobject.RuntimeObject) (context.Context, errors.OperatorError)

type Config struct {
	OperatorName   string
	ControllerName string
	ReadOnly       bool
}

type ConditionManager interface {
	SetInstanceConditionUnknownIfNotSet(conditions *[]metav1.Condition, observedGeneration int64) bool
	SetSubroutineConditionToUnknownIfNotSet(conditions *[]metav1.Condition, observedGeneration int64, subroutine subroutine.Subroutine, isFinalize bool, log *logger.Logger) bool
	SetSubroutineCondition(conditions *[]metav1.Condition, observedGeneration int64, subroutine subroutine.Subroutine, subroutineResult ctrl.Result, subroutineErr error, isFinalize bool, log *logger.Logger) bool
	SetInstanceConditionReady(conditions *[]metav1.Condition, observedGeneration int64, status metav1.ConditionStatus) bool
}

type RuntimeObjectConditions interface {
	GetConditions() []metav1.Condition
	SetConditions([]metav1.Condition)
}

type SpreadManager interface {
	ReconcileRequired(instance runtimeobject.RuntimeObject, log *logger.Logger) bool
	OnNextReconcile(instance runtimeobject.RuntimeObject, log *logger.Logger) (ctrl.Result, error)
	RemoveRefreshLabelIfExists(instance runtimeobject.RuntimeObject) bool
	SetNextReconcileTime(instanceStatusObj RuntimeObjectSpreadReconcileStatus, log *logger.Logger)
	UpdateObservedGeneration(instanceStatusObj RuntimeObjectSpreadReconcileStatus, log *logger.Logger)
}

type RuntimeObjectSpreadReconcileStatus interface {
	GetGeneration() int64
	GetObservedGeneration() int64
	SetObservedGeneration(int64)
	GetNextReconcileTime() metav1.Time
	SetNextReconcileTime(time metav1.Time)
}
