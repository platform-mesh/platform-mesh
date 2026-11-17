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

package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idpactivation"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

type testActivationGate struct {
	check func(idpactivation.Binding) (idpactivation.Permit, error)
}

func (g testActivationGate) Check(_ context.Context, binding idpactivation.Binding) (idpactivation.Permit, error) {
	if g.check != nil {
		return g.check(binding)
	}
	return idpactivation.Permit{Epoch: "epoch-1", RecordUID: "approval-uid", ResourceVersion: "7"}, nil
}

func TestPublicationRequiresActivationBeforeCreatingIntent(t *testing.T) {
	engine, workspace, _ := organizationFixture(t)
	publisher := &Publisher{
		Organization: engine, InstallationID: "installation", ClientConfigurationDigest: "sha256:" + strings.Repeat("a", 64),
		OrganizationWorkspacePath: "root:orgs",
		Gate: testActivationGate{check: func(idpactivation.Binding) (idpactivation.Permit, error) {
			return idpactivation.Permit{}, errors.New("participants are not prepared")
		}},
	}
	_, err := publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "participants are not prepared")
	tenants := &pmidentityv1alpha1.IdentityTenantList{}
	require.NoError(t, engine.Organizations.List(t.Context(), tenants))
	require.Empty(t, tenants.Items)
}

func TestPublicationPersistsActivationOnNewUnreadyTenant(t *testing.T) {
	engine, workspace, _ := organizationFixture(t)
	info := &pmcorev1alpha1.AccountInfo{}
	require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
	info.Spec.OIDC = nil
	require.NoError(t, workspace.Update(t.Context(), info))
	publisher := &Publisher{
		Organization: engine, InstallationID: "installation", ClientConfigurationDigest: "sha256:" + strings.Repeat("a", 64),
		OrganizationWorkspacePath: "root:orgs",
		Gate:                      testActivationGate{},
	}
	observation, err := publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorIs(t, err, ErrIdentityNotReady)
	require.NotNil(t, observation)
	require.True(t, observation.TenantCreated)
	require.Equal(t, "epoch-1", observation.Tenant.Annotations[epochAnnotation])
	require.Equal(t, "approval-uid", observation.Tenant.Annotations[approvalAnnotation])
	require.NoError(t, publisher.Organization.Organizations.Get(t.Context(),
		ctrlruntimeclient.ObjectKey{Name: "organization"}, observation.Tenant))
	require.Equal(t, "epoch-1", observation.Tenant.Annotations[epochAnnotation])
}

func TestPublicationDoesNotCreateTenantAfterActivationRevocation(t *testing.T) {
	engine, workspace, _ := organizationFixture(t)
	info := &pmcorev1alpha1.AccountInfo{}
	require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
	info.Spec.OIDC = nil
	require.NoError(t, workspace.Update(t.Context(), info))
	calls := 0
	publisher := &Publisher{
		Organization: engine, InstallationID: "installation", ClientConfigurationDigest: "sha256:" + strings.Repeat("a", 64),
		OrganizationWorkspacePath: "root:orgs",
		Gate: testActivationGate{check: func(idpactivation.Binding) (idpactivation.Permit, error) {
			calls++
			if calls > 1 {
				return idpactivation.Permit{}, errors.New("activation quiesced")
			}
			return idpactivation.Permit{Epoch: "epoch-1", RecordUID: "approval-uid", ResourceVersion: "7"}, nil
		}},
	}
	_, err := publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "activation quiesced")
	tenants := &pmidentityv1alpha1.IdentityTenantList{}
	require.NoError(t, engine.Organizations.List(t.Context(), tenants))
	require.Empty(t, tenants.Items)
}

