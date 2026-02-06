package workspacetype

import (
	"context"

	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/util"
)

const (
	SubroutineName      = "WorkspaceTypeSubroutine"
	SubroutineFinalizer = "workspacetype.core.platform-mesh.io/finalizer"

	rootOrgWorkspaceTypeName     = "org"
	rootWorkspace                = "root"
	rootAccountWorkspaceTypeName = "account"
	rootOrgsWorkspaceTypeName    = "orgs"
	orgsWorkspacePath            = "root:orgs"
)

var _ subroutine.Subroutine = &WorkspaceTypeSubroutine{}

type WorkspaceTypeSubroutine struct {
	orgsClient client.Client
}

func New(orgsClient client.Client) *WorkspaceTypeSubroutine {
	return &WorkspaceTypeSubroutine{
		orgsClient: orgsClient,
	}
}

func (w *WorkspaceTypeSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)

	if instance.Spec.Type != v1alpha1.AccountTypeOrg {
		return ctrl.Result{}, nil
	}

	orgWorkspaceTypeName := util.GetWorkspaceTypeName(instance.Name, instance.Spec.Type)
	accountWorkspaceTypeName := util.GetWorkspaceTypeName(instance.Name, v1alpha1.AccountTypeAccount)

	orgWst := generateOrgWorkspaceType(orgWorkspaceTypeName, accountWorkspaceTypeName, instance.Name)
	accWst := generateAccountWorkspaceType(orgWorkspaceTypeName, accountWorkspaceTypeName, instance.Name)

	if err := w.createOrPatchWorkspaceType(ctx, orgWst); err != nil { // coverage-ignore
		log.Error().Err(err).Str("name", orgWst.Name).Msg("failed to create or update org workspace type")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if err := w.createOrPatchWorkspaceType(ctx, accWst); err != nil { // coverage-ignore
		log.Error().Err(err).Str("name", accWst.Name).Msg("failed to create or update account workspace type")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	return ctrl.Result{}, nil
}

func (w *WorkspaceTypeSubroutine) createOrPatchWorkspaceType(ctx context.Context, desiredWst kcptenancyv1alpha.WorkspaceType) error {
	wst := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: desiredWst.Name}}
	_, err := controllerutil.CreateOrPatch(ctx, w.orgsClient, wst, func() error {
		wst.Spec.Extend = desiredWst.Spec.Extend
		wst.Spec.DefaultChildWorkspaceType = desiredWst.Spec.DefaultChildWorkspaceType
		wst.Spec.LimitAllowedParents = desiredWst.Spec.LimitAllowedParents
		wst.Spec.LimitAllowedChildren = desiredWst.Spec.LimitAllowedChildren
		return nil
	})
	return err
}

func (w *WorkspaceTypeSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)
	if instance.Spec.Type != v1alpha1.AccountTypeOrg {
		return ctrl.Result{}, nil
	}

	orgWorkspaceTypeName := util.GetWorkspaceTypeName(instance.Name, instance.Spec.Type)
	accountWorkspaceTypeName := util.GetWorkspaceTypeName(instance.Name, v1alpha1.AccountTypeAccount)

	if err := w.orgsClient.Delete(ctx, &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: orgWorkspaceTypeName}}); err != nil {
		if !kerrors.IsNotFound(err) {
			log.Error().Err(err).Str("name", orgWorkspaceTypeName).Msg("failed to delete org workspace type")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	}

	if err := w.orgsClient.Delete(ctx, &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: accountWorkspaceTypeName}}); err != nil {
		if !kerrors.IsNotFound(err) { // coverage-ignore
			log.Error().Err(err).Str("name", accountWorkspaceTypeName).Msg("failed to delete account workspace type")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	}

	return ctrl.Result{}, nil
}

func (w *WorkspaceTypeSubroutine) GetName() string {
	return SubroutineName
}

func (w *WorkspaceTypeSubroutine) Finalizers(obj runtimeobject.RuntimeObject) []string {
	account := obj.(*v1alpha1.Account)
	if account.Spec.Type != v1alpha1.AccountTypeOrg {
		return []string{}
	}

	return []string{SubroutineFinalizer}
}

func generateOrgWorkspaceType(orgWorkspaceTypeName, accountWorkspaceTypeName, orgName string) kcptenancyv1alpha.WorkspaceType {
	return kcptenancyv1alpha.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: orgWorkspaceTypeName, Labels: map[string]string{"core.platform-mesh.io/org": orgName}},
		Spec: kcptenancyv1alpha.WorkspaceTypeSpec{
			Extend: kcptenancyv1alpha.WorkspaceTypeExtension{
				With: []kcptenancyv1alpha.WorkspaceTypeReference{
					{
						Name: rootOrgWorkspaceTypeName,
						Path: rootWorkspace,
					},
				},
			},
			DefaultChildWorkspaceType: &kcptenancyv1alpha.WorkspaceTypeReference{
				Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName),
				Path: orgsWorkspacePath,
			},
			LimitAllowedParents: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{
						Name: rootOrgsWorkspaceTypeName,
						Path: rootWorkspace,
					},
				},
			},
			LimitAllowedChildren: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{
						Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName),
						Path: orgsWorkspacePath,
					},
				},
			},
		},
	}
}

func generateAccountWorkspaceType(orgWorkspaceTypeName, accountWorkspaceTypeName, orgName string) kcptenancyv1alpha.WorkspaceType {
	return kcptenancyv1alpha.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: accountWorkspaceTypeName, Labels: map[string]string{"core.platform-mesh.io/org": orgName}},
		Spec: kcptenancyv1alpha.WorkspaceTypeSpec{
			Extend: kcptenancyv1alpha.WorkspaceTypeExtension{
				With: []kcptenancyv1alpha.WorkspaceTypeReference{
					{
						Name: rootAccountWorkspaceTypeName,
						Path: rootWorkspace,
					},
				},
			},
			DefaultChildWorkspaceType: &kcptenancyv1alpha.WorkspaceTypeReference{
				Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
			LimitAllowedParents: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{
						Name: kcptenancyv1alpha.WorkspaceTypeName(orgWorkspaceTypeName),
						Path: orgsWorkspacePath,
					},
					{
						Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName),
						Path: orgsWorkspacePath,
					},
				},
			},
			LimitAllowedChildren: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{
						Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName),
						Path: orgsWorkspacePath,
					},
				},
			},
		},
	}
}
