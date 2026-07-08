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
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/internal/config"
	pmsubs "go.platform-mesh.io/provider-operator/pkg/subroutines"
)

const (
	ScopedKubeconfigSubroutineName = "ScopedKubeconfigSubroutine"
	scopedKubeconfigFinalizer      = "providers.platform-mesh.io/scoped-kubeconfig"

	providerSANamespace = "default"
)

func providerKubeconfigSecretName(providerName string) string {
	return providerName + "-provider-kubeconfig"
}

func providerServiceAccountName(provider *providersv1alpha1.Provider) string {
	return provider.Name + "-provider"
}

func providerServiceAccountTokenSecretName(provider *providersv1alpha1.Provider) string {
	return provider.Name + "-provider-token"
}

func providerClusterRoleBindingName(provider *providersv1alpha1.Provider) string {
	return provider.Name + "-provider"
}

// ScopedKubeconfigSubroutine creates the ServiceAccount, RBAC, static SA token
// Secret, and kubeconfig Secret inside the dedicated provider workspace in
// a single reconciliation step. The kubeconfig Secret is stored in the user
// workspace (where Provider object is created)
// via the cluster client.
type ScopedKubeconfigSubroutine struct {
	localClient client.Client
	kcpHelper   pmsubs.KcpHelper
	kcpCfg      config.KCPConfig
	kcpUrl      string

	getClusterClientFromContext func(context.Context) (client.Client, error)
}

func NewScopedKubeconfigSubroutine(localClient client.Client, kcpHelper pmsubs.KcpHelper, kcpCfg config.KCPConfig, kcpUrl string, getClusterClientFromContext func(context.Context) (client.Client, error)) *ScopedKubeconfigSubroutine {
	return &ScopedKubeconfigSubroutine{
		localClient:                 localClient,
		kcpHelper:                   kcpHelper,
		kcpCfg:                      kcpCfg,
		kcpUrl:                      kcpUrl,
		getClusterClientFromContext: getClusterClientFromContext,
	}
}

func (r *ScopedKubeconfigSubroutine) GetName() string {
	return ScopedKubeconfigSubroutineName
}

func (r *ScopedKubeconfigSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	inst := obj.(*providersv1alpha1.Provider)

	saName := providerServiceAccountName(inst)
	tokenSecretName := providerServiceAccountTokenSecretName(inst)
	clusterRoleBindingName := providerClusterRoleBindingName(inst)

	inst.Status.Phase = providersv1alpha1.ProviderPhaseProvisioningKubeconfig

	userWsClient, err := r.getClusterClientFromContext(ctx)
	if err != nil {
		return subroutines.OK(), err
	}

	wsName := providerWorkspaceName(inst)
	wsPath := providerWorkspacePath(inst)

	// Build admin rest config.
	adminKcpRESTConfig, err := pmsubs.BuildKubeconfigFromConfig(r.localClient, &r.kcpCfg, r.kcpUrl)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to build kcp admin config")
	}

	// Get a client scoped to the root:providers workspace to fetch the Workspace object.
	providersClient, err := r.kcpHelper.NewKcpClient(adminKcpRESTConfig, "root:providers")
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for root:providers")
	}

	// Fetch the provider workspace to get its status.
	ws := &kcptenancyv1alpha.Workspace{}
	if err := providersClient.Get(ctx, types.NamespacedName{Name: wsName}, ws); err != nil {
		if kerrors.IsNotFound(err) {
			log.Info().Str("workspace", wsPath).Msg("Provider workspace not found yet, requeuing")
			inst.Status.Phase = providersv1alpha1.ProviderPhaseProvisioningWorkspace
			return subroutines.StopWithRequeue(waitProviderRequeueDuration, "Waiting for provider workspace"), nil
		}
		return subroutines.OK(), gcerrors.Wrap(err, "failed to get provider workspace %s", wsName)
	}

	if ws.Status.Phase != "Ready" {
		log.Info().Str("workspace", wsPath).Str("phase", string(ws.Status.Phase)).Msg("Provider workspace not Ready yet, requeuing")
		inst.Status.Phase = providersv1alpha1.ProviderPhaseProvisioningWorkspace
		return subroutines.StopWithRequeue(waitProviderRequeueDuration, "Waiting for provider workspace to become Ready"), nil
	}

	// Get a client scoped to the provider workspace itself.
	providerWsClient, err := r.kcpHelper.NewKcpClient(adminKcpRESTConfig, wsPath)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for provider workspace %s", wsPath)
	}

	// Ensure the default namespace exists in the provider workspace.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: providerSANamespace}}
	if err := providerWsClient.Create(ctx, ns); err != nil && !kerrors.IsAlreadyExists(err) {
		return subroutines.OK(), gcerrors.Wrap(err, "ensure namespace %s in provider workspace", providerSANamespace)
	}

	// Ensure ServiceAccount.
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: providerSANamespace}}
	if err := providerWsClient.Create(ctx, sa); err != nil && !kerrors.IsAlreadyExists(err) {
		return subroutines.OK(), gcerrors.Wrap(err, "create ServiceAccount %s", saName)
	}

	// Ensure cluster-admin ClusterRoleBinding for this service account.
	clusterAdminClusterRole := "cluster-admin"
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleBindingName}}
	if _, err := controllerutil.CreateOrUpdate(ctx, providerWsClient, clusterRoleBinding, func() error {
		clusterRoleBinding.RoleRef = rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: clusterAdminClusterRole}
		clusterRoleBinding.Subjects = []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Namespace: providerSANamespace, Name: saName}}
		return nil
	}); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "create or update ClusterRoleBinding %s", clusterRoleBindingName)
	}

	// Ensure a static long-lived SA token Secret.
	tokenSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: providerSANamespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, providerWsClient, tokenSecret, func() error {
		tokenSecret.Type = corev1.SecretTypeServiceAccountToken
		if tokenSecret.Annotations == nil {
			tokenSecret.Annotations = map[string]string{}
		}
		tokenSecret.Annotations[corev1.ServiceAccountNameKey] = saName
		return nil
	}); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "create or update token Secret %s", tokenSecretName)
	}
	if len(tokenSecret.Data["token"]) == 0 || len(tokenSecret.Data["ca.crt"]) == 0 {
		log.Info().Str("secret", tokenSecretName).Msg("SA token not yet populated, requeuing")
		return subroutines.StopWithRequeue(waitProviderRequeueDuration, "waiting for SA token to be populated"), nil
	}

	token := string(tokenSecret.Data["token"])
	caData := tokenSecret.Data["ca.crt"]

	// Compute host URL: prefer HostOverride, else use workspace URL.
	hostURL := inst.Spec.HostOverride
	if hostURL == "" {
		hostURL = r.kcpUrl
	}
	hostURL += fmt.Sprintf("/clusters/%s", ws.Spec.Cluster)

	kubeconfigBytes, err := clientcmd.Write(buildProviderScopedKubeconfigForToken(hostURL, token, caData))
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "serialize kubeconfig")
	}

	// Determine kubeconfig Secret target.
	var kubeconfigSecretName, kubeconfigSecretNamespace, kubeconfigSecretKey string
	if inst.Spec.ProviderKubeconfigSecret != nil {
		kubeconfigSecretName = inst.Spec.ProviderKubeconfigSecret.Name
		kubeconfigSecretNamespace = inst.Spec.ProviderKubeconfigSecret.Namespace
		kubeconfigSecretKey = inst.Spec.ProviderKubeconfigSecret.Key
	} else {
		kubeconfigSecretName = providerKubeconfigSecretName(inst.Name)
		kubeconfigSecretNamespace = "default"
		kubeconfigSecretKey = "kubeconfig"
	}

	// Ensure the default namespace exists in the provider workspace.
	ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: kubeconfigSecretNamespace}}
	if err := userWsClient.Create(ctx, ns); err != nil && !kerrors.IsAlreadyExists(err) {
		return subroutines.OK(), gcerrors.Wrap(err, "ensure namespace %s in provider workspace", providerSANamespace)
	}

	// Write kubeconfig Secret into user's ws.
	kubeconfigSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: kubeconfigSecretName, Namespace: kubeconfigSecretNamespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, userWsClient, kubeconfigSecret, func() error {
		kubeconfigSecret.Data = map[string][]byte{kubeconfigSecretKey: kubeconfigBytes}
		return nil
	}); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "write kubeconfig Secret %s", kubeconfigSecretName)
	}

	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      kubeconfigSecretName,
		Namespace: kubeconfigSecretNamespace,
	}

	inst.Status.Phase = providersv1alpha1.ProviderPhaseReady

	log.Info().Str("provider", inst.Name).Str("secret", kubeconfigSecretName).Msg("Ensured scoped kubeconfig in provider workspace")
	return subroutines.OK(), nil
}

