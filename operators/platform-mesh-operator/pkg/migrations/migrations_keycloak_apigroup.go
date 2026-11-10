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

package migrations

import (
	"context"
	"slices"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	keycloakPlatformMeshIOExport = "keycloak.platform-mesh.io"
	orgsPath                     = "root:orgs"

	// keycloakTerminator is the pm-keycloak-operator terminator; kcp stamps
	// terminators at creation, so existing organizations get it here.
	keycloakTerminator = "root:keycloak"
	// featureDisableKeycloak mirrors subroutines.FeatureDisableKeycloak (import cycle).
	featureDisableKeycloak = "feature-disable-keycloak"

	legacyIDPNameLabel    = "core.platform-mesh.io/idp-name"
	legacyClientNameLabel = "core.platform-mesh.io/client-name"
	idpNameLabel          = "keycloak.platform-mesh.io/idp-name"
	clientNameLabel       = "keycloak.platform-mesh.io/client-name"
)

// keycloakAPIGroupSplit prepares an existing installation for
// keycloak.platform-mesh.io: it binds the keycloak export into every
// organization and account workspace, labels the OIDC client secrets for the
// keycloak export's secrets claim and adds the pm-keycloak-operator terminator
// to existing organizations. The deprecated core.platform-mesh.io objects
// stay and keep being reconciled; their owners move them to the new group
// (see docs/deployment/migration-v26.11/keycloak-api-group.md). The only
// object removed is the operator's own deprecated `welcome` realm, once its
// successor has adopted it.
// Safe to remove once every environment has reconciled past this change.
type keycloakAPIGroupSplit struct{}

func (keycloakAPIGroupSplit) Name() string { return "keycloak-apigroup-split" }

func (m keycloakAPIGroupSplit) Run(ctx context.Context, deps Deps) error {
	if slices.ContainsFunc(deps.Instance.Spec.FeatureToggles, func(ft pmcorev1alpha1.FeatureToggle) bool {
		return ft.Name == featureDisableKeycloak
	}) {
		return nil
	}
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("migration", m.Name())

	systemClient, err := deps.KcpHelper.NewKcpClient(deps.Config, platformMeshSystemPath)
	if err != nil {
		return gcerrors.Wrap(err, "Failed to create kcp client for %s workspace", platformMeshSystemPath)
	}
	if err := relabelClientSecrets(ctx, systemClient, systemClient); err != nil {
		return gcerrors.Wrap(err, "Failed to relabel OIDC client secrets of %s", platformMeshSystemPath)
	}

	orgsClient, err := deps.KcpHelper.NewKcpClient(deps.Config, orgsPath)
	if err != nil {
		return gcerrors.Wrap(err, "Failed to create kcp client for %s workspace", orgsPath)
	}
	var orgs kcptenancyv1alpha1.WorkspaceList
	if err := orgsClient.List(ctx, &orgs); err != nil {
		// root:orgs may not exist yet on a fresh install; nothing to migrate.
		return nil //nolint:nilerr
	}
	// The OIDC client secrets of every organization live in root:orgs.
	if err := relabelClientSecrets(ctx, orgsClient, orgsClient); err != nil {
		return gcerrors.Wrap(err, "Failed to relabel OIDC client secrets of %s", orgsPath)
	}

	for _, org := range orgs.Items {
		if org.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
			continue
		}
		orgPath := orgsPath + ":" + org.Name
		orgClient, err := deps.KcpHelper.NewKcpClient(deps.Config, orgPath)
		if err != nil {
			return gcerrors.Wrap(err, "Failed to create kcp client for %s workspace", orgPath)
		}
		if err := ensureKeycloakTerminator(ctx, orgClient); err != nil {
			return gcerrors.Wrap(err, "Failed to add the %s terminator to %s", keycloakTerminator, orgPath)
		}
		if err := migrateAccountTree(ctx, deps, orgPath, orgClient); err != nil {
			return err
		}
		log.Debug().Str("organization", org.Name).Msg("organization checked")
	}
	return nil
}

// migrateAccountTree binds the keycloak export into the workspace at path and
// every account workspace below it.
func migrateAccountTree(ctx context.Context, deps Deps, path string, client ctrlruntimeclient.Client) error {
	if err := ensureKeycloakBinding(ctx, client); err != nil {
		return gcerrors.Wrap(err, "Failed to bind %s in %s", keycloakPlatformMeshIOExport, path)
	}

	var children kcptenancyv1alpha1.WorkspaceList
	if err := client.List(ctx, &children); err != nil {
		return gcerrors.Wrap(err, "Failed to list workspaces in %s", path)
	}
	for _, child := range children.Items {
		if child.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
			continue
		}
		childPath := path + ":" + child.Name
		childClient, err := deps.KcpHelper.NewKcpClient(deps.Config, childPath)
		if err != nil {
			return gcerrors.Wrap(err, "Failed to create kcp client for %s workspace", childPath)
		}
		if err := migrateAccountTree(ctx, deps, childPath, childClient); err != nil {
			return err
		}
	}
	return nil
}

