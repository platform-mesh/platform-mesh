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
	"time"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	pmsubs "go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"
	"go.platform-mesh.io/subroutines"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	ScopedKubeconfigSubroutineName = "ScopedKubeconfigSubroutine"
	scopedKubeconfigFinalizer      = "providers.platform-mesh.io/scoped-kubeconfig"

	providerSANamespace = "default"

	rootCAConfigMapName      = "kube-root-ca.crt"
	providerTokenKey         = "token"
	tokenExpiresAtAnnotation = "providers.platform-mesh.io/token-expires-at"
	defaultTokenExpiration   = time.Hour
)

func providerKubeconfigSecretName(providerName string) string {
	return providerName + "-provider-kubeconfig"
}

func providerServiceAccountName(provider *pmprovidersv1alpha1.Provider) string {
	return provider.Name + "-provider"
}

func providerServiceAccountTokenSecretName(provider *pmprovidersv1alpha1.Provider) string {
	return provider.Name + "-provider-token"
}

func providerClusterRoleBindingName(provider *pmprovidersv1alpha1.Provider) string {
	return provider.Name + "-provider"
}

// ScopedKubeconfigSubroutine creates the ServiceAccount, RBAC, a short-lived SA
// token, and kubeconfig Secret inside the dedicated provider workspace in
// a single reconciliation step. The kubeconfig Secret is stored in the user
// workspace (where Provider object is created)
// via the cluster client.
type ScopedKubeconfigSubroutine struct {
	localClient ctrlruntimeclient.Client
	kcpHelper   pmsubs.KcpHelper
	kcpCfg      config.KCPConfig
	kcpUrl      string

	getClusterClientFromContext func(context.Context) (ctrlruntimeclient.Client, error)

	tokenExpiration time.Duration
}

// WithTokenExpiration sets the lifetime of the token in the kubeconfig; zero keeps the default.
func (r *ScopedKubeconfigSubroutine) WithTokenExpiration(d time.Duration) *ScopedKubeconfigSubroutine {
	if d > 0 {
		r.tokenExpiration = d
	}
	return r
}

func NewScopedKubeconfigSubroutine(localClient ctrlruntimeclient.Client, kcpHelper pmsubs.KcpHelper, kcpCfg config.KCPConfig, kcpUrl string, getClusterClientFromContext func(context.Context) (ctrlruntimeclient.Client, error)) *ScopedKubeconfigSubroutine {
	return &ScopedKubeconfigSubroutine{
		localClient:                 localClient,
		kcpHelper:                   kcpHelper,
		kcpCfg:                      kcpCfg,
		kcpUrl:                      kcpUrl,
		getClusterClientFromContext: getClusterClientFromContext,
		tokenExpiration:             defaultTokenExpiration,
	}
}

func (r *ScopedKubeconfigSubroutine) GetName() string {
	return ScopedKubeconfigSubroutineName
}

