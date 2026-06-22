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

package subroutine

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"text/template"

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

const (
	apiBindingFinalizer = "core.platform-mesh.io/apibinding-finalizer"
)

// toK8sName creates a valid Kubernetes metadata.name from the given parts.
func toK8sName(parts ...string) string {
	name := strings.ToLower(strings.Join(parts, "-"))
	name = strings.ReplaceAll(name, ".", "-")
	return strings.Trim(name, "-")
}

func NewAuthorizationModelGenerationSubroutine(mcMgr mcmanager.Manager, lister iclient.Lister) *AuthorizationModelGenerationSubroutine {
	return &AuthorizationModelGenerationSubroutine{
		mgr:    mcMgr,
		lister: lister,
	}
}

var (
	_ subroutines.Processor = &AuthorizationModelGenerationSubroutine{}
	_ subroutines.Finalizer = &AuthorizationModelGenerationSubroutine{}
)

type AuthorizationModelGenerationSubroutine struct {
	mgr    mcmanager.Manager
	lister iclient.Lister
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

// Finalize implements subroutines.Finalizer.
func (a *AuthorizationModelGenerationSubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	bindingToDelete := obj.(*kcpapisv1alpha2.APIBinding)

	bindingCluster, err := a.mgr.ClusterFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("unable to get cluster from context: %w", err)
	}

	var bindings kcpapisv1alpha2.APIBindingList
	err = a.lister.List(ctx, &bindings)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("listing APIBindings: %w", err)
	}

	var toDeleteAccountInfo corev1alpha1.AccountInfo
	err = bindingCluster.GetClient().Get(ctx, types.NamespacedName{Name: "account"}, &toDeleteAccountInfo)
	if err != nil {
		log.Error().Err(err).Msg("unable to get account info for binding deletion")
		return subroutines.OK(), fmt.Errorf("getting AccountInfo: %w", err)
	}

	bindingCount := 0
	for _, binding := range bindings.Items {
		if binding.Spec.Reference.Export.Name != bindingToDelete.Spec.Reference.Export.Name || binding.Spec.Reference.Export.Path != bindingToDelete.Spec.Reference.Export.Path {
			continue
		}

		bindingWsCluster, err := a.mgr.GetCluster(ctx, multicluster.ClusterName(logicalcluster.From(&binding).String()))
		if err != nil {
			return subroutines.OK(), fmt.Errorf("getting cluster for binding: %w", err)
		}

		var accountInfo corev1alpha1.AccountInfo
		err = bindingWsCluster.GetClient().Get(ctx, types.NamespacedName{Name: "account"}, &accountInfo)
		if kerrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			// If the accountinfo does not exist, skip this binding and continue with others
			continue
		}
		if err != nil {
			return subroutines.OK(), fmt.Errorf("getting AccountInfo for binding: %w", err)
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
		return subroutines.OK(), nil
	}

	apiExportCluster, err := a.mgr.GetCluster(ctx, multicluster.ClusterName(bindingToDelete.Status.APIExportClusterName))
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get cluster %q: %w", bindingToDelete.Status.APIExportClusterName, err)
	}
	apiExportClient := apiExportCluster.GetClient()

	var apiExport kcpapisv1alpha2.APIExport
	err = apiExportClient.Get(ctx, types.NamespacedName{Name: bindingToDelete.Spec.Reference.Export.Name}, &apiExport)
	if err != nil {
		log.Error().Err(err).Msg("failed to get apiexport for binding deletion")
		return subroutines.OK(), fmt.Errorf("getting APIExport: %w", err)
	}

	for _, latestResourceSchema := range apiExport.Spec.Resources {
		var resourceSchema kcpapisv1alpha1.APIResourceSchema
		err := apiExportClient.Get(ctx, types.NamespacedName{Name: latestResourceSchema.Schema}, &resourceSchema)
		if err != nil {
			log.Error().Err(err).Msg("failed to get resource schema for binding deletion")
			return subroutines.OK(), fmt.Errorf("getting APIResourceSchema: %w", err)
		}

		authModelName := toK8sName(resourceSchema.Spec.Group, resourceSchema.Spec.Names.Plural, toDeleteAccountInfo.Spec.Organization.Name)
		err = apiExportClient.Delete(ctx, &corev1alpha1.AuthorizationModel{
			ObjectMeta: metav1.ObjectMeta{
				Name: authModelName,
			},
		})
		if err != nil {
			if kerrors.IsNotFound(err) {
				// If the model does not exist, we can skip the deletion.
				log.Info().Msg(fmt.Sprintf("authorization model %s does not exist", authModelName))
				continue
			}
			log.Error().Err(err).Msg("failed to delete authorization model")
			return subroutines.OK(), fmt.Errorf("deleting AuthorizationModel %s: %w", authModelName, err)
		}
		log.Info().Msg(fmt.Sprintf("authorization model %s has been deleted", authModelName))
	}

	return subroutines.OK(), nil
}

