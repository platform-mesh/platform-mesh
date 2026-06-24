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

	pmmigrationv1alpha1 "go.platform-mesh.io/apis/migration/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/kcp-migration-operator/internal/config"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// KCPMigrationReconciler reconciles a KCPMigration object
type KCPMigrationReconciler struct {
	ctrlruntimeclient.Client
	Scheme *runtime.Scheme
	Log    *logger.Logger
	Config *config.OperatorConfig
}

// NewKCPMigrationReconciler creates a new KCPMigrationReconciler
func NewKCPMigrationReconciler(
	client ctrlruntimeclient.Client,
	scheme *runtime.Scheme,
	log *logger.Logger,
	cfg *config.OperatorConfig,
) *KCPMigrationReconciler {
	return &KCPMigrationReconciler{
		Client: client,
		Scheme: scheme,
		Log:    log,
		Config: cfg,
	}
}

//+kubebuilder:rbac:groups=migration.platform-mesh.io,resources=kcpmigrations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=migration.platform-mesh.io,resources=kcpmigrations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=migration.platform-mesh.io,resources=kcpmigrations/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile handles reconciliation of KCPMigration resources
func (r *KCPMigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.With().
		Str("name", req.Name).
		Str("namespace", req.Namespace).
		Logger()

	log.Info().Msg("reconciling KCPMigration")

	// Fetch the KCPMigration instance
	migration := &pmmigrationv1alpha1.KCPMigration{}
	if err := r.Get(ctx, req.NamespacedName, migration); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug().Msg("KCPMigration not found, likely deleted")
			return ctrl.Result{}, nil
		}
		log.Error().Err(err).Msg("failed to get KCPMigration")
		return ctrl.Result{}, err
	}

	// Log current state
	log.Info().
		Str("sourceAPIVersion", migration.Spec.Source.APIVersion).
		Str("sourceKind", migration.Spec.Source.Kind).
		Str("targetWorkspace", migration.Spec.Transform.TargetWorkspace.Expression).
		Str("phase", string(migration.Status.Phase)).
		Msg("processing KCPMigration")

	// Update status phase if not set
	if migration.Status.Phase == "" {
		migration.Status.Phase = pmmigrationv1alpha1.PhasePending
		if err := r.Status().Update(ctx, migration); err != nil {
			if apierrors.IsConflict(err) {
				log.Debug().Msg("conflict updating status, requeuing")
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error().Err(err).Msg("failed to update status")
			return ctrl.Result{}, err
		}
		log.Info().Msg("initialized status phase to Pending")
	}

	// TODO: Implement subroutines:
	// 1. ValidateSpec - validate the migration spec
	// 2. CreateConfigMap - create sync configuration ConfigMap
	// 3. CreateChildOperator - create child operator deployment
	// 4. UpdateStatus - update migration status

	// For now, just update to Running phase
	if migration.Status.Phase == pmmigrationv1alpha1.PhasePending {
		migration.Status.Phase = pmmigrationv1alpha1.PhaseRunning
		migration.Status.ObservedGeneration = migration.Generation
		if err := r.Status().Update(ctx, migration); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		log.Info().Msg("updated phase to Running")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *KCPMigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pmmigrationv1alpha1.KCPMigration{}).
		Named("kcpmigration").
		Complete(r)
}

var _ reconcile.Reconciler = &KCPMigrationReconciler{}