// ensureKeycloakBinding binds keycloak.platform-mesh.io into an existing
// workspace; kcp applies a type's defaultAPIBindings only at creation.
func ensureKeycloakBinding(ctx context.Context, client ctrlruntimeclient.Client) error {
	bindings := &unstructured.UnstructuredList{}
	bindings.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBindingList"})
	if err := client.List(ctx, bindings); err != nil {
		return gcerrors.Wrap(err, "Failed to list APIBindings")
	}
	if hasBindingToExport(bindings, keycloakPlatformMeshIOExport) {
		return nil
	}
	return applyBinding(ctx, client, keycloakPlatformMeshIOExport, keycloakPlatformMeshIOExport, platformMeshSystemPath, nil)
}

// ensureKeycloakTerminator adds root:keycloak to the LogicalCluster's
// terminators. kcp copies spec.terminators to the status at creation only
// and terminates by the status, so both are patched.
func ensureKeycloakTerminator(ctx context.Context, client ctrlruntimeclient.Client) error {
	var lc kcpcorev1alpha1.LogicalCluster
	if err := client.Get(ctx, types.NamespacedName{Name: kcpcorev1alpha1.LogicalClusterName}, &lc); err != nil {
		return gcerrors.Wrap(err, "Failed to get LogicalCluster")
	}
	if !slices.Contains(lc.Spec.Terminators, keycloakTerminator) {
		patch := ctrlruntimeclient.MergeFrom(lc.DeepCopy())
		lc.Spec.Terminators = append(lc.Spec.Terminators, keycloakTerminator)
		if err := client.Patch(ctx, &lc, patch); err != nil {
			return gcerrors.Wrap(err, "Failed to patch spec.terminators")
		}
	}
	if !slices.Contains(lc.Status.Terminators, keycloakTerminator) {
		patch := ctrlruntimeclient.MergeFrom(lc.DeepCopy())
		lc.Status.Terminators = append(lc.Status.Terminators, keycloakTerminator)
		if err := client.Status().Patch(ctx, &lc, patch); err != nil {
			return gcerrors.Wrap(err, "Failed to patch status.terminators")
		}
	}
	return nil
}

// relabelClientSecrets labels the OIDC client secrets of every deprecated
// IdentityProviderConfiguration in client's workspace for the keycloak export.
// secretClient is where the secrets live (root:orgs).
func relabelClientSecrets(ctx context.Context, client, secretClient ctrlruntimeclient.Client) error {
	//nolint:staticcheck // legacy kind read on purpose
	var legacyList pmcorev1alpha1.IdentityProviderConfigurationList
	if err := client.List(ctx, &legacyList); err != nil {
		if isAPINotServed(err) {
			return nil
		}
		return gcerrors.Wrap(err, "Failed to list legacy IdentityProviderConfigurations")
	}
	for i := range legacyList.Items {
		for _, managed := range legacyList.Items[i].Status.ManagedClients {
			if err := relabelClientSecret(ctx, secretClient, managed.SecretRef); err != nil {
				return gcerrors.Wrap(err, "Failed to relabel client secret %s/%s of IdentityProviderConfiguration %s", managed.SecretRef.Namespace, managed.SecretRef.Name, legacyList.Items[i].Name)
			}
		}
	}
	return nil
}

// relabelClientSecret adds the keycloak.platform-mesh.io labels the keycloak
// export's secrets claim selects; the legacy labels stay for the core claim.
func relabelClientSecret(ctx context.Context, client ctrlruntimeclient.Client, ref corev1.SecretReference) error {
	var secret corev1.Secret
	if err := client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return gcerrors.Wrap(err, "Failed to get client secret")
	}
	labels := secret.GetLabels()
	if labels[idpNameLabel] != "" && labels[clientNameLabel] != "" {
		return nil
	}
	patch := ctrlruntimeclient.MergeFrom(secret.DeepCopy())
	if labels == nil {
		labels = map[string]string{}
	}
	labels[idpNameLabel] = labels[legacyIDPNameLabel]
	labels[clientNameLabel] = labels[legacyClientNameLabel]
	secret.SetLabels(labels)
	return client.Patch(ctx, &secret, patch)
}

// isAPINotServed is true when the workspace does not bind the legacy API at all.
func isAPINotServed(err error) bool {
	return apierrors.IsNotFound(err) || meta.IsNoMatchError(err)
}