func TestPublicationDoesNotUpdateTenantAfterActivationRevocation(t *testing.T) {
	publisher, workspace := publicationFixture(t)
	tenant := &pmidentityv1alpha1.IdentityTenant{}
	require.NoError(t, publisher.Organization.Organizations.Get(t.Context(),
		ctrlruntimeclient.ObjectKey{Name: "organization"}, tenant))
	generation := tenant.Generation
	publisher.Organization.Clients = func(string) ([]pmidentityv1alpha1.OIDCClient, error) {
		client := tenant.Spec.Clients[0]
		client.DisplayName = "updated"
		return []pmidentityv1alpha1.OIDCClient{client}, nil
	}
	calls := 0
	publisher.Gate = testActivationGate{check: func(idpactivation.Binding) (idpactivation.Permit, error) {
		calls++
		if calls > 1 {
			return idpactivation.Permit{}, errors.New("activation quiesced")
		}
		return idpactivation.Permit{Epoch: "epoch-1", RecordUID: "approval-uid", ResourceVersion: "7"}, nil
	}}
	_, err := publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "activation quiesced")
	require.NoError(t, publisher.Organization.Organizations.Get(t.Context(),
		ctrlruntimeclient.ObjectKey{Name: "organization"}, tenant))
	require.Equal(t, generation, tenant.Generation)
	require.NotEqual(t, "updated", tenant.Spec.Clients[0].DisplayName)
}

func TestPublicationRejectsLegacyAccountInfoBeforeCreatingTenant(t *testing.T) {
	engine, workspace, _ := organizationFixture(t)
	info := &pmcorev1alpha1.AccountInfo{}
	require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
	info.Spec.OIDC = &pmcorev1alpha1.OIDCInfo{IssuerURL: "https://legacy.example"}
	require.NoError(t, workspace.Update(t.Context(), info))
	publisher := &Publisher{
		Organization: engine, Gate: testActivationGate{}, InstallationID: "installation",
		ClientConfigurationDigest: "sha256:" + strings.Repeat("a", 64), OrganizationWorkspacePath: "root:orgs",
	}
	_, err := publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "legacy AccountInfo")
	tenants := &pmidentityv1alpha1.IdentityTenantList{}
	require.NoError(t, engine.Organizations.List(t.Context(), tenants))
	require.Empty(t, tenants.Items)
}

func TestPublicationRejectsUnmanagedExistingTenant(t *testing.T) {
	publisher, workspace := publicationFixture(t)
	tenant := &pmidentityv1alpha1.IdentityTenant{}
	require.NoError(t, publisher.Organization.Organizations.Get(t.Context(),
		ctrlruntimeclient.ObjectKey{Name: "organization"}, tenant))
	delete(tenant.Annotations, epochAnnotation)
	delete(tenant.Annotations, approvalAnnotation)
	tenant.Spec.Clients[0].DisplayName = "unmanaged"
	require.NoError(t, publisher.Organization.Organizations.Update(t.Context(), tenant))
	_, err := publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "explicit coordinated migration approval")
	require.NoError(t, publisher.Organization.Organizations.Get(t.Context(),
		ctrlruntimeclient.ObjectKey{Name: "organization"}, tenant))
	require.Equal(t, "unmanaged", tenant.Spec.Clients[0].DisplayName)
}

func TestPublicationUsesObservedIdentityPreservesUnrelatedFieldsAndConverges(t *testing.T) {
	publisher, workspace := publicationFixture(t)
	observation, err := publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.NoError(t, err)
	info := &pmcorev1alpha1.AccountInfo{}
	require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
	require.Equal(t, "retained-fga-store", info.Spec.FGA.Store.Id)
	require.Equal(t, observation.OIDC, info.Spec.OIDC)
	require.Equal(t, "epoch-1", info.Annotations[epochAnnotation])
	auth := &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{}
	authName := authenticationConfigurationName("organization", "epoch-1")
	authKey := ctrlruntimeclient.ObjectKey{Name: authName}
	require.NoError(t, publisher.Organization.Organizations.Get(t.Context(), authKey, auth))
	require.Equal(t, observation.OIDC.IssuerURL, auth.Spec.JWT[0].Issuer.URL)
	for _, name := range []string{"organization-org", "organization-account"} {
		workspaceType := &kcptenancyv1alpha1.WorkspaceType{}
		require.NoError(t, publisher.Organization.Organizations.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, workspaceType))
		require.Equal(t, []kcptenancyv1alpha1.AuthenticationConfigurationReference{{Name: authName}}, workspaceType.Spec.AuthenticationConfigurations)
	}
	infoVersion, authVersion := info.ResourceVersion, auth.ResourceVersion
	_, err = publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.NoError(t, err)
	require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
	require.NoError(t, publisher.Organization.Organizations.Get(t.Context(), authKey, auth))
	require.Equal(t, infoVersion, info.ResourceVersion)
	require.Equal(t, authVersion, auth.ResourceVersion)

	observation.Tenant.Status.IssuerURL = "https://replacement.example"
	require.NoError(t, publisher.Organization.Organizations.Status().Update(t.Context(), observation.Tenant))
	_, err = publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "existing AccountInfo identity tenant binding requires coordinated migration")
	require.NoError(t, publisher.Organization.Organizations.Get(t.Context(), authKey, auth))
	require.NotEqual(t, "https://replacement.example", auth.Spec.JWT[0].Issuer.URL)
}

