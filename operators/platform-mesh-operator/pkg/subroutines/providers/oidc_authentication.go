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
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	// oidcTokenFile is where the provider controllers mount their token, e.g. a projected ServiceAccount token.
	oidcTokenFile = "/var/run/secrets/kcp/token"

	oidcAuthenticationParent = "root"
	oidcRootCAConfigMapName  = "kube-root-ca.crt"
)

func providerOIDC(provider *pmprovidersv1alpha1.Provider) *pmprovidersv1alpha1.ProviderOIDCAuthentication {
	if provider.Spec.Authentication == nil {
		return nil
	}
	return provider.Spec.Authentication.OIDC
}

// oidcUsernamePrefix keeps the identities of one provider apart from all others.
func oidcUsernamePrefix(provider *pmprovidersv1alpha1.Provider) string {
	return providerWorkspaceName(provider) + ":"
}

// providerWorkspaceType returns the type of the provider workspace. With OIDC every
// provider workspace has its own type, because kcp attaches authenticators to types.
func providerWorkspaceType(provider *pmprovidersv1alpha1.Provider) *kcptenancyv1alpha1.WorkspaceTypeReference {
	if providerOIDC(provider) != nil {
		return &kcptenancyv1alpha1.WorkspaceTypeReference{Name: kcptenancyv1alpha1.WorkspaceTypeName(providerWorkspaceName(provider)), Path: oidcAuthenticationParent}
	}
	return &kcptenancyv1alpha1.WorkspaceTypeReference{Name: providerWorkspaceTypeName, Path: providerWorkspaceTypePath}
}

// ensureOIDCWorkspaceType creates the authentication configuration and the workspace
// type that uses it, in the workspace where the provider workspace type lives.
func ensureOIDCWorkspaceType(ctx context.Context, rootClient ctrlruntimeclient.Client, provider *pmprovidersv1alpha1.Provider) error {
	oidc := providerOIDC(provider)
	name := providerWorkspaceName(provider)

	authConfig := &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if _, err := controllerutil.CreateOrUpdate(ctx, rootClient, authConfig, func() error {
		authConfig.Spec.JWT = []kcptenancyv1alpha1.JWTAuthenticator{{
			Issuer: kcptenancyv1alpha1.Issuer{
				URL:                  oidc.IssuerURL,
				DiscoveryURL:         oidc.DiscoveryURL,
				CertificateAuthority: oidc.CertificateAuthority,
				Audiences:            oidc.Audiences,
				AudienceMatchPolicy:  kcptenancyv1alpha1.AudienceMatchPolicyMatchAny,
			},
			ClaimMappings: kcptenancyv1alpha1.ClaimMappings{
				Username: kcptenancyv1alpha1.PrefixedClaimOrExpression{Claim: "sub", Prefix: ptr.To(oidcUsernamePrefix(provider))},
				// kcp requires a groups mapping; a token without the claim simply has no groups.
				Groups: kcptenancyv1alpha1.PrefixedClaimOrExpression{Claim: "groups", Prefix: ptr.To(oidcUsernamePrefix(provider))},
			},
		}}
		return nil
	}); err != nil {
		return gcerrors.Wrap(err, "ensure WorkspaceAuthenticationConfiguration %s", name)
	}

	wsType := &kcptenancyv1alpha1.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if _, err := controllerutil.CreateOrUpdate(ctx, rootClient, wsType, func() error {
		wsType.Spec.Extend.With = []kcptenancyv1alpha1.WorkspaceTypeReference{{Name: providerWorkspaceTypeName, Path: providerWorkspaceTypePath}}
		wsType.Spec.AuthenticationConfigurations = []kcptenancyv1alpha1.AuthenticationConfigurationReference{{Name: name}}
		return nil
	}); err != nil {
		return gcerrors.Wrap(err, "ensure WorkspaceType %s", name)
	}
	return nil
}

func deleteOIDCWorkspaceType(ctx context.Context, rootClient ctrlruntimeclient.Client, provider *pmprovidersv1alpha1.Provider) error {
	name := providerWorkspaceName(provider)
	for _, obj := range []ctrlruntimeclient.Object{
		&kcptenancyv1alpha1.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: name}},
		&kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}},
	} {
		if err := ctrlruntimeclient.IgnoreNotFound(rootClient.Delete(ctx, obj)); err != nil {
			return gcerrors.Wrap(err, "delete %T %s", obj, name)
		}
	}
	return nil
}

