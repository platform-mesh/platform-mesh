package subroutine

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	accountv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	lifecyclecontrollerruntime "github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	securityv1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
)

func NewAuthorizationModelGenerationSubroutine(mcMgr mcmanager.Manager) *AuthorizationModelGenerationSubroutine {
	return &AuthorizationModelGenerationSubroutine{
		mgr: mcMgr,
	}
}

var _ lifecyclesubroutine.Subroutine = &AuthorizationModelGenerationSubroutine{}

type AuthorizationModelGenerationSubroutine struct {
	mgr mcmanager.Manager
}

var modelTpl = template.Must(template.New("model").Parse(`module {{ .Name }}

{{ if eq .Scope "Cluster" }}
extend type core_platform-mesh_io_account
	relations
		define create_{{ .Group }}_{{ .Name }}: owner
		define list_{{ .Group }}_{{ .Name }}: member
		define watch_{{ .Group }}_{{ .Name }}: member
{{ end }}

{{ if eq .Scope "Namespaced" }}
extend type core_namespace
	relations
		define create_{{ .Group }}_{{ .Name }}: owner
		define list_{{ .Group }}_{{ .Name }}: member
		define watch_{{ .Group }}_{{ .Name }}: member
{{ end }}

type {{ .Group }}_{{ .Singular }}
	relations
		define parent: [{{ if eq .Scope "Namespaced" }}core_namespace{{ else }}core_platform-mesh_io_account{{ end }}]
		define member: [role#assignee] or owner or member from parent
		define owner: [role#assignee] or owner from parent
		
		define get: member
		define update: member
		define delete: member
		define patch: member
		define watch: member

		define manage_iam_roles: owner
		define get_iam_roles: member
		define get_iam_users: member

`))

type modelInput struct {
	Name     string
	Group    string
	Singular string
	Scope    string
}

// Finalize implements lifecycle.Subroutine.
func (a *AuthorizationModelGenerationSubroutine) Finalize(ctx context.Context, instance lifecyclecontrollerruntime.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)

	bindingToDelete := instance.(*kcpv1alpha2.APIBinding)

	cluster, err := a.mgr.ClusterFromContext(ctx)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to get cluster from context: %w", err), true, false)
	}

	var bindings kcpv1alpha2.APIBindingList
	err = cluster.GetClient().List(ctx, &bindings)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	var toDeleteAccountInfo accountv1alpha1.AccountInfo
	err = cluster.GetClient().Get(ctx, types.NamespacedName{Name: "account"}, &toDeleteAccountInfo)
	if err != nil {
		log.Error().Err(err).Msg("unable to get account info for binding deletion")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	bindingCount := 0
	for _, binding := range bindings.Items {
		if binding.Spec.Reference.Export.Name != bindingToDelete.Spec.Reference.Export.Name || binding.Spec.Reference.Export.Path != bindingToDelete.Spec.Reference.Export.Path {
			continue
		}

		bindingCluster, err := a.mgr.GetCluster(ctx, string(logicalcluster.From(&binding)))
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		var accountInfo accountv1alpha1.AccountInfo
		err = bindingCluster.GetClient().Get(ctx, types.NamespacedName{Name: "account"}, &accountInfo)
		if kerrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			// If the accountinfo does not exist, we can skip the model generation.
			return ctrl.Result{}, nil
		}
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		// org of the binding to be counted
		bindingOrg := accountInfo.Spec.Organization.GeneratedClusterId
		if bindingOrg != toDeleteAccountInfo.Spec.Organization.GeneratedClusterId {
			// If the binding is not for the same organization, we can skip it.
			continue
		}

		bindingCount++
	}

	if bindingCount > 1 {
		// If there are still other bindings for the same APIExport, we can skip the model deletion.
		return ctrl.Result{}, nil
	}

	err = cluster.GetClient().Delete(ctx, &securityv1alpha1.AuthorizationModel{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s", bindingToDelete.Spec.Reference.Export.Name, bindingToDelete.Spec.Reference.Export.Path),
		},
	})
	if err != nil {
		if kerrors.IsNotFound(err) {
			// If the model does not exist, we can skip the deletion.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	return ctrl.Result{}, nil

}

// Finalizers implements lifecycle.Subroutine.
func (a *AuthorizationModelGenerationSubroutine) Finalizers(_ lifecyclecontrollerruntime.RuntimeObject) []string {
	return []string{}
}

// GetName implements lifecycle.Subroutine.
func (a *AuthorizationModelGenerationSubroutine) GetName() string {
	return "AuthorizationModelGeneration"
}

// Process implements lifecycle.Subroutine.
func (a *AuthorizationModelGenerationSubroutine) Process(ctx context.Context, instance lifecyclecontrollerruntime.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	binding := instance.(*kcpv1alpha2.APIBinding)

	cluster, err := a.mgr.ClusterFromContext(ctx)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	var accountInfo accountv1alpha1.AccountInfo
	err = cluster.GetClient().Get(ctx, types.NamespacedName{Name: "account"}, &accountInfo)
	if kerrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		// If the accountinfo does not exist, we can skip the model generation.
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if binding.Spec.Reference.Export.Name == "core.platform-mesh.io" || strings.HasSuffix(binding.Spec.Reference.Export.Name, "kcp.io") {
		// If the APIExport is the core.platform-mesh.io, we can skip the model generation.
		return ctrl.Result{}, nil
	}

	apiExportCluster, err := a.mgr.GetCluster(ctx, binding.Status.APIExportClusterName)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	var apiExport kcpv1alpha2.APIExport
	err = apiExportCluster.GetClient().Get(ctx, types.NamespacedName{Name: binding.Spec.Reference.Export.Name}, &apiExport)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	for _, latestResourceSchema := range apiExport.Spec.Resources {
		var resourceSchema kcpv1alpha1.APIResourceSchema
		err := apiExportCluster.GetClient().Get(ctx, types.NamespacedName{Name: latestResourceSchema.Name}, &resourceSchema)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		longestRelationName := fmt.Sprintf("create_%s_%s", resourceSchema.Spec.Group, resourceSchema.Spec.Names.Plural)

		group := resourceSchema.Spec.Group

		if len(longestRelationName) > 50 {
			group = resourceSchema.Spec.Group[len(longestRelationName)-50:]
		}

		var buffer bytes.Buffer
		err = modelTpl.Execute(&buffer, modelInput{
			Name:     resourceSchema.Spec.Names.Plural,
			Group:    strings.ReplaceAll(group, ".", "_"),
			Singular: resourceSchema.Spec.Names.Singular,
			Scope:    string(resourceSchema.Spec.Scope),
		})
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		model := securityv1alpha1.AuthorizationModel{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%s", resourceSchema.Spec.Names.Plural, accountInfo.Spec.Organization.Name),
			},
		}

		_, err = controllerutil.CreateOrUpdate(ctx, apiExportCluster.GetClient(), &model, func() error {
			model.Spec = securityv1alpha1.AuthorizationModelSpec{
				Model: buffer.String(),
				StoreRef: securityv1alpha1.WorkspaceStoreRef{
					Name: accountInfo.Spec.Organization.Name,
					Path: accountInfo.Spec.Organization.OriginClusterId,
				},
			}
			return nil
		})
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

	}

	return ctrl.Result{}, nil
}
