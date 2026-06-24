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

// PlatformBackupReconciler reconciles PlatformBackup resources.
type PlatformBackupReconciler struct {
	lifecycle *lifecycle.Lifecycle
}

func NewPlatformBackupReconciler(mgr mcmanager.Manager) *PlatformBackupReconciler {
	lc := lifecycle.New(mgr, "PlatformBackupReconciler", func() ctrlruntimeclient.Object {
		return &pmbackupv1alpha1.PlatformBackup{}
	}, []subroutines.Subroutine{}...).WithConditions(conditions.NewManager())

	return &PlatformBackupReconciler{lifecycle: lc}
}

func (r *PlatformBackupReconciler) SetupWithManager(mgr mcmanager.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named("PlatformBackupReconciler").
		For(&pmbackupv1alpha1.PlatformBackup{}).
		Complete(r)
}

func (r *PlatformBackupReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}