func (r *ScopedKubeconfigSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	inst := obj.(*pmprovidersv1alpha1.Provider)

	saName := providerServiceAccountName(inst)
	tokenSecretName := providerServiceAccountTokenSecretName(inst)
	clusterRoleBindingName := providerClusterRoleBindingName(inst)

	inst.Status.Phase = pmprovidersv1alpha1.ProviderPhaseProvisioningKubeconfig

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
	ws := &kcptenancyv1alpha1.Workspace{}
	if err := providersClient.Get(ctx, types.NamespacedName{Name: wsName}, ws); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info().Str("workspace", wsPath).Msg("Provider workspace not found yet, requeuing")
			inst.Status.Phase = pmprovidersv1alpha1.ProviderPhaseProvisioningWorkspace
			return subroutines.StopWithRequeue(waitProviderRequeueDuration, "Waiting for provider workspace"), nil
		}
		return subroutines.OK(), gcerrors.Wrap(err, "failed to get provider workspace %s", wsName)
	}

	if ws.Status.Phase != "Ready" {
		log.Info().Str("workspace", wsPath).Str("phase", string(ws.Status.Phase)).Msg("Provider workspace not Ready yet, requeuing")
		inst.Status.Phase = pmprovidersv1alpha1.ProviderPhaseProvisioningWorkspace
		return subroutines.StopWithRequeue(waitProviderRequeueDuration, "Waiting for provider workspace to become Ready"), nil
	}

	// Get a client scoped to the provider workspace itself.
	providerWsClient, err := r.kcpHelper.NewKcpClient(adminKcpRESTConfig, wsPath)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to create kcp client for provider workspace %s", wsPath)
	}

	// Ensure the default namespace exists in the provider workspace.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: providerSANamespace}}
	if err := providerWsClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return subroutines.OK(), gcerrors.Wrap(err, "ensure namespace %s in provider workspace", providerSANamespace)
	}

	// Ensure ServiceAccount.
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: providerSANamespace}}
	if err := providerWsClient.Create(ctx, sa); err != nil && !apierrors.IsAlreadyExists(err) {
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

	// Older versions stored a token that never expires; deleting its Secret revokes it.
	legacyTokenSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: providerSANamespace}}
	if err := ctrlruntimeclient.IgnoreNotFound(providerWsClient.Delete(ctx, legacyTokenSecret)); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "delete legacy token Secret %s", tokenSecretName)
	}

	rootCA := &corev1.ConfigMap{}
	if err := providerWsClient.Get(ctx, types.NamespacedName{Name: rootCAConfigMapName, Namespace: providerSANamespace}, rootCA); ctrlruntimeclient.IgnoreNotFound(err) != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "get ConfigMap %s", rootCAConfigMapName)
	}
	caData := []byte(rootCA.Data["ca.crt"])
	if len(caData) == 0 {
		log.Info().Str("configmap", rootCAConfigMapName).Msg("Root CA not yet published, requeuing")
		return subroutines.StopWithRequeue(waitProviderRequeueDuration, "waiting for the root CA ConfigMap"), nil
	}

	// Compute host URL: prefer HostOverride, else use workspace URL.
	hostURL := inst.Spec.HostOverride
	if hostURL == "" {
		hostURL = r.kcpUrl
	}
	hostURL += fmt.Sprintf("/clusters/%s", ws.Spec.Cluster)

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
	if err := userWsClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return subroutines.OK(), gcerrors.Wrap(err, "ensure namespace %s in provider workspace", providerSANamespace)
	}

	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      kubeconfigSecretName,
		Namespace: kubeconfigSecretNamespace,
	}

	kubeconfigSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: kubeconfigSecretName, Namespace: kubeconfigSecretNamespace}}
	if err := userWsClient.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(kubeconfigSecret), kubeconfigSecret); ctrlruntimeclient.IgnoreNotFound(err) != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "get kubeconfig Secret %s", kubeconfigSecretName)
	}
	now := time.Now()
	if refreshAt, ok := tokenRefreshTime(kubeconfigSecret, kubeconfigSecretKey, hostURL, r.tokenExpiration); ok && now.Before(refreshAt) {
		inst.Status.Phase = pmprovidersv1alpha1.ProviderPhaseReady
		return subroutines.OKWithRequeue(refreshAt.Sub(now)), nil
	}

	expirationSeconds := int64(r.tokenExpiration.Seconds())
	tokenRequest := &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expirationSeconds}}
	if err := providerWsClient.SubResource("token").Create(ctx, sa, tokenRequest); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "request token for ServiceAccount %s", saName)
	}
	expiresAt := tokenRequest.Status.ExpirationTimestamp.Time

	tokenKey := providerTokenSecretKey(kubeconfigSecretKey)
	kubeconfigBytes, err := clientcmd.Write(buildProviderScopedKubeconfigForToken(hostURL, tokenRequest.Status.Token, tokenKey, caData))
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "serialize kubeconfig")
	}

	// Write kubeconfig Secret into user's ws.
	if _, err := controllerutil.CreateOrUpdate(ctx, userWsClient, kubeconfigSecret, func() error {
		kubeconfigSecret.Data = map[string][]byte{
			kubeconfigSecretKey: kubeconfigBytes,
			tokenKey:            []byte(tokenRequest.Status.Token),
		}
		if kubeconfigSecret.Annotations == nil {
			kubeconfigSecret.Annotations = map[string]string{}
		}
		kubeconfigSecret.Annotations[tokenExpiresAtAnnotation] = expiresAt.UTC().Format(time.RFC3339)
		return nil
	}); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "write kubeconfig Secret %s", kubeconfigSecretName)
	}

	inst.Status.Phase = pmprovidersv1alpha1.ProviderPhaseReady

	log.Info().Str("provider", inst.Name).Str("secret", kubeconfigSecretName).Time("expiresAt", expiresAt).Msg("Ensured scoped kubeconfig in provider workspace")
	return subroutines.OKWithRequeue(expiresAt.Sub(now) * 2 / 3), nil
}

