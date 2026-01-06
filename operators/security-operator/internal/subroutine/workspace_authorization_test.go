package subroutine

import (
	"context"
	"errors"
	"testing"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alphav1 "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/config"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestWorkspaceAuthSubroutine_Process(t *testing.T) {
	tests := []struct {
		name           string
		logicalCluster *kcpv1alpha1.LogicalCluster
		cfg            config.Config
		setupMocks     func(*mocks.MockClient)
		expectError    bool
		expectedResult ctrl.Result
	}{
		{
			name: "success - create new WorkspaceAuthenticationConfiguration",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test-workspace",
					},
				},
			},
			cfg: config.Config{BaseDomain: "test.domain", GroupClaim: "groups", UserClaim: "email"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(apierrors.NewNotFound(kcptenancyv1alphav1.Resource("workspaceauthenticationconfigurations"), "test-workspace")).Once()
				m.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						wac := obj.(*kcptenancyv1alphav1.WorkspaceAuthenticationConfiguration)
						assert.Equal(t, "test-workspace", wac.Name)
						assert.Equal(t, "https://test.domain/keycloak/realms/test-workspace", wac.Spec.JWT[0].Issuer.URL)
						assert.Equal(t, kcptenancyv1alphav1.AudienceMatchPolicyMatchAny, wac.Spec.JWT[0].Issuer.AudienceMatchPolicy)
						assert.Equal(t, "groups", wac.Spec.JWT[0].ClaimMappings.Groups.Claim)
						assert.Equal(t, "email", wac.Spec.JWT[0].ClaimMappings.Username.Claim)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace-org"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "test-workspace-org"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "test-workspace-org", wt.Name)
						assert.Equal(t, "test-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace-acc"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "test-workspace-acc"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "test-workspace-acc", wt.Name)
						assert.Equal(t, "test-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()
			},
			expectError:    false,
			expectedResult: ctrl.Result{},
		},
		{
			name: "success - update existing WorkspaceAuthenticationConfiguration",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:existing-workspace",
					},
				},
			},
			cfg: config.Config{BaseDomain: "example.com", GroupClaim: "groups", UserClaim: "email"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "existing-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wac := obj.(*kcptenancyv1alphav1.WorkspaceAuthenticationConfiguration)
						wac.Name = "existing-workspace"
						wac.Spec = kcptenancyv1alphav1.WorkspaceAuthenticationConfigurationSpec{
							JWT: []kcptenancyv1alphav1.JWTAuthenticator{
								{
									Issuer: kcptenancyv1alphav1.Issuer{
										URL: "https://old.domain/keycloak/realms/existing-workspace",
									},
								},
							},
						}
						return nil
					}).Once()
				m.EXPECT().Update(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
						wac := obj.(*kcptenancyv1alphav1.WorkspaceAuthenticationConfiguration)
						assert.Equal(t, "existing-workspace", wac.Name)
						assert.Equal(t, "https://example.com/keycloak/realms/existing-workspace", wac.Spec.JWT[0].Issuer.URL)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "existing-workspace-org"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "existing-workspace-org"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "existing-workspace-org", wt.Name)
						assert.Equal(t, "existing-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "existing-workspace-acc"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "existing-workspace-acc"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "existing-workspace-acc", wt.Name)
						assert.Equal(t, "existing-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()
			},
			expectError:    false,
			expectedResult: ctrl.Result{},
		},
		{
			name: "error - missing workspace path annotation",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			cfg:            config.Config{BaseDomain: "test.domain", GroupClaim: "groups", UserClaim: "email"},
			setupMocks:     func(m *mocks.MockClient) {},
			expectError:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "error - empty workspace path annotation",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "",
					},
				},
			},
			cfg:            config.Config{BaseDomain: "test.domain", GroupClaim: "groups", UserClaim: "email"},
			setupMocks:     func(m *mocks.MockClient) {},
			expectError:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "error - create fails",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test-workspace",
					},
				},
			},
			cfg: config.Config{BaseDomain: "test.domain", GroupClaim: "groups", UserClaim: "email"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(apierrors.NewNotFound(kcptenancyv1alphav1.Resource("workspaceauthenticationconfigurations"), "test-workspace")).Once()
				m.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(errors.New("create failed")).Once()
			},
			expectError:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "error - update fails",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test-workspace",
					},
				},
			},
			cfg: config.Config{BaseDomain: "test.domain", GroupClaim: "groups", UserClaim: "email"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wac := obj.(*kcptenancyv1alphav1.WorkspaceAuthenticationConfiguration)
						wac.Name = "test-workspace"
						return nil
					}).Once()
				m.EXPECT().Update(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(errors.New("update failed")).Once()
			},
			expectError:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "error - get fails with non-not-found error",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test-workspace",
					},
				},
			},
			cfg: config.Config{BaseDomain: "test.domain", GroupClaim: "groups", UserClaim: "email"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(errors.New("get failed")).Once()
			},
			expectError:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "success - workspace path with single element",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "single-workspace",
					},
				},
			},
			cfg: config.Config{BaseDomain: "test.domain", GroupClaim: "groups", UserClaim: "email"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "single-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(apierrors.NewNotFound(kcptenancyv1alphav1.Resource("workspaceauthenticationconfigurations"), "single-workspace")).Once()
				m.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						wac := obj.(*kcptenancyv1alphav1.WorkspaceAuthenticationConfiguration)
						assert.Equal(t, "single-workspace", wac.Name)
						assert.Equal(t, "https://test.domain/keycloak/realms/single-workspace", wac.Spec.JWT[0].Issuer.URL)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "single-workspace-org"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "single-workspace-org"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "single-workspace-org", wt.Name)
						assert.Equal(t, "single-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "single-workspace-acc"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "single-workspace-acc"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "single-workspace-acc", wt.Name)
						assert.Equal(t, "single-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()
			},
			expectError:    false,
			expectedResult: ctrl.Result{},
		},
		{
			name: "success - workspace path with single element and domain CA lookup",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "single-workspace",
					},
				},
			},
			cfg: config.Config{
				BaseDomain:     "test.domain",
				GroupClaim:     "groups",
				UserClaim:      "email",
				DomainCALookup: true,
			},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "domain-certificate-ca", Namespace: "platform-mesh-system"}, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						secret := obj.(*corev1.Secret)
						secret.Data = map[string][]byte{
							"tls.crt": []byte("dummy-ca-data"),
						}
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "single-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(apierrors.NewNotFound(kcptenancyv1alphav1.Resource("workspaceauthenticationconfigurations"), "single-workspace")).Once()
				m.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						wac := obj.(*kcptenancyv1alphav1.WorkspaceAuthenticationConfiguration)
						assert.Equal(t, "single-workspace", wac.Name)
						assert.Equal(t, "https://test.domain/keycloak/realms/single-workspace", wac.Spec.JWT[0].Issuer.URL)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "single-workspace-org"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "single-workspace-org"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "single-workspace-org", wt.Name)
						assert.Equal(t, "single-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "single-workspace-acc"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "single-workspace-acc"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "single-workspace-acc", wt.Name)
						assert.Equal(t, "single-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()
			},
			expectError:    false,
			expectedResult: ctrl.Result{},
		},
		{
			name: "error - patchWorkspaceType fails for -org",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test-workspace",
					},
				},
			},
			cfg: config.Config{BaseDomain: "test.domain", GroupClaim: "groups", UserClaim: "email"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(apierrors.NewNotFound(kcptenancyv1alphav1.Resource("workspaceauthenticationconfigurations"), "test-workspace")).Once()
				m.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).Return(nil).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace-org"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "test-workspace-org"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					Return(errors.New("failed to patch workspace type")).Once()
			},
			expectError:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "error - patchWorkspaceType fails for -acc",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test-workspace",
					},
				},
			},
			cfg: config.Config{BaseDomain: "test.domain", GroupClaim: "groups", UserClaim: "email"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(apierrors.NewNotFound(kcptenancyv1alphav1.Resource("workspaceauthenticationconfigurations"), "test-workspace")).Once()
				m.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).Return(nil).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace-org"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "test-workspace-org"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).Return(nil).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test-workspace-acc"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "test-workspace-acc"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					Return(errors.New("failed to patch workspace type")).Once()
			},
			expectError:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "error - domain CA secret Get fails",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test-workspace",
					},
				},
			},
			cfg: config.Config{
				BaseDomain:     "test.domain",
				GroupClaim:     "groups",
				UserClaim:      "email",
				DomainCALookup: true,
			},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "domain-certificate-ca", Namespace: "platform-mesh-system"}, mock.Anything, mock.Anything).
					Return(errors.New("failed to get domain CA secret")).Once()
			},
			expectError:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "success - allow unverified emails in development mode",
			logicalCluster: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:dev-workspace",
					},
				},
			},
			cfg: config.Config{
				BaseDomain:                       "dev.domain",
				GroupClaim:                       "groups",
				UserClaim:                        "email",
				DevelopmentAllowUnverifiedEmails: true,
			},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "dev-workspace"}, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					Return(apierrors.NewNotFound(kcptenancyv1alphav1.Resource("workspaceauthenticationconfigurations"), "dev-workspace")).Once()

				m.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceAuthenticationConfiguration"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						wac := obj.(*kcptenancyv1alphav1.WorkspaceAuthenticationConfiguration)
						assert.Equal(t, "dev-workspace", wac.Name)
						assert.Equal(t, "https://dev.domain/keycloak/realms/dev-workspace", wac.Spec.JWT[0].Issuer.URL)
						assert.Equal(t, kcptenancyv1alphav1.AudienceMatchPolicyMatchAny, wac.Spec.JWT[0].Issuer.AudienceMatchPolicy)
						assert.Equal(t, "groups", wac.Spec.JWT[0].ClaimMappings.Groups.Claim)
						assert.Equal(t, "claims.email", wac.Spec.JWT[0].ClaimMappings.Username.Expression)
						assert.Equal(t, "", wac.Spec.JWT[0].ClaimMappings.Username.Claim)
						assert.Len(t, wac.Spec.JWT[0].ClaimValidationRules, 1)
						assert.Equal(t, "claims.?email_verified.orValue(true) == true || claims.?email_verified.orValue(true) == false", wac.Spec.JWT[0].ClaimValidationRules[0].Expression)
						assert.Equal(t, "Allowing both verified and unverified emails", wac.Spec.JWT[0].ClaimValidationRules[0].Message)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "dev-workspace-org"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "dev-workspace-org"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "dev-workspace-org", wt.Name)
						assert.Equal(t, "dev-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()

				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "dev-workspace-acc"}, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						wt.Name = "dev-workspace-acc"
						return nil
					}).Once()
				m.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType"), mock.Anything).
					RunAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						wt := obj.(*kcptenancyv1alphav1.WorkspaceType)
						assert.Equal(t, "dev-workspace-acc", wt.Name)
						assert.Equal(t, "dev-workspace", wt.Spec.AuthenticationConfigurations[0].Name)
						return nil
					}).Once()
			},
			expectError:    false,
			expectedResult: ctrl.Result{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := mocks.NewMockClient(t)
			if tt.setupMocks != nil {
				tt.setupMocks(mockClient)
			}

			subroutine := NewWorkspaceAuthConfigurationSubroutine(mockClient, mockClient, tt.cfg)

			result, opErr := subroutine.Process(context.Background(), tt.logicalCluster)

			if tt.expectError {
				assert.NotNil(t, opErr)
			} else {
				assert.Nil(t, opErr)
			}

			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestWorkspaceAuthConfigurationSubroutine_GetName(t *testing.T) {
	sub := NewWorkspaceAuthConfigurationSubroutine(nil, nil, config.Config{})
	assert.Equal(t, "workspaceAuthConfiguration", sub.GetName())
}

func TestWorkspaceAuthConfigurationSubroutine_Finalizers(t *testing.T) {
	sub := NewWorkspaceAuthConfigurationSubroutine(nil, nil, config.Config{})
	assert.Equal(t, []string{}, sub.Finalizers(nil))
}

func TestWorkspaceAuthConfigurationSubroutine_Finalize(t *testing.T) {
	sub := NewWorkspaceAuthConfigurationSubroutine(nil, nil, config.Config{})
	result, err := sub.Finalize(context.Background(), nil)
	assert.Nil(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}
