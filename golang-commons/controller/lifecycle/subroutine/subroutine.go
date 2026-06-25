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

package subroutine

import (
	"context"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	"go.platform-mesh.io/golang-commons/errors"

	ctrl "sigs.k8s.io/controller-runtime"
)

type Subroutine interface {
	Process(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError)
	Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError)
	GetName() string
	Finalizers(instance runtimeobject.RuntimeObject) []string
}

// Terminator can be implemeted to act as KCP terminator[0], i.e. act on
// reconciles for LogicalClusters with deletion timestamp but without finalizer.
// Use together with LifecycleManager.WithTerminator(string) to ensure removal
// of the terminator on successful reconciliation.
//
// [0] https://docs.kcp.io/kcp/v0.30/concepts/workspaces/workspace-termination/
type Terminator interface {
	Terminate(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError)
}

// Initializer can be implemented to act as KCP initializer[0], i.e. act on
// reconciles for LogicalClusters without deletion timestamp. Use together with
// LifecycleManager.WithInitializer(string) to ensure removal of the terminator
// on successful reconciliation.
//
// [0]
// https://docs.kcp.io/kcp/v0.30/concepts/workspaces/workspace-initialization/
type Initializer interface {
	Initialize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError)
}
