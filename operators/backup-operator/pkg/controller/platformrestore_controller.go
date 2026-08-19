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
	"fmt"
	"time"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// PlatformRestoreReconciler reconciles PlatformRestore resources.
type PlatformRestoreReconciler struct {
	client     ctrlruntimeclient.Client
	processors []subroutines.StatusProcessor
}

const portalAccessRepairAnnotation = "restore.platform-mesh.io/portal-access-repair"

func NewPlatformRestoreReconciler(mgr ctrl.Manager) *PlatformRestoreReconciler {
	cl := mgr.GetClient()
	return &PlatformRestoreReconciler{
		client: mgr.GetClient(),
		processors: []subroutines.StatusProcessor{
			restore.NewQuiescePlatformSubroutine(cl),
			restore.NewEtcdRestoreSubroutine(cl),
			restore.NewCNPGRestoreSubroutine(cl),
			restore.NewVeleroRestoreSubroutine(cl),
			restore.NewOpenFGADumpRestoreSubroutine(cl),
			restore.NewControlPlaneRestartSubroutine(cl),
			restore.NewPlatformRecoverySubroutine(cl),
			restore.NewIdentityValidationSubroutine(cl),
		},
	}
}

func (r *PlatformRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("PlatformRestoreReconciler").
		For(&pmbackupv1alpha1.PlatformRestore{}).
		Complete(r)
}

func (r *PlatformRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	obj := &pmbackupv1alpha1.PlatformRestore{}
	if err := r.client.Get(ctx, req.NamespacedName, obj); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error().
				Str("controller", "PlatformRestore").
				Str("platformRestore", req.Name).
				Err(err).
				Msg("failed to load PlatformRestore")
		}
		return ctrl.Result{}, ctrlruntimeclient.IgnoreNotFound(err)
	}

	log.Info().
		Str("controller", "PlatformRestore").
		Str("platformRestore", obj.Name).
		Int64("generation", obj.Generation).
		Str("phase", string(obj.Status.Phase)).
		Msg("reconciling PlatformRestore")

	terminal := obj.Status.Phase == pmbackupv1alpha1.RestorePhaseSucceeded ||
		obj.Status.Phase == pmbackupv1alpha1.RestorePhaseFailed
	portalAccessRepairRequested := obj.Annotations[portalAccessRepairAnnotation] != ""
	if terminal && !portalAccessRepairRequested {
		log.Info().
			Str("controller", "PlatformRestore").
			Str("platformRestore", obj.Name).
			Str("phase", string(obj.Status.Phase)).
			Msg("PlatformRestore is terminal; skipping reconciliation")

		return ctrl.Result{}, nil
	}
	if terminal {
		// A completed restore can need a post-upgrade Portal/KCP RBAC repair.
		// The individual restore subroutines remain terminal no-ops; only the
		// identity-validation repair path performs work.
		log.Info().
			Str("controller", "PlatformRestore").
			Str("platformRestore", obj.Name).
			Str("annotation", portalAccessRepairAnnotation).
			Msg("processing terminal Portal access repair")
	}

	for _, p := range r.processors {
		log.Info().
			Str("controller", "PlatformRestore").
			Str("platformRestore", obj.Name).
			Str("processor", p.GetName()).
			Msg("processing PlatformRestore subroutine")

		before := obj.DeepCopy()
		res, statusChanged, err := p.Process(ctx, obj)
		statusChanged = statusChanged || !apiequality.Semantic.DeepEqual(before.Status, obj.Status)

		if statusChanged {
			if patchErr := r.client.Status().Patch(ctx, obj, ctrlruntimeclient.MergeFrom(before)); patchErr != nil {
				if apierrors.IsConflict(patchErr) {
					log.Info().
						Str("controller", "PlatformRestore").
						Str("platformRestore", obj.Name).
						Str("processor", p.GetName()).
						Msg("PlatformRestore status update conflicted; requeueing")
					return ctrl.Result{RequeueAfter: time.Second}, nil
				}
				log.Error().
					Str("controller", "PlatformRestore").
					Str("platformRestore", obj.Name).
					Str("processor", p.GetName()).
					Err(patchErr).
					Msg("failed to patch PlatformRestore status")
				return ctrl.Result{}, fmt.Errorf("patch PlatformRestore status after %s: %w", p.GetName(), patchErr)
			}
		}

		if err != nil {
			log.Error().
				Str("controller", "PlatformRestore").
				Str("platformRestore", obj.Name).
				Str("processor", p.GetName()).
				Err(err).
				Msg("PlatformRestore subroutine failed")
			return ctrl.Result{}, err
		}

		if statusChanged {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		if res.Requeue() > 0 {
			log.Info().
				Str("controller", "PlatformRestore").
				Str("platformRestore", obj.Name).
				Str("processor", p.GetName()).
				Str("reason", res.Message()).
				Dur("requeueAfter", res.Requeue()).
				Msg("Requeue")
			return ctrl.Result{RequeueAfter: res.Requeue()}, nil
		}
	}

	return ctrl.Result{}, nil
}
