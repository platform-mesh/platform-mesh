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

package directory

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.platform-mesh.io/apis/identity/authentication"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idpdirectory"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
)

const KeycloakControllerName = "keycloak.platform-mesh.io"

// KCPResolver uses installation-scoped readers, never caller-selected endpoints.
// The provider records a Keycloak tenant's realm name as status.externalId.
type KCPResolver struct {
	ClassReader      ctrlruntimeclient.Reader
	ReaderForCluster func(context.Context, logicalcluster.Name) (ctrlruntimeclient.Reader, error)
	ClassName        string
}

func (r *KCPResolver) Resolve(ctx context.Context, requested idpdirectory.Tenant) (TenantConfiguration, error) {
	if r.ClassReader == nil || r.ReaderForCluster == nil || r.ClassName == "" {
		return TenantConfiguration{}, errors.New("directory provider class reader and cluster resolver are required")
	}
	cluster := logicalcluster.Name(requested.LogicalCluster)
	if !cluster.IsValid() || requested.Name == "" || requested.UID == "" {
		return TenantConfiguration{}, idpdirectory.ErrUnauthorized
	}
	reader, err := r.ReaderForCluster(ctx, cluster)
	if err != nil {
		return TenantConfiguration{}, fmt.Errorf("resolve tenant cluster: %w", err)
	}
	var tenant pmidentityv1alpha1.IdentityTenant
	if err := reader.Get(ctx, ctrlruntimeclient.ObjectKey{Name: requested.Name}, &tenant); err != nil {
		if apierrors.IsNotFound(err) {
			return TenantConfiguration{}, idpdirectory.ErrNotFound
		}
		return TenantConfiguration{}, fmt.Errorf("read identity tenant: %w", err)
	}
	if string(tenant.UID) != requested.UID || logicalcluster.From(&tenant) != cluster ||
		tenant.Spec.ProviderClassRef != r.ClassName ||
		tenant.Spec.Purpose != pmidentityv1alpha1.IdentityTenantPurposeOrganization {
		return TenantConfiguration{}, idpdirectory.ErrUnauthorized
	}
	var class pmidentityv1alpha1.IdentityProviderClass
	if err := r.ClassReader.Get(ctx, ctrlruntimeclient.ObjectKey{Name: r.ClassName}, &class); err != nil {
		return TenantConfiguration{}, fmt.Errorf("read directory provider class: %w", err)
	}
	if class.Spec.ControllerName != KeycloakControllerName {
		return TenantConfiguration{}, idpdirectory.ErrUnauthorized
	}
	if class.Spec.AuthenticationPolicy.IssuerMode != pmidentityv1alpha1.IssuerModeDedicated {
		return TenantConfiguration{}, errors.New("keycloak directory requires dedicated issuers; shared realms require a membership-filtering adapter")
	}
	if !slices.Contains(class.Status.Capabilities, pmidentityv1alpha1.IdentityCapabilityDirectory) {
		return TenantConfiguration{}, errors.New("provider class has not established directory capability")
	}
	if _, err := authentication.Resolve(&class, &tenant, ""); err != nil {
		return TenantConfiguration{}, fmt.Errorf("directory tenant is not ready: %w", err)
	}
	return TenantConfiguration{Realm: tenant.Status.ExternalID, IssuerURL: tenant.Status.IssuerURL}, nil
}