// tokenRefreshTime returns when the token in the kubeconfig Secret must be renewed.
// ok is false if the Secret has no token for hostURL yet.
func tokenRefreshTime(secret *corev1.Secret, kubeconfigKey, hostURL string, lifetime time.Duration) (refreshAt time.Time, ok bool) {
	expiresAt, err := time.Parse(time.RFC3339, secret.Annotations[tokenExpiresAtAnnotation])
	if err != nil || len(secret.Data[providerTokenSecretKey(kubeconfigKey)]) == 0 {
		return time.Time{}, false
	}
	kubeconfig, err := clientcmd.Load(secret.Data[kubeconfigKey])
	if err != nil {
		return time.Time{}, false
	}
	kubeContext, found := kubeconfig.Contexts[kubeconfig.CurrentContext]
	if !found {
		return time.Time{}, false
	}
	if cluster, found := kubeconfig.Clusters[kubeContext.Cluster]; !found || cluster.Server != hostURL {
		return time.Time{}, false
	}
	return expiresAt.Add(-lifetime / 3), true
}

// providerTokenSecretKey is the Secret key of the plain token, next to the kubeconfig key.
func providerTokenSecretKey(kubeconfigKey string) string {
	if kubeconfigKey == providerTokenKey {
		return providerTokenKey + "-file"
	}
	return providerTokenKey
}

func (r *ScopedKubeconfigSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	inst := obj.(*pmprovidersv1alpha1.Provider)
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	inst.Status.Phase = pmprovidersv1alpha1.ProviderPhaseDeleting

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

	for _, res := range []ctrlruntimeclient.Object{
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleBindingName}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: providerSANamespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: providerSANamespace}},
	} {
		if err := ctrlruntimeclient.IgnoreNotFound(providerWsClient.Delete(ctx, res)); err != nil {
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
		if err := ctrlruntimeclient.IgnoreNotFound(userWsClient.Delete(ctx, kubeconfigSecret)); err != nil {
			return subroutines.OK(), gcerrors.Wrap(err, "delete kubeconfig Secret %s", kubeconfigSecret.Name)
		}
	}

	log.Info().Str("provider", inst.Name).Msg("Deleted scoped kubeconfig resources from provider workspace")
	return subroutines.OK(), nil
}

func (r *ScopedKubeconfigSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{scopedKubeconfigFinalizer}
}

// buildProviderScopedKubeconfigForToken sets both token and tokenFile: client-go re-reads
// tokenFile (relative to the kubeconfig) and uses token when the file is not there.
func buildProviderScopedKubeconfigForToken(hostURL, token, tokenFile string, caData []byte) clientcmdapi.Config {
	return clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"default-cluster": {Server: hostURL, CertificateAuthorityData: caData}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"default-auth": {Token: token, TokenFile: tokenFile}},
		Contexts:       map[string]*clientcmdapi.Context{"default-context": {Cluster: "default-cluster", AuthInfo: "default-auth"}},
		CurrentContext: "default-context",
	}
}
