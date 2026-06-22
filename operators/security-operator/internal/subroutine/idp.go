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
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const (
	kubectlClientName = "kubectl"
	secretNamespace   = "default"
)

func NewIDPSubroutine(mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, cfg config.Config) (*IDPSubroutine, error) {
	limiter, err := ratelimiter.NewStaticThenExponentialRateLimiter[*corev1alpha1.IdentityProviderConfiguration](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}
	return &IDPSubroutine{
		mgr:                       mgr,
		kcpClientGetter:           kcpClientGetter,
		additionalRedirectURLs:    cfg.IDP.AdditionalRedirectURLs,
		kubectlClientRedirectURLs: cfg.IDP.KubectlClientRedirectURLs,
		baseDomain:                cfg.BaseDomain,
		registrationAllowed:       cfg.IDP.RegistrationAllowed,
		limiter:                   limiter,
	}, nil
}

var (
	_ subroutines.Initializer = &IDPSubroutine{}
	_ subroutines.Processor   = &IDPSubroutine{}
)

type IDPSubroutine struct {
	mgr                       mcmanager.Manager
	kcpClientGetter           iclient.KCPClientGetter
	additionalRedirectURLs    []string
	kubectlClientRedirectURLs []string
	baseDomain                string
	registrationAllowed       bool
	limiter                   workqueue.TypedRateLimiter[*corev1alpha1.IdentityProviderConfiguration]
}

func (i *IDPSubroutine) GetName() string { return "IDPSubroutine" }

// Initialize implements subroutines.Initializer.
func (i *IDPSubroutine) Initialize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return i.reconcile(ctx, obj)
}

// Process implements subroutines.Processor.
func (i *IDPSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return i.reconcile(ctx, obj)
}

func (i *IDPSubroutine) reconcile(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	lc := obj.(*kcpcorev1alpha1.LogicalCluster)

	workspaceName := getWorkspaceName(lc)
	if workspaceName == "" {
		return subroutines.OK(), fmt.Errorf("failed to get workspace name")
	}

	cl, err := i.mgr.ClusterFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get cluster from context %w", err)
	}

	orgsClient, err := i.kcpClientGetter.NewClientForLogicalCluster(ctx, "root:orgs")
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting orgs client: %w", err)
	}
	var account corev1alpha1.Account
	err = orgsClient.Get(ctx, types.NamespacedName{Name: workspaceName}, &account)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get account resource %w", err)
	}

	if account.Spec.Type != corev1alpha1.AccountTypeOrg {
		log.Debug().Str("workspace", workspaceName).Msg("account is not of type organization, skipping idp creation")
		return subroutines.OK(), nil
	}

	clients := []corev1alpha1.IdentityProviderClientConfig{
		{
			ClientName:             workspaceName,
			ClientType:             corev1alpha1.IdentityProviderClientTypeConfidential,
			RedirectURIs:           append(i.additionalRedirectURLs, fmt.Sprintf("https://%s.%s/*", workspaceName, i.baseDomain)),
			PostLogoutRedirectURIs: []string{fmt.Sprintf("https://%s.%s/logout*", workspaceName, i.baseDomain)},
			SecretRef: corev1.SecretReference{
				Name:      fmt.Sprintf("portal-client-secret-%s-%s", workspaceName, workspaceName),
				Namespace: secretNamespace,
			},
		},
		{
			ClientName:   kubectlClientName,
			ClientType:   corev1alpha1.IdentityProviderClientTypePublic,
			RedirectURIs: i.kubectlClientRedirectURLs,
			SecretRef: corev1.SecretReference{
				Name:      fmt.Sprintf("portal-client-secret-%s-%s", workspaceName, kubectlClientName),
				Namespace: secretNamespace,
			},
		},
	}

	idp := &corev1alpha1.IdentityProviderConfiguration{ObjectMeta: metav1.ObjectMeta{Name: workspaceName}}
	_, err = controllerutil.CreateOrPatch(ctx, orgsClient, idp, func() error {
		idp.Spec.RegistrationAllowed = i.registrationAllowed

		for _, desired := range clients {
			idp.Spec.Clients = ensureClient(idp.Spec.Clients, desired)
		}
		return nil
	})
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to create idp resource %w", err)
	}

	log.Info().Str("workspace", workspaceName).Msg("idp configuration resource is created")
	if err := orgsClient.Get(ctx, types.NamespacedName{Name: workspaceName}, idp); err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get idp resource %w", err)
	}

	if !meta.IsStatusConditionTrue(idp.GetConditions(), "Ready") {
		log.Debug().Str("workspace", workspaceName).Msg("idp resource is not ready yet, requeuing")
		return subroutines.StopWithRequeue(i.limiter.When(idp), "idp resource is not ready yet"), nil
	}

	if len(idp.Spec.Clients) == 0 || len(idp.Status.ManagedClients) == 0 {
		return subroutines.OK(), fmt.Errorf("IdentityProviderConfiguration %s has no clients in spec or status", workspaceName)
	}

	for _, specClient := range idp.Spec.Clients {
		managedClient, ok := idp.Status.ManagedClients[specClient.ClientName]
		if !ok {
			return subroutines.OK(), fmt.Errorf("managed client %s not found in IdentityProviderConfiguration status", specClient.ClientName)
		}
		if managedClient.ClientID == "" {
			return subroutines.OK(), fmt.Errorf("managed client %s has empty ClientID in IdentityProviderConfiguration status", specClient.ClientName)
		}
	}

	i.limiter.Forget(idp)

	if err := i.patchAccountInfo(ctx, cl.GetClient(), workspaceName, idp); err != nil {
		return subroutines.OK(), fmt.Errorf("unable to update accountInfo: %w", err)
	}

	log.Info().Str("workspace", workspaceName).Msg("idp resource is ready")
	return subroutines.OK(), nil
}

