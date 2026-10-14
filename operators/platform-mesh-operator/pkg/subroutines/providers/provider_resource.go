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

package providers

import (
	"context"
	"fmt"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	pmsubs "go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ProviderResourceSubroutineName = "ProviderResourceSubroutine"
	providerResourceFinalizer      = "providers.platform-mesh.io/provider-resource"
)

// ProviderResourceSubroutine creates a Provider resource inside the provider
// workspace, triggering the Provider controller to bootstrap SA, RBAC, and the
// kubeconfig Secret on the kcp side.
type ProviderResourceSubroutine struct {
	client    ctrlruntimeclient.Client
	kcpHelper pmsubs.KcpHelper
	cfg       *config.OperatorConfig
	kcpUrl    string

	limiter workqueue.TypedRateLimiter[*pmprovidersv1alpha1.ManagedProvider]
}

func NewProviderResourceSubroutine(cl ctrlruntimeclient.Client, kcpHelper pmsubs.KcpHelper, cfg *config.OperatorConfig, kcpUrl string) (*ProviderResourceSubroutine, error) {
	rl, err := ratelimiter.NewObjectRateLimiter[*pmprovidersv1alpha1.ManagedProvider](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %v", err)
	}
	return &ProviderResourceSubroutine{
		client:    cl,
		kcpHelper: kcpHelper,
		cfg:       cfg,
		kcpUrl:    kcpUrl,
		limiter:   rl,
	}, nil
}

func (r *ProviderResourceSubroutine) GetName() string {
	return ProviderResourceSubroutineName
}

func (r *ProviderResourceSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	inst := obj.(*pmprovidersv1alpha1.ManagedProvider)

	wsPath := providerRefPath(inst)
	providerName := providerRefName(inst)

	restCfg, err := pmsubs.BuildKubeconfigFromConfig(r.client, &r.cfg.KCP, r.kcpUrl)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to build kcp admin config")
	}

	scopedKcpClient, err := r.kcpHelper.NewKcpClient(restCfg, wsPath)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for provider workspace %s", wsPath)
	}

	// Ensure Provider in user's workspace.
	provider := pmprovidersv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name: providerName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, scopedKcpClient, &provider, func() error {
		providerKubeconfigSecret := providerKubeconfigSecretSpec(
			inst.Name,
			inst.Namespace,
			inst.Spec.ProviderKubeconfigSecret,
		)
		provider.Spec.ProviderKubeconfigSecret = &providerKubeconfigSecret
		provider.Spec.HostOverride = inst.Spec.ProviderHostOverride
		return nil
	}); err != nil {
		log.Err(err).Msgf("failed to ensure Provider %q in workspace %q", providerName, wsPath)
		return subroutines.Result{}, err
	}

	log.Info().Str("workspace", wsPath).Str("provider", providerName).Msg("Ensured provider resource")
	return subroutines.OK(), nil
}

func (r *ProviderResourceSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	inst := obj.(*pmprovidersv1alpha1.ManagedProvider)
	if !inst.Spec.CleanupOnDelete {
		return subroutines.OK(), nil
	}

	inst.Status.Phase = pmprovidersv1alpha1.ManagedProviderPhaseDeleting

	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	wsPath := providerRefPath(inst)
	provName := providerRefName(inst)

	restCfg, err := pmsubs.BuildKubeconfigFromConfig(r.client, &r.cfg.KCP, r.kcpUrl)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to build kcp admin config")
	}

	scopedKcpClient, err := r.kcpHelper.NewKcpClient(restCfg, wsPath)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for provider workspace %s", wsPath)
	}

	provider := &pmprovidersv1alpha1.Provider{}
	provider.Name = provName
	if err = scopedKcpClient.Delete(ctx, provider); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info().Str("workspace", wsPath).Str("provider", provName).Msg("Deleted provider")
			r.limiter.Forget(inst)
			return subroutines.OK(), nil
		}
		return subroutines.OK(), gcerrors.Wrap(err, "failed to delete provider %s", provider.Name)
	}

	return subroutines.StopWithRequeue(r.limiter.When(inst), "Waiting for Provider to be deleted"), nil
}

func (r *ProviderResourceSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{providerResourceFinalizer}
}

func providerKubeconfigSecretSpec(name, namespace string, spec *pmprovidersv1alpha1.LocalKubeconfigSecretSpec) pmprovidersv1alpha1.KubeconfigSecretSpec {
	if spec == nil {
		return pmprovidersv1alpha1.KubeconfigSecretSpec{
			Name:      providerKubeconfigSecretName(name),
			Namespace: namespace,
			Key:       "kubeconfig",
		}
	}
	return pmprovidersv1alpha1.KubeconfigSecretSpec{
		Name:      spec.Name,
		Namespace: namespace,
		Key:       spec.Key,
	}
}

// providerRefPath returns the path where ManagedProvider should look for a Provider resource.
func providerRefPath(inst *pmprovidersv1alpha1.ManagedProvider) string {
	if inst.Spec.ProviderReference != nil && inst.Spec.ProviderReference.Path != "" {
		return inst.Spec.ProviderReference.Path
	}
	return "root:providers:system"
}

// providerRefName returns the name of the Provider resource referenced by ManagedProvider.
func providerRefName(inst *pmprovidersv1alpha1.ManagedProvider) string {
	if inst.Spec.ProviderReference != nil && inst.Spec.ProviderReference.Name != "" {
		return inst.Spec.ProviderReference.Name
	}
	return inst.Name
}
