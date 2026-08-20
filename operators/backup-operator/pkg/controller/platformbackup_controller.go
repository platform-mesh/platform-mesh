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
	"go.platform-mesh.io/backup-operator/pkg/backup"
	operatorcfg "go.platform-mesh.io/backup-operator/pkg/config"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const InfrastructureNamespace = "platform-mesh-system"

// PlatformBackupReconciler reconciles PlatformBackup resources.
type PlatformBackupReconciler struct {
	client     ctrlruntimeclient.Client
	namespace  string
	processors []subroutines.StatusProcessor
}

func NewPlatformBackupReconciler(mgr ctrl.Manager) *PlatformBackupReconciler {
	cl := mgr.GetClient()
	return &PlatformBackupReconciler{
		client:    mgr.GetClient(),
		namespace: operatorcfg.DefaultNamespace,
		processors: []subroutines.StatusProcessor{
			backup.NewCredentialInventorySubroutine(InfrastructureNamespace, cl),
			backup.NewEtcdCaptureSubroutine(InfrastructureNamespace, cl),
			backup.NewCNPGCaptureSubroutine(InfrastructureNamespace, cl),
			backup.NewOpenFGADumpSubroutine(cl),
			backup.NewKCPPolicyCaptureSubroutine(cl),
			backup.NewVeleroCaptureSubroutine(InfrastructureNamespace, cl),
		},
	}
}

func (r *PlatformBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("PlatformBackupReconciler").
		For(&pmbackupv1alpha1.PlatformBackup{}).
		Complete(r)
}

func (r *PlatformBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	obj := &pmbackupv1alpha1.PlatformBackup{}
	if err := r.client.Get(ctx, req.NamespacedName, obj); err != nil {
		if ctrlruntimeclient.IgnoreNotFound(err) != nil {
			log.Error().
				Str("controller", "PlatformBackup").
				Str("platformBackup", req.Name).
				Err(err).
				Msg("failed to load PlatformBackup")
		}
		return ctrl.Result{}, ctrlruntimeclient.IgnoreNotFound(err)
	}

	log.Info().
		Str("controller", "PlatformBackup").
		Str("platformBackup", obj.Name).
		Int64("generation", obj.Generation).
		Msg("reconciling PlatformBackup")

	for _, p := range r.processors {
		log.Info().
			Str("controller", "PlatformBackup").
			Str("platformBackup", obj.Name).
			Str("processor", p.GetName()).
			Msg("processing PlatformBackup subroutine")
		res, statusChanged, err := p.Process(ctx, obj)
		if err != nil {
			log.Error().
				Str("controller", "PlatformBackup").
				Str("platformBackup", obj.Name).
				Str("processor", p.GetName()).
				Err(err).
				Msg("PlatformBackup subroutine failed")
			return ctrl.Result{}, err
		}

		if statusChanged {
			if err = r.client.Status().Update(ctx, obj); err != nil {
				log.Error().
					Str("controller", "PlatformBackup").
					Str("platformBackup", obj.Name).
					Str("processor", p.GetName()).
					Err(err).
					Msg("failed to update PlatformBackup status")
				return ctrl.Result{}, err
			}
		}

		if res.Requeue() > 0 {
			log.Info().
				Str("controller", "PlatformBackup").
				Str("platformBackup", obj.Name).
				Str("processor", p.GetName()).
				Dur("requeueAfter", res.Requeue()).
				Msg("requeueing PlatformBackup")
			return ctrl.Result{RequeueAfter: res.Requeue()}, nil
		}
	}
	return ctrl.Result{}, nil
}