// Finalizers implements subroutines.Finalizer.
func (a *AuthorizationModelGenerationSubroutine) Finalizers(obj client.Object) []string {
	binding := obj.(*kcpapisv1alpha2.APIBinding)
	if strings.Contains(binding.Name, "platform-mesh.io") || strings.Contains(binding.Name, "kcp.io") {
		return []string{}
	}
	return []string{apiBindingFinalizer}
}

// GetName implements subroutines.Subroutine.
func (a *AuthorizationModelGenerationSubroutine) GetName() string {
	return "AuthorizationModelGeneration"
}

// Process implements subroutines.Processor.
func (a *AuthorizationModelGenerationSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	binding := obj.(*kcpapisv1alpha2.APIBinding)

	internalAPIBindings := []string{"core.platform-mesh.io", "system.platform-mesh.io"}

	if slices.Contains(internalAPIBindings, binding.Spec.Reference.Export.Name) || strings.HasSuffix(binding.Spec.Reference.Export.Name, "kcp.io") {
		// If the APIExport is the core.platform-mesh.io, system.platform-mesh.io we can skip the model generation.
		return subroutines.OK(), nil
	}

	cluster, err := a.mgr.ClusterFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting cluster from context: %w", err)
	}

	var accountInfo corev1alpha1.AccountInfo
	err = cluster.GetClient().Get(ctx, types.NamespacedName{Name: "account"}, &accountInfo)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting AccountInfo: %w", err)
	}

	apiExportCluster, err := a.mgr.GetCluster(ctx, multicluster.ClusterName(binding.Status.APIExportClusterName))
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting APIExport cluster: %w", err)
	}

	var apiExport kcpapisv1alpha2.APIExport
	err = apiExportCluster.GetClient().Get(ctx, types.NamespacedName{Name: binding.Spec.Reference.Export.Name}, &apiExport)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting APIExport: %w", err)
	}

	for _, latestResourceSchema := range apiExport.Spec.Resources {
		var resourceSchema kcpapisv1alpha1.APIResourceSchema
		err := apiExportCluster.GetClient().Get(ctx, types.NamespacedName{Name: latestResourceSchema.Schema}, &resourceSchema)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("getting APIResourceSchema: %w", err)
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
			return subroutines.OK(), fmt.Errorf("executing model template: %w", err)
		}

		model := corev1alpha1.AuthorizationModel{
			ObjectMeta: metav1.ObjectMeta{
				Name: toK8sName(resourceSchema.Spec.Group, resourceSchema.Spec.Names.Plural, accountInfo.Spec.Organization.Name),
			},
		}

		_, err = controllerutil.CreateOrUpdate(ctx, apiExportCluster.GetClient(), &model, func() error {
			model.Spec = corev1alpha1.AuthorizationModelSpec{
				Model: buffer.String(),
				StoreRef: corev1alpha1.WorkspaceStoreRef{
					Name:    accountInfo.Spec.Organization.Name,
					Cluster: accountInfo.Spec.Organization.OriginClusterId,
				},
			}
			return nil
		})
		if err != nil {
			return subroutines.OK(), fmt.Errorf("creating or updating AuthorizationModel: %w", err)
		}
	}

	return subroutines.OK(), nil
}
