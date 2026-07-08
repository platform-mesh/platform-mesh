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
	"time"

	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/internal/config"
	pmsubs "go.platform-mesh.io/provider-operator/pkg/subroutines"
)

const (
	KubeconfigCopySubroutineName  = "KubeconfigCopySubroutine"
	kubeconfigCopyFinalizer       = "providers.platform-mesh.io/kubeconfig-secret"
	kubeconfigCopyRequeueDuration = 10 * time.Second
)

// KubeconfigCopySubroutine copies the kubeconfig Secret produced by the
// Provider controller from the kcp provider workspace into the runtime
// namespace, and records its name in status.providerKubeconfigSecretRef.
type KubeconfigCopySubroutine struct {
	client      client.Client
	kcpHelper   pmsubs.KcpHelper
	operatorCfg *config.OperatorConfig
	kcpUrl      string
}

func NewKubeconfigCopySubroutine(cl client.Client, kcpHelper pmsubs.KcpHelper, operatorCfg *config.OperatorConfig, kcpUrl string) *KubeconfigCopySubroutine {
	return &KubeconfigCopySubroutine{
		client:      cl,
		kcpHelper:   kcpHelper,
		operatorCfg: operatorCfg,
		kcpUrl:      kcpUrl,
	}
}

func (r *KubeconfigCopySubroutine) GetName() string {
	return KubeconfigCopySubroutineName
}