func (r *ScopedKubeconfigSubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	inst := obj.(*providersv1alpha1.Provider)
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	inst.Status.Phase = providersv1alpha1.ProviderPhaseDeleting

	wsPath := providerWorkspacePath(inst)

	// Build admin rest config.
	restCfg, err := pmsubs.BuildKubeconfigFromConfig(r.localClient, &r.kcpCfg, r.kcpUrl)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to build kcp admin config for finalize")
	}

	providerWsClient, err := r.kcpHelper.NewKcpClient(restCfg, wsPath)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for provider workspace %s during finalize", wsPath)
	}

	saName := providerServiceAccountName(inst)
	tokenSecretName := providerServiceAccountTokenSecretName(inst)
	clusterRoleBindingName := providerClusterRoleBindingName(inst)

	for _, res := range []client.Object{
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleBindingName}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: providerSANamespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: providerSANamespace}},
	} {
		if err := client.IgnoreNotFound(providerWsClient.Delete(ctx, res)); err != nil {
			return subroutines.OK(), gcerrors.Wrap(err, "delete %T %s", res, res.GetName())
		}
	}

	// Delete kubeconfig Secret in user's ws.
	if inst.Status.ProviderKubeconfigSecretRef != nil {
		userWsClient, err := r.getClusterClientFromContext(ctx)
		if err != nil {
			return subroutines.OK(), err
		}
		kubeconfigSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      inst.Status.ProviderKubeconfigSecretRef.Name,
				Namespace: inst.Status.ProviderKubeconfigSecretRef.Namespace,
			},
		}
		if err := client.IgnoreNotFound(userWsClient.Delete(ctx, kubeconfigSecret)); err != nil {
			return subroutines.OK(), gcerrors.Wrap(err, "delete kubeconfig Secret %s", kubeconfigSecret.Name)
		}
	}

	log.Info().Str("provider", inst.Name).Msg("Deleted scoped kubeconfig resources from provider workspace")
	return subroutines.OK(), nil
}

func (r *ScopedKubeconfigSubroutine) Finalizers(_ client.Object) []string {
	return []string{scopedKubeconfigFinalizer}
}

func buildProviderScopedKubeconfigForToken(hostURL, token string, caData []byte) clientcmdapi.Config {
	return clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"default-cluster": {Server: hostURL, CertificateAuthorityData: caData}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"default-auth": {Token: token}},
		Contexts:       map[string]*clientcmdapi.Context{"default-context": {Cluster: "default-cluster", AuthInfo: "default-auth"}},
		CurrentContext: "default-context",
	}
}
