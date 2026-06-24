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
	"context"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

// PlatformRestoreReconciler reconciles PlatformRestore resources.
type PlatformRestoreReconciler struct {
	lifecycle *lifecycle.Lifecycle
}

func NewPlatformRestoreReconciler(mgr mcmanager.Manager) *PlatformRestoreReconciler {
	lc := lifecycle.New(mgr, "PlatformRestoreReconciler", func() ctrlruntimeclient.Object {
		return &pmbackupv1alpha1.PlatformRestore{}
	}, []subroutines.Subroutine{}...).WithConditions(conditions.NewManager())

	return &PlatformRestoreReconciler{lifecycle: lc}
}

func (r *PlatformRestoreReconciler) SetupWithManager(mgr mcmanager.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named("PlatformRestoreReconciler").
		For(&pmbackupv1alpha1.PlatformRestore{}).
		Complete(r)
}

func (r *PlatformRestoreReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}