func TestPublicationDoesNotOverwriteLegacyAuthenticationOrProceedAfterRevocation(t *testing.T) {
	publisher, workspace := publicationFixture(t)
	info := &pmcorev1alpha1.AccountInfo{}
	require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
	info.Spec.OIDC = &pmcorev1alpha1.OIDCInfo{IssuerURL: "https://legacy.example"}
	require.NoError(t, workspace.Update(t.Context(), info))
	_, err := publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "legacy AccountInfo")
	info.Spec.OIDC = nil
	require.NoError(t, workspace.Update(t.Context(), info))
	calls := 0
	publisher.Gate = testActivationGate{check: func(idpactivation.Binding) (idpactivation.Permit, error) {
		calls++
		if calls > 1 {
			return idpactivation.Permit{}, errors.New("activation quiesced")
		}
		return idpactivation.Permit{Epoch: "epoch-1", RecordUID: "approval-uid", ResourceVersion: "7"}, nil
	}}
	_, err = publisher.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "activation quiesced")
	auth := &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{}
	require.True(t, apierrors.IsNotFound(publisher.Organization.Organizations.Get(t.Context(), ctrlruntimeclient.ObjectKey{
		Name: authenticationConfigurationName("organization", "epoch-1"),
	}, auth)))
	require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
	require.Nil(t, info.Spec.OIDC)
}

func publicationFixture(t *testing.T) (*Publisher, ctrlruntimeclient.Client) {
	t.Helper()
	engine, workspace, class := organizationFixture(t)
	observation, err := engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorIs(t, err, ErrIdentityNotReady)
	readyOrganizationTenant(observation.Tenant, class)
	require.NoError(t, engine.Organizations.Status().Update(t.Context(), observation.Tenant))
	observation, err = engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.NoError(t, err)
	stampActivation(observation.Tenant, idpactivation.Permit{
		Epoch: "epoch-1", RecordUID: "approval-uid", ResourceVersion: "7",
	}, observation)
	require.NoError(t, engine.Organizations.Update(t.Context(), observation.Tenant))
	rootWorkspace := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, engine.Organizations.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "organization"}, rootWorkspace))
	rootWorkspace.Spec.Type = &kcptenancyv1alpha1.WorkspaceTypeReference{Name: "organization-org", Path: "root:orgs"}
	require.NoError(t, engine.Organizations.Update(t.Context(), rootWorkspace))
	for _, name := range []string{"organization-org", "organization-account"} {
		workspaceType := &kcptenancyv1alpha1.WorkspaceType{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, UID: "type-uid",
				Labels:      map[string]string{"core.platform-mesh.io/org": "organization"},
				Annotations: map[string]string{logicalcluster.AnnotationKey: "organizations-root"},
			},
		}
		if name == "organization-org" {
			workspaceType.Spec.DefaultChildWorkspaceType = &kcptenancyv1alpha1.WorkspaceTypeReference{Name: "organization-account", Path: "root:orgs"}
		}
		require.NoError(t, engine.Organizations.Create(t.Context(), workspaceType))
	}
	info := &pmcorev1alpha1.AccountInfo{}
	require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
	info.Spec.OIDC = nil
	info.Spec.FGA.Store.Id = "retained-fga-store"
	require.NoError(t, workspace.Update(t.Context(), info))
	return &Publisher{
		Organization: engine, Gate: testActivationGate{}, InstallationID: "installation",
		ClientConfigurationDigest: "sha256:" + strings.Repeat("a", 64), OrganizationWorkspacePath: "root:orgs",
	}, workspace
}
