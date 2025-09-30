package subroutines

import (
	"context"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
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
)

const (
	workspaceTypeSubroutineName           = "WorkspaceTypeSubroutine"
	workspaceTypeSubroutineFinalizer      = "workspacetype.core.platform-mesh.io/finalizer"
	rootOrgWorkspaceTypeName              = "org"
	rootOrgWorkspaceTypeWorkspacePath     = "root"
	rootAccountWorkspaceTypeName          = "account"
	rootAccountWorkspaceTypeWorkspacePath = "root"
	rootOrgsWorkspaceTypeName             = "orgs"
)

var _ subroutine.Subroutine = &WorkspaceTypeSubroutine{}

type WorkspaceTypeSubroutine struct {
	orgsClient client.Client
}

func (w WorkspaceTypeSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)

	if instance.Spec.Type != v1alpha1.AccountTypeOrg {
		// Only process org accounts
		return ctrl.Result{}, nil
	}

	orgWorkspaceTypeName := generateOrganizationWorkspaceTypeName(instance.Name)
	accountWorkspaceTypeName := generateAccountWorkspaceTypeName(instance.Name)
	orgWst := generateOrgWorkspaceType(instance, orgWorkspaceTypeName, accountWorkspaceTypeName)
	accWst := generateAccountWorkspaceType(instance, orgWorkspaceTypeName, accountWorkspaceTypeName)

	err := w.createOrUpdateWorkspaceType(ctx, orgWst)
	if err != nil {
		log.Error().Err(err).Str("name", accWst.Name).Msg("failed to create or update org workspace type")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	err = w.createOrUpdateWorkspaceType(ctx, accWst)
	if err != nil {
		log.Error().Err(err).Str("name", accWst.Name).Msg("failed to create or update account workspace type")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	return ctrl.Result{}, nil
}

func (w WorkspaceTypeSubroutine) createOrUpdateWorkspaceType(ctx context.Context, desiredWst kcptenancyv1alpha.WorkspaceType) error {
	wst := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: desiredWst.Name}}
	_, err := controllerutil.CreateOrUpdate(ctx, w.orgsClient, wst, func() error {
		wst.Spec = desiredWst.Spec
		return nil
	})
	return err
}

func (w WorkspaceTypeSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)
	if instance.Spec.Type != v1alpha1.AccountTypeOrg {
		// Only process org accounts
		return ctrl.Result{}, nil
	}

	orgWorkspaceTypeName := generateOrganizationWorkspaceTypeName(instance.Name)
	accountWorkspaceTypeName := generateAccountWorkspaceTypeName(instance.Name)

	err := w.orgsClient.Delete(ctx, &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: orgWorkspaceTypeName}})
	if err != nil {
		if !kerrors.IsNotFound(err) {
			log.Error().Err(err).Str("name", orgWorkspaceTypeName).Msg("failed to delete org workspace")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	}
	err = w.orgsClient.Delete(ctx, &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: accountWorkspaceTypeName}})
	if err != nil {
		if !kerrors.IsNotFound(err) {
			log.Error().Err(err).Str("name", accountWorkspaceTypeName).Msg("failed to delete acc workspace")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
	}

	return ctrl.Result{}, nil
}

func (w WorkspaceTypeSubroutine) GetName() string {
	return workspaceTypeSubroutineName
}

func (w WorkspaceTypeSubroutine) Finalizers() []string {
	return []string{workspaceTypeSubroutineFinalizer}
}

func NewWorkspaceTypeSubroutine(mgr ctrl.Manager) *WorkspaceTypeSubroutine {
	clientCfg, err := createOrganizationRestConfig(mgr.GetConfig())
	if err != nil {
		panic(err)
	}
	orgsClient, err := client.New(clientCfg, client.Options{
		Scheme: mgr.GetScheme(),
	})
	if err != nil {
		panic(err)
	}
	return &WorkspaceTypeSubroutine{orgsClient: orgsClient}
}

func generateOrgWorkspaceType(instance *v1alpha1.Account, orgWorkspaceTypeName, accountWorkspaceTypeName string) kcptenancyv1alpha.WorkspaceType {
	return kcptenancyv1alpha.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: orgWorkspaceTypeName},
		Spec: kcptenancyv1alpha.WorkspaceTypeSpec{
			Extend: kcptenancyv1alpha.WorkspaceTypeExtension{
				With: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: rootOrgWorkspaceTypeName, Path: rootOrgWorkspaceTypeWorkspacePath},
				},
			},
			DefaultChildWorkspaceType: &kcptenancyv1alpha.WorkspaceTypeReference{
				Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
			LimitAllowedParents: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: rootOrgsWorkspaceTypeName, Path: rootOrgWorkspaceTypeWorkspacePath},
				},
			},
			LimitAllowedChildren: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
				},
			},
			AuthenticationConfigurations: []kcptenancyv1alpha.AuthenticationConfigurationReference{
				{
					Name: instance.Name,
				},
			},
		},
	}
}

func generateAccountWorkspaceType(instance *v1alpha1.Account, orgWorkspaceTypeName, accountWorkspaceTypeName string) kcptenancyv1alpha.WorkspaceType {
	return kcptenancyv1alpha.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: accountWorkspaceTypeName},
		Spec: kcptenancyv1alpha.WorkspaceTypeSpec{
			Extend: kcptenancyv1alpha.WorkspaceTypeExtension{
				With: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: rootAccountWorkspaceTypeName, Path: rootAccountWorkspaceTypeWorkspacePath},
				},
			},
			DefaultChildWorkspaceType: &kcptenancyv1alpha.WorkspaceTypeReference{
				Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
			LimitAllowedParents: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: kcptenancyv1alpha.WorkspaceTypeName(orgWorkspaceTypeName), Path: orgsWorkspacePath},
					{Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
				},
			},
			LimitAllowedChildren: &kcptenancyv1alpha.WorkspaceTypeSelector{
				Types: []kcptenancyv1alpha.WorkspaceTypeReference{
					{Name: kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), Path: orgsWorkspacePath},
				},
			},
			AuthenticationConfigurations: []kcptenancyv1alpha.AuthenticationConfigurationReference{
				{
					Name: instance.Name,
				},
			},
		},
	}
}