func (r *KubeconfigCopySubroutine) newRuntimeClusterClient(ctx context.Context, managedProvider *providersv1alpha1.ManagedProvider) (client.Client, error) {
	if managedProvider.Spec.RuntimeKubeconfigSecretName == "" {
		return r.client, nil
	}

	// TODO(gman0): consider adding caching so that we don't have to instantiate a remote client each time.

	// Get the kubeconfig secret.
	var runtimeClusterKubeconfigSecret corev1.Secret
	runtimeClusterKubeconfigSecretNamespacedName := types.NamespacedName{Name: managedProvider.Spec.RuntimeKubeconfigSecretName, Namespace: managedProvider.Namespace}
	err := r.client.Get(ctx, runtimeClusterKubeconfigSecretNamespacedName, &runtimeClusterKubeconfigSecret)
	if err != nil {
		return nil, err
	}

	kubeconfig := runtimeClusterKubeconfigSecret.Data["kubeconfig"]
	if len(kubeconfig) == 0 {
		return nil, fmt.Errorf("kubeconfig secret for runtime cluster %s doesn't have key 'kubeconfig' defined", runtimeClusterKubeconfigSecretNamespacedName)
	}

	// Create REST config.
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create REST config from kubeconfig secret %s: %v", runtimeClusterKubeconfigSecretNamespacedName, err)
	}

	// And finally create the client.
	cl, err := client.New(config, client.Options{
		Scheme: r.client.Scheme(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create client from kubeconfig secret %s: %v", runtimeClusterKubeconfigSecretNamespacedName, err)
	}

	return cl, nil
}

func (r *KubeconfigCopySubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	inst := obj.(*providersv1alpha1.ManagedProvider)

	wsPath := providerRefPath(inst)
	provName := providerRefName(inst)
	managedProviderKubeconfigSecretSpec := providerKubeconfigSecretSpec(inst.Name, inst.Namespace, inst.Spec.ProviderKubeconfigSecret)

	runtimeClusterClient, err := r.newRuntimeClusterClient(ctx, inst)
	if err != nil {
		return subroutines.OK(), err
	}

	restCfg, err := pmsubs.BuildKubeconfigFromConfig(r.client, &r.operatorCfg.KCP, r.kcpUrl)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to build kcp admin config")
	}

	scopedClient, err := r.kcpHelper.NewKcpClient(restCfg, wsPath)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for provider workspace %s", wsPath)
	}

	// Fetch the Provider to find the kubeconfig Secret name set by the Provider controller.
	provider := &providersv1alpha1.Provider{}
	if err := scopedClient.Get(ctx, types.NamespacedName{Name: provName}, provider); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to get Provider %s from workspace %s", provName, wsPath)
	}
	if provider.Status.ProviderKubeconfigSecretRef == nil {
		log.Info().Str("workspace", wsPath).Str("provider", provider.Name).Msg("Provider providerKubeconfigSecretRef not set yet, requeuing")
		inst.Status.Phase = providersv1alpha1.ManagedProviderPhaseCopyingKubeconfig
		return subroutines.StopWithRequeue(kubeconfigCopyRequeueDuration, "waiting for Provider to set providerKubeconfigSecretRef"), nil
	}

	// Validate that Provider's providerKubeconfigSecret must match ManagedProvider's.
	// It's a user error if these two are different. We'll let the user resolve.
	if provider.Spec.ProviderKubeconfigSecret == nil || *provider.Spec.ProviderKubeconfigSecret != managedProviderKubeconfigSecretSpec {
		log.Info().Str("workspace", wsPath).Str("provider", provider.Name).Msg("Provider providerKubeconfigSecretRef not set yet, requeuing")
		inst.Status.Phase = providersv1alpha1.ManagedProviderPhaseCopyingKubeconfigFailed
		return subroutines.StopWithRequeue(kubeconfigCopyRequeueDuration, fmt.Sprintf("providerKubeconfigSecretRef set on Provider %s:%s differs from the one set on ManagedProvider %s/%s", wsPath, provName, inst.Namespace, inst.Name)), nil
	}

	// Fetch the kubeconfig Secret from the provider workspace.
	origSecret := &corev1.Secret{}
	if err := scopedClient.Get(ctx, types.NamespacedName{
		Name:      provider.Status.ProviderKubeconfigSecretRef.Name,
		Namespace: provider.Status.ProviderKubeconfigSecretRef.Namespace,
	}, origSecret); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to get kubeconfig secret %s from workspace %s", provider.Status.ProviderKubeconfigSecretRef.Name, wsPath)
	}

	var kcpKubeconfig []byte
	if origSecret.Data != nil {
		kcpKubeconfig = origSecret.Data[managedProviderKubeconfigSecretSpec.Key]
	}

	if len(kcpKubeconfig) == 0 {
		inst.Status.Phase = providersv1alpha1.ManagedProviderPhaseCopyingKubeconfig
		return subroutines.StopWithRequeue(kubeconfigCopyRequeueDuration, "waiting for Provider to set kubeconfig in secret"), nil
	}

	// Ensure namespace for the Secret we're about to create exists.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedProviderKubeconfigSecretSpec.Namespace}}
	if err := runtimeClusterClient.Create(ctx, ns); err != nil && !kerrors.IsAlreadyExists(err) {
		return subroutines.OK(), gcerrors.Wrap(err, "ensure namespace %s in runtime cluster", managedProviderKubeconfigSecretSpec.Namespace)
	}

	// Ensure copied Secret.
	copySecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedProviderKubeconfigSecretSpec.Name,
			Namespace: managedProviderKubeconfigSecretSpec.Namespace,
		},
	}
	if _, err = controllerruntime.CreateOrUpdate(ctx, runtimeClusterClient, &copySecret, func() error {
		copySecret.Data = map[string][]byte{
			managedProviderKubeconfigSecretSpec.Key: kcpKubeconfig,
		}
		return nil
	}); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to copy kubeconfig Secret %s/%s from workspace %s", copySecret.Namespace, copySecret.Name, wsPath)
	}

	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      copySecret.Name,
		Namespace: copySecret.Namespace,
	}

	log.Info().Str("workspace", wsPath).Str("secret", copySecret.Name).Msg("Copied kubeconfig Secret to runtime cluster")
	return subroutines.OK(), nil
}

func (r *KubeconfigCopySubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	inst := obj.(*providersv1alpha1.ManagedProvider)
	if inst.Status.ProviderKubeconfigSecretRef == nil {
		return subroutines.OK(), nil
	}

	inst.Status.Phase = providersv1alpha1.ManagedProviderPhaseDeleting

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      inst.Status.ProviderKubeconfigSecretRef.Name,
			Namespace: inst.Namespace,
		},
	}
	if err := client.IgnoreNotFound(r.client.Delete(ctx, &secret)); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "delete %T %s", secret, secret.GetName())
	}

	return subroutines.OK(), nil
}

func (r *KubeconfigCopySubroutine) Finalizers(_ client.Object) []string {
	return []string{kubeconfigCopyFinalizer}
}
