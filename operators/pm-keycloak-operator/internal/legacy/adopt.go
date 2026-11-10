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

package legacy

import (
	"context"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	iclient "go.platform-mesh.io/pm-keycloak-operator/internal/client"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/config"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/logicalcluster/v3"
)

// IdentityProviderConfigurationAdopter hands the realm of a deprecated
// IdentityProviderConfiguration over to its keycloak.platform-mesh.io
// successor of the same name in the same logical cluster: the legacy object
// is marked migrated and its managed clients are returned, so the successor
// continues with the existing Keycloak clients instead of registering new ones.
// Returns nil when there is no legacy object.
// getter must reach the legacy kind through the system provider.
func IdentityProviderConfigurationAdopter(getter iclient.KCPClientGetter) func(ctx context.Context, engaged multicluster.ClusterName, name string) (map[string]pmkeycloakv1alpha1.ManagedClient, error) {
	return func(ctx context.Context, engaged multicluster.ClusterName, name string) (map[string]pmkeycloakv1alpha1.ManagedClient, error) {
		cl, err := getter.NewClientForLogicalCluster(ctx, string(config.ProviderClusterName(config.SystemProviderName, engaged)))
		if err != nil {
			return nil, fmt.Errorf("getting system client for cluster %q: %w", engaged, err)
		}
		//nolint:staticcheck // legacy kind served on purpose
		var legacy pmcorev1alpha1.IdentityProviderConfiguration
		if err := cl.Get(ctx, types.NamespacedName{Name: name}, &legacy); err != nil {
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("getting legacy IdentityProviderConfiguration %q: %w", name, err)
		}
		if !Migrated(&legacy) {
			patch := ctrlruntimeclient.MergeFrom(legacy.DeepCopy())
			annotations := legacy.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[MigratedAnnotation] = "true"
			legacy.SetAnnotations(annotations)
			if err := cl.Patch(ctx, &legacy, patch); err != nil {
				return nil, fmt.Errorf("marking legacy IdentityProviderConfiguration %q migrated: %w", name, err)
			}
		}
		return identityProviderConfigurationToCurrent(&legacy).Status.ManagedClients, nil
	}
}

// IdentityProviderConfigurationExists reports whether a deprecated
// IdentityProviderConfiguration named name exists in cluster.
// getter must reach kcp with admin credentials (webhook requests carry the
// cluster as kcp.io/cluster annotation, not a provider-engaged cluster).
func IdentityProviderConfigurationExists(getter iclient.KCPClientGetter) func(ctx context.Context, cluster logicalcluster.Name, name string) (bool, error) {
	return func(ctx context.Context, cluster logicalcluster.Name, name string) (bool, error) {
		cl, err := getter.NewClientForLogicalCluster(ctx, cluster.String())
		if err != nil {
			return false, fmt.Errorf("getting client for cluster %q: %w", cluster, err)
		}
		//nolint:staticcheck // legacy kind served on purpose
		var legacy pmcorev1alpha1.IdentityProviderConfiguration
		if err := cl.Get(ctx, types.NamespacedName{Name: name}, &legacy); err != nil {
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				return false, nil
			}
			return false, fmt.Errorf("getting legacy IdentityProviderConfiguration %q: %w", name, err)
		}
		return true, nil
	}
}

// IdentityProviderConfigurationMirror copies the status of a
// keycloak.platform-mesh.io IdentityProviderConfiguration to the deprecated
// one of the same name in the same logical cluster and marks it migrated, so
// consumers still reading the deprecated kind (the portal) see the clients the
// successor manages. No-op without a deprecated twin.
// getter must reach the legacy kind through the system provider.
func IdentityProviderConfigurationMirror(getter iclient.KCPClientGetter) func(ctx context.Context, engaged multicluster.ClusterName, name string, status pmkeycloakv1alpha1.IdentityProviderConfigurationStatus) error {
	return func(ctx context.Context, engaged multicluster.ClusterName, name string, status pmkeycloakv1alpha1.IdentityProviderConfigurationStatus) error {
		cl, err := getter.NewClientForLogicalCluster(ctx, string(config.ProviderClusterName(config.SystemProviderName, engaged)))
		if err != nil {
			return fmt.Errorf("getting system client for cluster %q: %w", engaged, err)
		}
		//nolint:staticcheck // legacy kind served on purpose
		var legacy pmcorev1alpha1.IdentityProviderConfiguration
		if err := cl.Get(ctx, types.NamespacedName{Name: name}, &legacy); err != nil {
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				return nil
			}
			return fmt.Errorf("getting legacy IdentityProviderConfiguration %q: %w", name, err)
		}
		if !Migrated(&legacy) {
			patch := ctrlruntimeclient.MergeFrom(legacy.DeepCopy())
			annotations := legacy.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[MigratedAnnotation] = "true"
			legacy.SetAnnotations(annotations)
			if err := cl.Patch(ctx, &legacy, patch); err != nil {
				return fmt.Errorf("marking legacy IdentityProviderConfiguration %q migrated: %w", name, err)
			}
		}
		desired := IdentityProviderConfigurationStatusFromCurrent(status)
		if equality.Semantic.DeepEqual(legacy.Status, desired) {
			return nil
		}
		patch := ctrlruntimeclient.MergeFrom(legacy.DeepCopy())
		legacy.Status = desired
		if err := cl.Status().Patch(ctx, &legacy, patch); err != nil {
			return fmt.Errorf("mirroring status to legacy IdentityProviderConfiguration %q: %w", name, err)
		}
		return nil
	}
}
