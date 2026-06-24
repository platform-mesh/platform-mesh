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

package workspacetype

import (
	"context"
	"maps"

	"go.platform-mesh.io/account-operator/internal/metrics"
	"go.platform-mesh.io/account-operator/pkg/subroutines/util"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

var _ subroutines.Processor = (*WorkspaceTypeSubroutine)(nil)
var _ subroutines.Finalizer = (*WorkspaceTypeSubroutine)(nil)

const (
	SubroutineName      = "WorkspaceTypeSubroutine"
	SubroutineFinalizer = "workspacetype.core.platform-mesh.io/finalizer"

	rootOrgWorkspaceTypeName     = "org"
	rootWorkspace                = "root"
	rootAccountWorkspaceTypeName = "account"
	rootOrgsWorkspaceTypeName    = "orgs"
	orgsWorkspacePath            = "root:orgs"
)

type WorkspaceTypeSubroutine struct {
	mgr mcmanager.Manager
}

func New(mgr mcmanager.Manager) *WorkspaceTypeSubroutine {
	return &WorkspaceTypeSubroutine{
		mgr: mgr,
	}
}

func (w *WorkspaceTypeSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	instance := obj.(*pmcorev1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)

	if instance.Spec.Type != pmcorev1alpha1.AccountTypeOrg {
		return subroutines.OK(), nil
	}

	orgWorkspaceTypeName := util.GetWorkspaceTypeName(instance.Name, instance.Spec.Type)
	accountWorkspaceTypeName := util.GetWorkspaceTypeName(instance.Name, pmcorev1alpha1.AccountTypeAccount)

	orgWst := generateOrgWorkspaceType(orgWorkspaceTypeName, accountWorkspaceTypeName, instance.Name)
	accWst := generateAccountWorkspaceType(orgWorkspaceTypeName, accountWorkspaceTypeName, instance.Name)

	if err := w.createOrPatchWorkspaceType(ctx, orgWst, "org"); err != nil {
		log.Error().Err(err).Str("name", orgWst.Name).Msg("failed to create or update org workspace type")
		return subroutines.OK(), err
	}

	if err := w.createOrPatchWorkspaceType(ctx, accWst, "account"); err != nil {
		log.Error().Err(err).Str("name", accWst.Name).Msg("failed to create or update account workspace type")
		return subroutines.OK(), err
	}

	return subroutines.OK(), nil
}

func (w *WorkspaceTypeSubroutine) createOrPatchWorkspaceType(ctx context.Context, desiredWst kcptenancyv1alpha1.WorkspaceType, wstKind string) error {
	orgsCluster, err := w.mgr.GetCluster(ctx, orgsWorkspacePath)
	if err != nil {
		return err
	}
	orgsClusterClient := orgsCluster.GetClient()

	wst := &kcptenancyv1alpha1.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: desiredWst.Name}}
	result, err := controllerutil.CreateOrPatch(ctx, orgsClusterClient, wst, func() error {
		if wst.Labels == nil {
			wst.Labels = make(map[string]string)
		}
		maps.Copy(wst.Labels, desiredWst.Labels)

		wst.Spec.Extend = desiredWst.Spec.Extend
		wst.Spec.DefaultChildWorkspaceType = desiredWst.Spec.DefaultChildWorkspaceType
		wst.Spec.LimitAllowedParents = desiredWst.Spec.LimitAllowedParents
		wst.Spec.LimitAllowedChildren = desiredWst.Spec.LimitAllowedChildren
		return nil
	})
	if err != nil {
		return err
	}
	metrics.WorkspaceTypeOperations.WithLabelValues(string(result), wstKind).Inc()
	return nil
}

