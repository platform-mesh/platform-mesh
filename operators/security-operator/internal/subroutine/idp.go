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

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const (
	kubectlClientName = "kubectl"
	secretNamespace   = "default"
)

func NewIDPSubroutine(mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, cfg config.Config) (*IDPSubroutine, error) {
	limiter, err := ratelimiter.NewStaticThenExponentialRateLimiter[*pmcorev1alpha1.IdentityProviderConfiguration](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	var seedConfig *config.SeedUpstreamConfig
	if cfg.IDP.SeedConfigFile != "" {
		seedConfig, err = config.LoadSeedUpstreamConfig(cfg.IDP.SeedConfigFile)
		if err != nil {
			return nil, fmt.Errorf("loading IDP seed config: %w", err)
		}
	}

	return &IDPSubroutine{
		mgr:                       mgr,
		kcpClientGetter:           kcpClientGetter,
		additionalRedirectURLs:    cfg.IDP.AdditionalRedirectURLs,
		kubectlClientRedirectURLs: cfg.IDP.KubectlClientRedirectURLs,
		baseDomain:                cfg.BaseDomain,
		registrationAllowed:       cfg.IDP.RegistrationAllowed,
		seedConfig:                seedConfig,
		limiter:                   limiter,
	}, nil
}

var (
	_ subroutines.Initializer = &IDPSubroutine{}
	_ subroutines.Processor   = &IDPSubroutine{}
	_ subroutines.Terminator  = &IDPSubroutine{}
)

type IDPSubroutine struct {
	mgr                       mcmanager.Manager
	kcpClientGetter           iclient.KCPClientGetter
	additionalRedirectURLs    []string
	kubectlClientRedirectURLs []string
	baseDomain                string
	registrationAllowed       bool
	seedConfig                *config.SeedUpstreamConfig
	limiter                   workqueue.TypedRateLimiter[*pmcorev1alpha1.IdentityProviderConfiguration]
}

func (i *IDPSubroutine) GetName() string { return "IDPSubroutine" }

// Initialize implements subroutines.Initializer.
func (i *IDPSubroutine) Initialize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return i.reconcile(ctx, obj)
}

// Process implements subroutines.Processor.
func (i *IDPSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return i.reconcile(ctx, obj)
}

func (i *IDPSubroutine) reconcile(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
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
	var account pmcorev1alpha1.Account
	err = orgsClient.Get(ctx, types.NamespacedName{Name: workspaceName}, &account)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get account resource %w", err)
	}

	if account.Spec.Type != pmcorev1alpha1.AccountTypeOrg {
		log.Debug().Str("workspace", workspaceName).Msg("account is not of type organization, skipping idp creation")
		return subroutines.OK(), nil
	}

	clients := []pmcorev1alpha1.IdentityProviderClientConfig{
		{
			ClientName:             workspaceName,
			ClientType:             pmcorev1alpha1.IdentityProviderClientTypeConfidential,
			RedirectURIs:           append(i.additionalRedirectURLs, fmt.Sprintf("https://%s.%s/*", workspaceName, i.baseDomain)),
			PostLogoutRedirectURIs: []string{fmt.Sprintf("https://%s.%s/logout*", workspaceName, i.baseDomain)},
			SecretRef: corev1.SecretReference{
				Name:      fmt.Sprintf("portal-client-secret-%s-%s", workspaceName, workspaceName),
				Namespace: secretNamespace,
			},
		},
		{
			ClientName:   kubectlClientName,
			ClientType:   pmcorev1alpha1.IdentityProviderClientTypePublic,
			RedirectURIs: i.kubectlClientRedirectURLs,
			SecretRef: corev1.SecretReference{
				Name:      fmt.Sprintf("portal-client-secret-%s-%s", workspaceName, kubectlClientName),
				Namespace: secretNamespace,
			},
		},
	}

	if i.seedConfig != nil && i.seedConfig.AllowsSeedingForRealm(workspaceName) {
		for _, seedProvider := range i.seedConfig.SeedUpstreamIdentityProviders.Providers {
			if err := i.createOrUpdateSeedUpstreamClientSecret(ctx, orgsClient, workspaceName, seedProvider); err != nil {
				return subroutines.OK(), fmt.Errorf("failed to create or update seed upstream client secret for %q: %w", seedProvider.Alias, err)
			}
		}
	}

	idp := &pmcorev1alpha1.IdentityProviderConfiguration{ObjectMeta: metav1.ObjectMeta{Name: workspaceName}}
	_, err = controllerutil.CreateOrPatch(ctx, orgsClient, idp, func() error {
		idp.Spec.RegistrationAllowed = i.registrationAllowed

		idp.Spec.Clients = mergeManagedClients(idp.Spec.Clients, clients)

		idp.Spec.UpstreamIdentityProviders = i.mergeSeedUpstreamProviders(workspaceName, idp.Spec.UpstreamIdentityProviders)

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

// Terminate deletes the IdentityProviderConfiguration created during org init.
func (i *IDPSubroutine) Terminate(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	lc := obj.(*kcpcorev1alpha1.LogicalCluster)

	workspaceName := getWorkspaceName(lc)
	if workspaceName == "" {
		return subroutines.OK(), fmt.Errorf("failed to get workspace name")
	}

	orgsClient, err := i.kcpClientGetter.NewClientForLogicalCluster(ctx, config.OrgsClusterPath)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting orgs client: %w", err)
	}

	pending, err := deleteOrgResource(ctx, orgsClient, &pmcorev1alpha1.IdentityProviderConfiguration{}, workspaceName)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("deleting IdentityProviderConfiguration %s: %w", workspaceName, err)
	}
	if pending {
		return subroutines.StopWithRequeue(orgResourceDeleteRequeue, "waiting for IdentityProviderConfiguration to be deleted"), nil
	}

	return subroutines.OK(), nil
}