func (i *IDPSubroutine) patchAccountInfo(ctx context.Context, cl client.Client, workspaceName string, idp *corev1alpha1.IdentityProviderConfiguration) error {
	accountInfo := corev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: "account"}, &accountInfo); err != nil {
		return fmt.Errorf("failed to get accountInfo: %w", err)
	}

	desiredIssuerURL := fmt.Sprintf("https://%s/keycloak/realms/%s", i.baseDomain, workspaceName)
	desiredClients := make(map[string]corev1alpha1.ClientInfo)
	for clientName, managedClient := range idp.Status.ManagedClients {
		desiredClients[clientName] = corev1alpha1.ClientInfo{
			ClientID: managedClient.ClientID,
		}
	}

	desiredOIDC := &corev1alpha1.OIDCInfo{
		IssuerURL: desiredIssuerURL,
		Clients:   desiredClients,
	}

	if equality.Semantic.DeepEqual(accountInfo.Spec.OIDC, desiredOIDC) {
		log.Debug().Str("workspace", workspaceName).Msg("accountInfo OIDC configuration already up to date, skip patching")
		return nil
	}

	original := accountInfo.DeepCopy()
	accountInfo.Spec.OIDC = desiredOIDC

	if err := cl.Patch(ctx, &accountInfo, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("failed to patch accountInfo: %w", err)
	}
	return nil
}

// ensureClient updates only clients managed by this subroutine
func ensureClient(existing []corev1alpha1.IdentityProviderClientConfig, desired corev1alpha1.IdentityProviderClientConfig) []corev1alpha1.IdentityProviderClientConfig {
	idx := slices.IndexFunc(existing, func(c corev1alpha1.IdentityProviderClientConfig) bool {
		return c.ClientName == desired.ClientName
	})

	if idx != -1 {
		existing[idx].ClientType = desired.ClientType
		existing[idx].RedirectURIs = desired.RedirectURIs
		existing[idx].PostLogoutRedirectURIs = desired.PostLogoutRedirectURIs
		existing[idx].SecretRef = desired.SecretRef
		return existing
	}

	return append(existing, desired)
}

func getWorkspaceName(lc *kcpcorev1alpha1.LogicalCluster) string {
	if path, ok := lc.Annotations["kcp.io/path"]; ok {
		pathElements := strings.Split(path, ":")
		return pathElements[len(pathElements)-1]
	}
	return ""
}
