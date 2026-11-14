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
	"go.platform-mesh.io/backup-operator/pkg/storage"
	subbackup "go.platform-mesh.io/backup-operator/pkg/subroutines/backup"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const backupReconcilerName = "BackupReconciler"

// BackupReconciler runs one backup for each Backup object.
type BackupReconciler struct {
	lifecycle *lifecycle.Lifecycle
}

// +kubebuilder:rbac:groups=backup.platform-mesh.io,resources=backups;backupstorages,verbs=get;list;watch
// +kubebuilder:rbac:groups=backup.platform-mesh.io,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=druid.gardener.cloud,resources=etcds,verbs=get;list;watch
// +kubebuilder:rbac:groups=druid.gardener.cloud,resources=etcdopstasks,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates;issuers,verbs=get;list;watch
// +kubebuilder:rbac:groups=operator.kcp.io,resources=rootshards;shards;cacheservers;frontproxies;kubeconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func NewBackupReconciler(mgr mcmanager.Manager, stores storage.Factory) *BackupReconciler {
	cl := mgr.GetLocalManager().GetClient()
	lc := lifecycle.New(mgr, backupReconcilerName, func() ctrlruntimeclient.Object {
		return &pmbackupv1alpha1.Backup{}
	}, subbackup.New(cl, stores, subbackup.FromBackupStorage(cl, stores))...).WithConditions(conditions.NewManager())
	return &BackupReconciler{lifecycle: lc}
}

func (r *BackupReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}

func (r *BackupReconciler) SetupWithManager(mgr mcmanager.Manager, maxConcurrent int) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named(backupReconcilerName).
		For(&pmbackupv1alpha1.Backup{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(false)).
		WithOptions(controller.TypedOptions[mcreconcile.Request]{MaxConcurrentReconciles: maxConcurrent}).
		Complete(r)
}
