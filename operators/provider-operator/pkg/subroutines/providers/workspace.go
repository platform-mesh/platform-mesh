/*
Copyright 2026.

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

package providers

import (
	"context"
	"fmt"

	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/internal/config"
	pmsubs "go.platform-mesh.io/provider-operator/pkg/subroutines"
)

const (
	ProviderWorkspaceSubroutineName      = "ProviderWorkspaceSubroutine"
	ProviderWorkspaceSubroutineFinalizer = "providers.platform-mesh.io/provider-workspace"

	defaultWorkspaceParent    = "root:providers"
	providerWorkspaceTypeName = "provider"
	providerWorkspaceTypePath = "root"

	// providerComponentName attributes bootstrapped objects to this component via
	// corev1alpha1.OwnedByAnnotation.
	providerComponentName = "provider-operator"
)

func providerWorkspaceName(provider *providersv1alpha1.Provider) string {
	return provider.Name + "-" + provider.Annotations["kcp.io/cluster"]
}

func providerWorkspacePath(provider *providersv1alpha1.Provider) string {
	return defaultWorkspaceParent + ":" + providerWorkspaceName(provider)
}

// ProviderWorkspaceSubroutine creates the provider workspace in kcp under
// root:providers:<provider.Name>-<provider.Annotations["kcp.io/cluster"]>.
type ProviderWorkspaceSubroutine struct {
	localClient client.Client
	kcpHelper   pmsubs.KcpHelper
	kcpCfg      config.KCPConfig
	kcpUrl      string

	limiter workqueue.TypedRateLimiter[*kcptenancyv1alpha.Workspace]
}

func NewProviderWorkspaceSubroutine(localClient client.Client, kcpHelper pmsubs.KcpHelper, kcpCfg config.KCPConfig, kcpUrl string) (*ProviderWorkspaceSubroutine, error) {
	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[*kcptenancyv1alpha.Workspace](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %v", err)
	}
	return &ProviderWorkspaceSubroutine{
		localClient: localClient,
		kcpHelper:   kcpHelper,
		kcpCfg:      kcpCfg,
		kcpUrl:      kcpUrl,
		limiter:     rl,
	}, nil
}

func (r *ProviderWorkspaceSubroutine) GetName() string {
	return ProviderWorkspaceSubroutineName
}

func (r *ProviderWorkspaceSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	inst := obj.(*providersv1alpha1.Provider)

	if inst.Status.Phase == "" {
		inst.Status.Phase = providersv1alpha1.ProviderPhasePending
	}

	providerWsName := providerWorkspaceName(inst)
	providerWsPath := providerWorkspacePath(inst)

	log.Debug().Str("parentPath", defaultWorkspaceParent).Str("workspaceName", providerWsName).Msg("Ensuring provider workspace")

	restCfg, err := pmsubs.BuildKubeconfigFromConfig(r.localClient, &r.kcpCfg, r.kcpUrl)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to build kcp admin config")
	}

	scopedKcpClient, err := r.kcpHelper.NewKcpClient(restCfg, defaultWorkspaceParent)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for parent workspace %s", defaultWorkspaceParent)
	}

	// Ensure the provider workspace with "root:providers" workspace type.
	ws := kcptenancyv1alpha.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name: providerWsName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, scopedKcpClient, &ws, func() error {
		if ws.GetResourceVersion() == "" {
			corev1alpha1.SetOwnedBy(&ws, providerComponentName)
		}
		ws.Spec.Type = &kcptenancyv1alpha.WorkspaceTypeReference{
			Name: providerWorkspaceTypeName,
			Path: providerWorkspaceTypePath,
		}
		return nil
	}); err != nil {
		return subroutines.OK(), err
	}

	// Check that the workspace is Ready before proceeding.
	if err := scopedKcpClient.Get(ctx, types.NamespacedName{Name: providerWsName}, &ws); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to get workspace %s", providerWsPath)
	}
	if ws.Status.Phase != "Ready" {
		log.Info().Str("workspace", providerWsPath).Str("phase", string(ws.Status.Phase)).Msg("Workspace not Ready yet, requeuing")
		inst.Status.Phase = providersv1alpha1.ProviderPhaseProvisioningWorkspace
		return subroutines.StopWithRequeue(r.limiter.When(&ws), "Waiting for workspace to become Ready"), nil
	}

	log.Info().Str("workspace", providerWsPath).Msg("Ensured provider workspace")
	r.limiter.Forget(&ws)
	return subroutines.OK(), nil
}

func (r *ProviderWorkspaceSubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	inst := obj.(*providersv1alpha1.Provider)

	providerWsName := providerWorkspaceName(inst)
	providerWsPath := providerWorkspacePath(inst)

	log.Debug().Str("parentPath", defaultWorkspaceParent).Str("workspaceName", providerWsName).Msg("Deleting provider workspace")

	inst.Status.Phase = providersv1alpha1.ProviderPhaseDeleting

	restCfg, err := pmsubs.BuildKubeconfigFromConfig(r.localClient, &r.kcpCfg, r.kcpUrl)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to build kcp admin config")
	}

	scopedKcpClient, err := r.kcpHelper.NewKcpClient(restCfg, defaultWorkspaceParent)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for parent workspace %s", defaultWorkspaceParent)
	}

	ws := kcptenancyv1alpha.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name: providerWsName,
		},
	}
	if err = scopedKcpClient.Delete(ctx, &ws); err != nil {
		if kerrors.IsNotFound(err) {
			log.Info().Str("parentPath", defaultWorkspaceParent).Str("workspaceName", providerWsName).Msg("Deleted provider workspace")
			r.limiter.Forget(&ws)
			return subroutines.OK(), nil
		}
		return subroutines.OK(), gcerrors.Wrap(err, "failed to delete provider workspace %s", providerWsPath)
	}

	return subroutines.StopWithRequeue(r.limiter.When(&ws), "Waiting for provider workspace to be deleted"), nil
}

func (r *ProviderWorkspaceSubroutine) Finalizers(_ client.Object) []string {
	return []string{ProviderWorkspaceSubroutineFinalizer}
}
