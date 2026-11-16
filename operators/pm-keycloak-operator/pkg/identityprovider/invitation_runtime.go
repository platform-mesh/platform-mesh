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

package identityprovider

import (
	"context"
	"errors"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/kcp-dev/logicalcluster/v3"
)

func (r *Runtime) ReconcileInvitation(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, r.RequestTimeout)
	defer cancel()
	cluster := logicalcluster.Name(req.ClusterName)
	if !cluster.IsValid() {
		return ctrl.Result{}, errors.New("invitation reconciliation requires an immutable logical cluster")
	}
	requests, err := r.ClientForCluster(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	invitation := &pmidentityv1alpha1.Invitation{}
	if err := requests.Get(ctx, req.NamespacedName, invitation); err != nil {
		return ctrl.Result{}, ctrlruntimeclient.IgnoreNotFound(err)
	}
	engine := &InvitationReconciler{
		Requests: requests, Private: r.Private, Tenants: r.Organizations,
		Cluster: cluster, TenantCluster: r.OrganizationCluster, RuntimeNamespace: r.RuntimeNamespace,
	}
	if !invitation.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, engine.Reconcile(ctx, nil, invitation)
	}
	info := &pmcorev1alpha1.AccountInfo{}
	if err := requests.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "account"}, info); err != nil {
		return ctrl.Result{}, err
	}
	if info.Spec.OIDC == nil || info.Spec.OIDC.Identity == nil {
		return ctrl.Result{}, errors.New("invitation requires a UID-bound identity projection; legacy realm-name inference is forbidden")
	}
	if info.Spec.OIDC.Identity.ProviderClassRef != r.ClassName {
		return ctrl.Result{}, nil
	}
	class, config, err := r.ObserveClass(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve invitation provider: %w", err)
	}
	defer config.Close()
	engine.Backend, engine.ProviderIdentity = config.Backend, config.Identity
	if err := engine.Reconcile(ctx, class, invitation); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.ResyncInterval}, nil
}