func (w *WorkspaceTypeSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	instance := obj.(*pmcorev1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)
	if instance.Spec.Type != pmcorev1alpha1.AccountTypeOrg {
		return subroutines.OK(), nil
	}

	orgsCluster, err := w.mgr.GetCluster(ctx, "root:orgs")
	if err != nil {
		return subroutines.OK(), err
	}
	orgsClusterClient := orgsCluster.GetClient()

	orgWorkspaceTypeName := util.GetWorkspaceTypeName(instance.Name, instance.Spec.Type)
	accountWorkspaceTypeName := util.GetWorkspaceTypeName(instance.Name, pmcorev1alpha1.AccountTypeAccount)

	if err := orgsClusterClient.Delete(ctx, &kcptenancyv1alpha1.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: orgWorkspaceTypeName}}); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error().Err(err).Str("name", orgWorkspaceTypeName).Msg("failed to delete org workspace type")
			return subroutines.OK(), err
		}
	}

	if err := orgsClusterClient.Delete(ctx, &kcptenancyv1alpha1.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: accountWorkspaceTypeName}}); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error().Err(err).Str("name", accountWorkspaceTypeName).Msg("failed to delete account workspace type")
			return subroutines.OK(), err
		}
	}

	return subroutines.OK(), nil
}

func (w *WorkspaceTypeSubroutine) GetName() string {
	return SubroutineName
}

func (w *WorkspaceTypeSubroutine) Finalizers(obj ctrlruntimeclient.Object) []string {
	account := obj.(*pmcorev1alpha1.Account)
	if account.Spec.Type != pmcorev1alpha1.AccountTypeOrg {
		return []string{}
	}

	return []string{SubroutineFinalizer}
}

func generateOrgWorkspaceType(orgWorkspaceTypeName, accountWorkspaceTypeName, orgName string) kcptenancyv1alpha1.WorkspaceType {
	return kcptenancyv1alpha1.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: orgWorkspaceTypeName, Labels: map[string]string{"core.platform-mesh.io/org": orgName}},
		Spec: kcptenancyv1alpha1.WorkspaceTypeSpec{
			Extend: kcptenancyv1alpha1.WorkspaceTypeExtension{
				With: []kcptenancyv1alpha1.WorkspaceTypeReference{
					{
						Name: rootOrgWorkspaceTypeName,
						Path: rootWorkspace,
					},
				},
			},
			DefaultChildWorkspaceType: &kcptenancyv1alpha1.WorkspaceTypeReference{
				Name: kcptenancyv1alpha1.WorkspaceTypeName(accountWorkspaceTypeName),
				Path: orgsWorkspacePath,
			},
			LimitAllowedParents: &kcptenancyv1alpha1.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha1.WorkspaceTypeReference{
					{
						Name: rootOrgsWorkspaceTypeName,
						Path: rootWorkspace,
					},
				},
			},
			LimitAllowedChildren: &kcptenancyv1alpha1.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha1.WorkspaceTypeReference{
					{
						Name: kcptenancyv1alpha1.WorkspaceTypeName(accountWorkspaceTypeName),
						Path: orgsWorkspacePath,
					},
				},
			},
		},
	}
}

func generateAccountWorkspaceType(orgWorkspaceTypeName, accountWorkspaceTypeName, orgName string) kcptenancyv1alpha1.WorkspaceType {
	return kcptenancyv1alpha1.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: accountWorkspaceTypeName, Labels: map[string]string{"core.platform-mesh.io/org": orgName}},
		Spec: kcptenancyv1alpha1.WorkspaceTypeSpec{
			Extend: kcptenancyv1alpha1.WorkspaceTypeExtension{
				With: []kcptenancyv1alpha1.WorkspaceTypeReference{
					{
						Name: rootAccountWorkspaceTypeName,
						Path: rootWorkspace,
					},
				},
			},
			DefaultChildWorkspaceType: &kcptenancyv1alpha1.WorkspaceTypeReference{
				Name: kcptenancyv1alpha1.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath,
			},
			LimitAllowedParents: &kcptenancyv1alpha1.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha1.WorkspaceTypeReference{
					{
						Name: kcptenancyv1alpha1.WorkspaceTypeName(orgWorkspaceTypeName),
						Path: orgsWorkspacePath,
					},
					{
						Name: kcptenancyv1alpha1.WorkspaceTypeName(accountWorkspaceTypeName),
						Path: orgsWorkspacePath,
					},
				},
			},
			LimitAllowedChildren: &kcptenancyv1alpha1.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha1.WorkspaceTypeReference{
					{
						Name: kcptenancyv1alpha1.WorkspaceTypeName(accountWorkspaceTypeName),
						Path: orgsWorkspacePath,
					},
				},
			},
		},
	}
}