func (i *IDPSubroutine) patchAccountInfo(ctx context.Context, cl ctrlruntimeclient.Client, workspaceName string, idp *pmcorev1alpha1.IdentityProviderConfiguration) error {
	accountInfo := pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: "account"}, &accountInfo); err != nil {
		return fmt.Errorf("failed to get accountInfo: %w", err)
	}

	desiredIssuerURL := fmt.Sprintf("https://%s/keycloak/realms/%s", i.baseDomain, workspaceName)
	desiredClients := make(map[string]pmcorev1alpha1.ClientInfo)
	for clientName, managedClient := range idp.Status.ManagedClients {
		desiredClients[clientName] = pmcorev1alpha1.ClientInfo{
			ClientID: managedClient.ClientID,
		}
	}

	desiredOIDC := &pmcorev1alpha1.OIDCInfo{
		IssuerURL: desiredIssuerURL,
		Clients:   desiredClients,
	}

	if equality.Semantic.DeepEqual(accountInfo.Spec.OIDC, desiredOIDC) {
		log.Debug().Str("workspace", workspaceName).Msg("accountInfo OIDC configuration already up to date, skip patching")
		return nil
	}

	original := accountInfo.DeepCopy()
	accountInfo.Spec.OIDC = desiredOIDC

	if err := cl.Patch(ctx, &accountInfo, ctrlruntimeclient.MergeFrom(original)); err != nil {
		return fmt.Errorf("failed to patch accountInfo: %w", err)
	}
	return nil
}

// mergeManagedClients merges the clients managed by this subroutine into the
// existing spec clients: entries are matched by client name (updating the
// managed fields in place, otherwise appended). Clients not managed by this
// subroutine are left untouched.
func mergeManagedClients(
	existing []pmcorev1alpha1.IdentityProviderClientConfig,
	managed []pmcorev1alpha1.IdentityProviderClientConfig,
) []pmcorev1alpha1.IdentityProviderClientConfig {
	for _, desired := range managed {
		idx := slices.IndexFunc(existing, func(c pmcorev1alpha1.IdentityProviderClientConfig) bool {
			return c.ClientName == desired.ClientName
		})
		if idx != -1 {
			existing[idx].ClientType = desired.ClientType
			existing[idx].RedirectURIs = desired.RedirectURIs
			existing[idx].PostLogoutRedirectURIs = desired.PostLogoutRedirectURIs
			existing[idx].SecretRef = desired.SecretRef
			continue
		}
		existing = append(existing, desired)
	}

	return existing
}

// mergeSeedUpstreamProviders returns the upstream identity providers for the
// workspace: the seed configuration's providers merged into whatever is already
// present in the spec. Seeded providers are matched by alias (replacing an
// existing entry in place, otherwise appended); providers not part of the seed
// config are left untouched. Returns existing unchanged when seeding is disabled
// or the workspace is not covered by the seed config.
func (i *IDPSubroutine) mergeSeedUpstreamProviders(
	workspaceName string,
	existing []pmcorev1alpha1.UpstreamIdentityProvider,
) []pmcorev1alpha1.UpstreamIdentityProvider {
	if i.seedConfig == nil || !i.seedConfig.AllowsSeedingForRealm(workspaceName) {
		return existing
	}

	for _, seedProvider := range i.seedConfig.SeedUpstreamIdentityProviders.Providers {
		desired := seedProvider.ToUpstreamIdentityProvider(workspaceName)
		idx := slices.IndexFunc(existing, func(p pmcorev1alpha1.UpstreamIdentityProvider) bool {
			return p.Alias == desired.Alias
		})
		if idx != -1 {
			existing[idx] = desired
			continue
		}
		existing = append(existing, desired)
	}

	return existing
}

func (i *IDPSubroutine) createOrUpdateSeedUpstreamClientSecret(
	ctx context.Context,
	orgsClient ctrlruntimeclient.Client,
	realm string,
	seedProvider config.SeedUpstreamIdentityProvider,
) error {
	alias := strings.TrimSpace(seedProvider.Alias)
	if alias == "" {
		return fmt.Errorf("upstream provider alias is required")
	}
	if seedProvider.ClientSecret == "" {
		return fmt.Errorf("upstream provider %q clientSecret is required", alias)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.UpstreamIdentityProviderClientSecretName(realm, alias),
			Namespace: secretNamespace,
			Labels: map[string]string{
				"core.platform-mesh.io/idp-name":           realm,
				"core.platform-mesh.io/upstream-idp-alias": alias,
			},
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, orgsClient, secret, func() error {
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		secret.Data["client_secret"] = []byte(seedProvider.ClientSecret)
		secret.Type = corev1.SecretTypeOpaque
		return nil
	})
	return err
}

func getWorkspaceName(lc *kcpcorev1alpha1.LogicalCluster) string {
	if path, ok := lc.Annotations["kcp.io/path"]; ok {
		pathElements := strings.Split(path, ":")
		return pathElements[len(pathElements)-1]
	}
	return ""
}