// processOIDC gives the OIDC subjects full access to the provider workspace and writes a
// kubeconfig without credentials: the controllers read their token from oidcTokenFile.
func (r *ScopedKubeconfigSubroutine) processOIDC(ctx context.Context, inst *pmprovidersv1alpha1.Provider, ws *kcptenancyv1alpha1.Workspace, providerWsClient, userWsClient ctrlruntimeclient.Client) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	oidc := providerOIDC(inst)

	bindingName := providerClusterRoleBindingName(inst) + "-oidc"
	binding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: bindingName}}
	if _, err := controllerutil.CreateOrUpdate(ctx, providerWsClient, binding, func() error {
		binding.RoleRef = rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "cluster-admin"}
		binding.Subjects = make([]rbacv1.Subject, 0, len(oidc.Subjects))
		for _, subject := range oidc.Subjects {
			binding.Subjects = append(binding.Subjects, rbacv1.Subject{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: oidcUsernamePrefix(inst) + subject})
		}
		return nil
	}); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "create or update ClusterRoleBinding %s", bindingName)
	}

	// kcp publishes the root CA ConfigMap only into existing namespaces.
	if err := providerWsClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: providerSANamespace}}); err != nil && !apierrors.IsAlreadyExists(err) {
		return subroutines.OK(), gcerrors.Wrap(err, "ensure namespace %s in provider workspace", providerSANamespace)
	}

	rootCA := &corev1.ConfigMap{}
	if err := providerWsClient.Get(ctx, types.NamespacedName{Name: oidcRootCAConfigMapName, Namespace: providerSANamespace}, rootCA); ctrlruntimeclient.IgnoreNotFound(err) != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "get ConfigMap %s", oidcRootCAConfigMapName)
	}
	if rootCA.Data["ca.crt"] == "" {
		log.Info().Str("configmap", oidcRootCAConfigMapName).Msg("Root CA not yet published, requeuing")
		return subroutines.StopWithRequeue(waitProviderRequeueDuration, "waiting for the root CA ConfigMap"), nil
	}

	hostURL := inst.Spec.HostOverride
	if hostURL == "" {
		hostURL = r.kcpUrl
	}
	hostURL += fmt.Sprintf("/clusters/%s", ws.Spec.Cluster)

	kubeconfigBytes, err := clientcmd.Write(clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"default-cluster": {Server: hostURL, CertificateAuthorityData: []byte(rootCA.Data["ca.crt"])}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"default-auth": {TokenFile: oidcTokenFile}},
		Contexts:       map[string]*clientcmdapi.Context{"default-context": {Cluster: "default-cluster", AuthInfo: "default-auth"}},
		CurrentContext: "default-context",
	})
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "serialize kubeconfig")
	}

	secretRef := providerKubeconfigSecretTarget(inst)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: secretRef.Namespace}}
	if err := userWsClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return subroutines.OK(), gcerrors.Wrap(err, "ensure namespace %s", secretRef.Namespace)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretRef.Name, Namespace: secretRef.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, userWsClient, secret, func() error {
		secret.Data = map[string][]byte{secretRef.Key: kubeconfigBytes}
		return nil
	}); err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "write kubeconfig Secret %s", secretRef.Name)
	}

	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{Name: secretRef.Name, Namespace: secretRef.Namespace}
	inst.Status.Phase = pmprovidersv1alpha1.ProviderPhaseReady
	log.Info().Str("provider", inst.Name).Str("secret", secretRef.Name).Msg("Ensured OIDC access to provider workspace")
	return subroutines.OK(), nil
}

// providerKubeconfigSecretTarget is the Secret the kubeconfig is written to.
func providerKubeconfigSecretTarget(provider *pmprovidersv1alpha1.Provider) pmprovidersv1alpha1.KubeconfigSecretSpec {
	if provider.Spec.ProviderKubeconfigSecret != nil {
		return *provider.Spec.ProviderKubeconfigSecret
	}
	return pmprovidersv1alpha1.KubeconfigSecretSpec{Name: providerKubeconfigSecretName(provider.Name), Namespace: "default", Key: "kubeconfig"}
}
