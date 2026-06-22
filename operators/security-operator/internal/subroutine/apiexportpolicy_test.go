package subroutine_test

import (
	"context"
	"errors"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func getAPIExportPolicyTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	return scheme
}

func TestAPIExportPolicySubroutine_GetName(t *testing.T) {
	sub := subroutine.NewAPIExportPolicySubroutine(nil, nil, nil, nil, nil)
	assert.Equal(t, "APIExportPolicySubroutine", sub.GetName())
}

func TestAPIExportPolicySubroutine_Finalizers(t *testing.T) {
	sub := subroutine.NewAPIExportPolicySubroutine(nil, nil, nil, nil, nil)
	assert.Equal(t, []string{"system.platform-mesh.io/apiexportpolicy-finalizer"}, sub.Finalizers(nil))
}

func TestAPIExportPolicySubroutine_Process(t *testing.T) {
	tests := []struct {
		name        string
		policy      *corev1alpha1.APIExportPolicy
		setupMocks  func(*testing.T, *mocks.MockOpenFGAServiceClient, *mocks.MockStoreIDGetter, *mocks.MockKCPClientGetter, *mocks.MockLister)
		cfg         *config.Config
		expectError bool
	}{
		{
			name: "should fail when getting provider cluster ID fails - GetCluster fails",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(nil, errors.New("failed to get cluster"))
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "should fail when expression starts with wrong prefix",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"wrong:prefix:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "should handle wildcard expression with root:orgs path - List fails",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				lister.EXPECT().List(mock.Anything, mock.Anything).Return(errors.New("unable to list")).Maybe()
			},
			cfg:         &config.Config{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			storeIDGetter := mocks.NewMockStoreIDGetter(t)
			lister := mocks.NewMockLister(t)
			kcpClientGetter := mocks.NewMockKCPClientGetter(t)

			if tt.setupMocks != nil {
				tt.setupMocks(t, fga, storeIDGetter, kcpClientGetter, lister)
			}

			l := testlogger.New()
			ctx := l.WithContext(context.Background())

			sub := subroutine.NewAPIExportPolicySubroutine(fga, tt.cfg, storeIDGetter, lister, kcpClientGetter)

			_, err := sub.Process(ctx, tt.policy)

			if tt.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestAPIExportPolicySubroutine_Finalize(t *testing.T) {
	tests := []struct {
		name        string
		policy      *corev1alpha1.APIExportPolicy
		setupMocks  func(*testing.T, *mocks.MockOpenFGAServiceClient, *mocks.MockStoreIDGetter, *mocks.MockKCPClientGetter, *mocks.MockLister)
		cfg         *config.Config
		expectError bool
	}{
		{
			name: "should fail when getting provider cluster ID fails",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(nil, errors.New("failed to get cluster"))
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "should handle finalize with wildcard expression - NewClientForLogicalCluster fails",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:acme:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(nil, errors.New("failed to get acme client"))
			},
			cfg:         &config.Config{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			storeIDGetter := mocks.NewMockStoreIDGetter(t)
			lister := mocks.NewMockLister(t)
			kcpClientGetter := mocks.NewMockKCPClientGetter(t)

			if tt.setupMocks != nil {
				tt.setupMocks(t, fga, storeIDGetter, kcpClientGetter, lister)
			}

			l := testlogger.New()
			ctx := l.WithContext(context.Background())

			sub := subroutine.NewAPIExportPolicySubroutine(fga, tt.cfg, storeIDGetter, lister, kcpClientGetter)

			_, err := sub.Finalize(ctx, tt.policy)

			if tt.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestAPIExportPolicySubroutine_Process_Success(t *testing.T) {
	tests := []struct {
		name                string
		policy              *corev1alpha1.APIExportPolicy
		setupMocks          func(*testing.T, *mocks.MockOpenFGAServiceClient, *mocks.MockStoreIDGetter, *mocks.MockKCPClientGetter, *mocks.MockLister)
		cfg                 *config.Config
		expectError         bool
		expectedTupleWrites []corev1alpha1.Tuple
	}{
		{
			name: "should write correct FGA tuple for single org expression",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()

				// Provider cluster mock
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)

				// Target workspace client with AccountInfo
				targetClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "account",
						},
						Spec: corev1alpha1.AccountInfoSpec{
							Account: corev1alpha1.AccountLocation{
								Name:            "acme-account",
								OriginClusterId: "acme-cluster-id",
								Type:            corev1alpha1.AccountTypeOrg,
							},
							Organization: corev1alpha1.AccountLocation{
								Name: "acme-org",
							},
						},
					}).
					Build()

				// Cluster client for status patch
				clusterClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&corev1alpha1.APIExportPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-policy",
						},
					}).
					WithStatusSubresource(&corev1alpha1.APIExportPolicy{}).
					Build()

				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil)

				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("test-store-id", nil)

				// Expect FGA Write with specific tuple content
				fga.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
					if req.StoreId != "test-store-id" {
						return false
					}
					if len(req.Writes.TupleKeys) != 1 {
						return false
					}
					tuple := req.Writes.TupleKeys[0]
					return tuple.Object == "core_platform-mesh_io_account:acme-cluster-id/acme-account" &&
						tuple.Relation == "bind" &&
						tuple.User == "apis_kcp_io_apiexport:provider-cluster-id/my-export"
				}), mock.Anything).Return(&openfgav1.WriteResponse{}, nil)

				kcpClientGetter.EXPECT().NewClientFromContext(mock.Anything).Return(clusterClient, nil)
			},
			cfg:         &config.Config{},
			expectError: false,
		},
		{
			name: "should write correct FGA tuple with bind_inherited for wildcard expression",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:acme:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()

				// Provider cluster mock
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)

				// Target workspace client with AccountInfo
				targetClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "account",
						},
						Spec: corev1alpha1.AccountInfoSpec{
							Account: corev1alpha1.AccountLocation{
								Name:            "acme-account",
								OriginClusterId: "acme-cluster-id",
								Type:            corev1alpha1.AccountTypeOrg,
							},
							Organization: corev1alpha1.AccountLocation{
								Name: "acme-org",
							},
						},
					}).
					Build()

				// Cluster client for status patch
				clusterClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&corev1alpha1.APIExportPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-policy",
						},
					}).
					WithStatusSubresource(&corev1alpha1.APIExportPolicy{}).
					Build()

				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil)

				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("test-store-id", nil)

				// Expect FGA Write with bind_inherited relation for wildcard
				fga.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
					if req.StoreId != "test-store-id" {
						return false
					}
					if len(req.Writes.TupleKeys) != 1 {
						return false
					}
					tuple := req.Writes.TupleKeys[0]
					return tuple.Object == "core_platform-mesh_io_account:acme-cluster-id/acme-account" &&
						tuple.Relation == "bind_inherited" &&
						tuple.User == "apis_kcp_io_apiexport:provider-cluster-id/my-export"
				}), mock.Anything).Return(&openfgav1.WriteResponse{}, nil)

				kcpClientGetter.EXPECT().NewClientFromContext(mock.Anything).Return(clusterClient, nil)
			},
			cfg:         &config.Config{},
			expectError: false,
		},
		{
			name: "should write FGA tuples for all orgs when root:orgs:* expression is used",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()

				// Provider cluster mock
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)

				// All client with AccountInfo list
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					if list, ok := ol.(*corev1alpha1.AccountInfoList); ok {
						list.Items = []corev1alpha1.AccountInfo{
							{
								ObjectMeta: metav1.ObjectMeta{Name: "account-1"},
								Spec: corev1alpha1.AccountInfoSpec{
									Account:      corev1alpha1.AccountLocation{Name: "org1-account", OriginClusterId: "org1-cluster-id", Type: corev1alpha1.AccountTypeOrg},
									Organization: corev1alpha1.AccountLocation{Name: "org1"},
								},
							},
							{
								ObjectMeta: metav1.ObjectMeta{Name: "account-2"},
								Spec: corev1alpha1.AccountInfoSpec{
									Account:      corev1alpha1.AccountLocation{Name: "org2-account", OriginClusterId: "org2-cluster-id", Type: corev1alpha1.AccountTypeOrg},
									Organization: corev1alpha1.AccountLocation{Name: "org2"},
								},
							},
						}
					}
					return nil
				})

				// Cluster client for status patch
				clusterClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&corev1alpha1.APIExportPolicy{
						ObjectMeta: metav1.ObjectMeta{Name: "test-policy"},
					}).
					WithStatusSubresource(&corev1alpha1.APIExportPolicy{}).
					Build()

				storeIDGetter.EXPECT().Get(mock.Anything, "org1").Return("store-id-org1", nil)
				storeIDGetter.EXPECT().Get(mock.Anything, "org2").Return("store-id-org2", nil)

				// Expect FGA Write for org1 with bind_inherited relation
				fga.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
					if req.StoreId != "store-id-org1" {
						return false
					}
					if len(req.Writes.TupleKeys) != 1 {
						return false
					}
					tuple := req.Writes.TupleKeys[0]
					return tuple.Object == "core_platform-mesh_io_account:org1-cluster-id/org1-account" &&
						tuple.Relation == "bind_inherited" &&
						tuple.User == "apis_kcp_io_apiexport:provider-cluster-id/my-export"
				}), mock.Anything).Return(&openfgav1.WriteResponse{}, nil)

				// Expect FGA Write for org2 with bind_inherited relation
				fga.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
					if req.StoreId != "store-id-org2" {
						return false
					}
					if len(req.Writes.TupleKeys) != 1 {
						return false
					}
					tuple := req.Writes.TupleKeys[0]
					return tuple.Object == "core_platform-mesh_io_account:org2-cluster-id/org2-account" &&
						tuple.Relation == "bind_inherited" &&
						tuple.User == "apis_kcp_io_apiexport:provider-cluster-id/my-export"
				}), mock.Anything).Return(&openfgav1.WriteResponse{}, nil)

				kcpClientGetter.EXPECT().NewClientFromContext(mock.Anything).Return(clusterClient, nil)
			},
			cfg:         &config.Config{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			storeIDGetter := mocks.NewMockStoreIDGetter(t)
			lister := mocks.NewMockLister(t)
			kcpClientGetter := mocks.NewMockKCPClientGetter(t)

			if tt.setupMocks != nil {
				tt.setupMocks(t, fga, storeIDGetter, kcpClientGetter, lister)
			}

			l := testlogger.New()
			ctx := l.WithContext(context.Background())

			sub := subroutine.NewAPIExportPolicySubroutine(fga, tt.cfg, storeIDGetter, lister, kcpClientGetter)

			_, err := sub.Process(ctx, tt.policy)

			if tt.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestAPIExportPolicySubroutine_Finalize_Success(t *testing.T) {
	tests := []struct {
		name        string
		policy      *corev1alpha1.APIExportPolicy
		setupMocks  func(*testing.T, *mocks.MockOpenFGAServiceClient, *mocks.MockStoreIDGetter, *mocks.MockKCPClientGetter, *mocks.MockLister)
		cfg         *config.Config
		expectError bool
	}{
		{
			name: "should delete FGA tuple for single org expression",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()

				// Provider cluster mock
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)

				// Target workspace client with AccountInfo
				targetClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "account",
						},
						Spec: corev1alpha1.AccountInfoSpec{
							Account: corev1alpha1.AccountLocation{
								Name:            "acme-account",
								OriginClusterId: "acme-cluster-id",
								Type:            corev1alpha1.AccountTypeOrg,
							},
							Organization: corev1alpha1.AccountLocation{
								Name: "acme-org",
							},
						},
					}).
					Build()

				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil)

				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("test-store-id", nil)

				// Expect FGA Write with Deletes for tuple deletion
				fga.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
					if req.StoreId != "test-store-id" {
						return false
					}
					if req.Deletes == nil || len(req.Deletes.TupleKeys) != 1 {
						return false
					}
					tuple := req.Deletes.TupleKeys[0]
					return tuple.Object == "core_platform-mesh_io_account:acme-cluster-id/acme-account" &&
						tuple.Relation == "bind" &&
						tuple.User == "apis_kcp_io_apiexport:provider-cluster-id/my-export"
				}), mock.Anything).Return(&openfgav1.WriteResponse{}, nil)
			},
			cfg:         &config.Config{},
			expectError: false,
		},
		{
			name: "should delete FGA tuple with bind_inherited for wildcard expression",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:acme:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()

				// Provider cluster mock
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)

				// Target workspace client with AccountInfo
				targetClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "account",
						},
						Spec: corev1alpha1.AccountInfoSpec{
							Account: corev1alpha1.AccountLocation{
								Name:            "acme-account",
								OriginClusterId: "acme-cluster-id",
								Type:            corev1alpha1.AccountTypeOrg,
							},
							Organization: corev1alpha1.AccountLocation{
								Name: "acme-org",
							},
						},
					}).
					Build()

				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil)

				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("test-store-id", nil)

				// Expect FGA Write with Deletes using bind_inherited relation
				fga.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
					if req.StoreId != "test-store-id" {
						return false
					}
					if req.Deletes == nil || len(req.Deletes.TupleKeys) != 1 {
						return false
					}
					tuple := req.Deletes.TupleKeys[0]
					return tuple.Object == "core_platform-mesh_io_account:acme-cluster-id/acme-account" &&
						tuple.Relation == "bind_inherited" &&
						tuple.User == "apis_kcp_io_apiexport:provider-cluster-id/my-export"
				}), mock.Anything).Return(&openfgav1.WriteResponse{}, nil)
			},
			cfg:         &config.Config{},
			expectError: false,
		},
		{
			name: "should delete FGA tuples for all orgs when root:orgs:* expression is used",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-policy",
				},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef: corev1alpha1.APIExportRef{
						Name:        "my-export",
						ClusterPath: "root:providers:my-provider",
					},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {

				scheme := getAPIExportPolicyTestScheme()

				// Provider cluster mock
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)

				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					if list, ok := ol.(*corev1alpha1.AccountInfoList); ok {
						list.Items = []corev1alpha1.AccountInfo{
							{
								ObjectMeta: metav1.ObjectMeta{Name: "account-1"},
								Spec: corev1alpha1.AccountInfoSpec{
									Account:      corev1alpha1.AccountLocation{Name: "org1-account", OriginClusterId: "org1-cluster-id", Type: corev1alpha1.AccountTypeOrg},
									Organization: corev1alpha1.AccountLocation{Name: "org1"},
								},
							},
							{
								ObjectMeta: metav1.ObjectMeta{Name: "account-2"},
								Spec: corev1alpha1.AccountInfoSpec{
									Account:      corev1alpha1.AccountLocation{Name: "org2-account", OriginClusterId: "org2-cluster-id", Type: corev1alpha1.AccountTypeOrg},
									Organization: corev1alpha1.AccountLocation{Name: "org2"},
								},
							},
						}
					}
					return nil
				})

				storeIDGetter.EXPECT().Get(mock.Anything, "org1").Return("store-id-org1", nil)
				storeIDGetter.EXPECT().Get(mock.Anything, "org2").Return("store-id-org2", nil)

				// Expect FGA Write with Deletes for org1 with bind_inherited relation
				fga.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
					if req.StoreId != "store-id-org1" {
						return false
					}
					if req.Deletes == nil || len(req.Deletes.TupleKeys) != 1 {
						return false
					}
					tuple := req.Deletes.TupleKeys[0]
					return tuple.Object == "core_platform-mesh_io_account:org1-cluster-id/org1-account" &&
						tuple.Relation == "bind_inherited" &&
						tuple.User == "apis_kcp_io_apiexport:provider-cluster-id/my-export"
				}), mock.Anything).Return(&openfgav1.WriteResponse{}, nil)

				// Expect FGA Write with Deletes for org2 with bind_inherited relation
				fga.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
					if req.StoreId != "store-id-org2" {
						return false
					}
					if req.Deletes == nil || len(req.Deletes.TupleKeys) != 1 {
						return false
					}
					tuple := req.Deletes.TupleKeys[0]
					return tuple.Object == "core_platform-mesh_io_account:org2-cluster-id/org2-account" &&
						tuple.Relation == "bind_inherited" &&
						tuple.User == "apis_kcp_io_apiexport:provider-cluster-id/my-export"
				}), mock.Anything).Return(&openfgav1.WriteResponse{}, nil)
			},
			cfg:         &config.Config{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			storeIDGetter := mocks.NewMockStoreIDGetter(t)
			lister := mocks.NewMockLister(t)
			kcpClientGetter := mocks.NewMockKCPClientGetter(t)

			if tt.setupMocks != nil {
				tt.setupMocks(t, fga, storeIDGetter, kcpClientGetter, lister)
			}

			l := testlogger.New()
			ctx := l.WithContext(context.Background())

			sub := subroutine.NewAPIExportPolicySubroutine(fga, tt.cfg, storeIDGetter, lister, kcpClientGetter)

			_, err := sub.Finalize(ctx, tt.policy)

			if tt.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func newProviderClient(scheme *runtime.Scheme) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&kcpcorev1alpha1.LogicalCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "cluster",
				Annotations: map[string]string{"kcp.io/cluster": "provider-cluster-id"},
			},
		}).
		Build()
}

func TestAPIExportPolicySubroutine_Process_AdditionalErrorPaths(t *testing.T) {
	tests := []struct {
		name        string
		policy      *corev1alpha1.APIExportPolicy
		setupMocks  func(*testing.T, *mocks.MockOpenFGAServiceClient, *mocks.MockStoreIDGetter, *mocks.MockKCPClientGetter, *mocks.MockLister)
		cfg         *config.Config
		expectError bool
	}{
		{
			name: "getClusterIDFromPath: Get LogicalCluster fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				providerClient := mocks.NewMockClient(t)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				providerClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "getClusterIDFromPath: kcp.io/cluster annotation missing",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				noAnnotationClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&kcpcorev1alpha1.LogicalCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}).
					Build()
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(noAnnotationClient, nil)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "deleteRemovedExpressions: internal getClusterIDFromPath fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "deleteRemovedExpressions: removed expression triggers delete failure",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "test-policy"},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:other"},
				},
				Status: corev1alpha1.APIExportPolicyStatus{
					// "root:orgs:other" is still in spec → continue; "root:orgs:acme" is removed → delete
					ManagedAllowExpressions: []string{"root:orgs:other", "root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(nil, assert.AnError).Once()
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "orgs: List AccountInfo fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				lister.EXPECT().List(mock.Anything, mock.Anything).Return(assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "orgs: non-org type skipped, storeIDGetter fails for org account",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ol client.ObjectList, _ ...client.ListOption) error {
					list := ol.(*corev1alpha1.AccountInfoList)
					list.Items = []corev1alpha1.AccountInfo{
						{Spec: corev1alpha1.AccountInfoSpec{
							Account: corev1alpha1.AccountLocation{Type: corev1alpha1.AccountTypeAccount},
						}},
						{Spec: corev1alpha1.AccountInfoSpec{
							Account:      corev1alpha1.AccountLocation{Type: corev1alpha1.AccountTypeOrg},
							Organization: corev1alpha1.AccountLocation{Name: "org1"},
						}},
					}
					return nil
				})
				storeIDGetter.EXPECT().Get(mock.Anything, "org1").Return("", assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "orgs: fga.Write fails when applying tuple",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ol client.ObjectList, _ ...client.ListOption) error {
					list := ol.(*corev1alpha1.AccountInfoList)
					list.Items = []corev1alpha1.AccountInfo{
						{Spec: corev1alpha1.AccountInfoSpec{
							Account:      corev1alpha1.AccountLocation{Type: corev1alpha1.AccountTypeOrg, Name: "org1"},
							Organization: corev1alpha1.AccountLocation{Name: "org1"},
						}},
					}
					return nil
				})
				storeIDGetter.EXPECT().Get(mock.Anything, "org1").Return("store-id", nil)
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "non-orgs: NewForLogicalCluster fails for workspace",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(nil, assert.AnError).Once()
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "non-orgs: Get AccountInfo fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				targetClient := mocks.NewMockClient(t)
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil).Once()
				targetClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "non-orgs: storeIDGetter fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				targetClient := fake.NewClientBuilder().WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{Name: "account"},
						Spec:       corev1alpha1.AccountInfoSpec{Organization: corev1alpha1.AccountLocation{Name: "acme-org"}},
					}).Build()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil).Once()
				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("", assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "non-orgs: fga.Write fails when applying tuple",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				targetClient := fake.NewClientBuilder().WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{Name: "account"},
						Spec:       corev1alpha1.AccountInfoSpec{Organization: corev1alpha1.AccountLocation{Name: "acme-org"}},
					}).Build()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil).Once()
				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("store-id", nil)
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "ClusterFromContext fails",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "test-policy"},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				targetClient := fake.NewClientBuilder().WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{Name: "account"},
						Spec:       corev1alpha1.AccountInfoSpec{Organization: corev1alpha1.AccountLocation{Name: "acme-org"}},
					}).Build()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil).Once()
				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("store-id", nil)
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil)
				kcpClientGetter.EXPECT().NewClientFromContext(mock.Anything).Return(nil, assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "Status Patch fails",
			policy: &corev1alpha1.APIExportPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "test-policy"},
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				targetClient := fake.NewClientBuilder().WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{Name: "account"},
						Spec:       corev1alpha1.AccountInfoSpec{Organization: corev1alpha1.AccountLocation{Name: "acme-org"}},
					}).Build()
				// Empty cluster client — Status().Patch will fail with NotFound for "test-policy"
				clusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()
				providerClient := newProviderClient(scheme)
				// Mock provider cluster ID lookup
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil).Once()
				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("store-id", nil)
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil)
				kcpClientGetter.EXPECT().NewClientFromContext(mock.Anything).Return(clusterClient, nil)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			storeIDGetter := mocks.NewMockStoreIDGetter(t)
			lister := mocks.NewMockLister(t)
			kcpClientGetter := mocks.NewMockKCPClientGetter(t)

			if tt.setupMocks != nil {
				tt.setupMocks(t, fga, storeIDGetter, kcpClientGetter, lister)
			}

			l := testlogger.New()
			ctx := l.WithContext(context.Background())

			sub := subroutine.NewAPIExportPolicySubroutine(fga, tt.cfg, storeIDGetter, lister, kcpClientGetter)

			_, err := sub.Process(ctx, tt.policy)

			if tt.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestAPIExportPolicySubroutine_Finalize_AdditionalErrorPaths(t *testing.T) {
	tests := []struct {
		name        string
		policy      *corev1alpha1.APIExportPolicy
		setupMocks  func(*testing.T, *mocks.MockOpenFGAServiceClient, *mocks.MockStoreIDGetter, *mocks.MockKCPClientGetter, *mocks.MockLister)
		cfg         *config.Config
		expectError bool
	}{
		{
			name: "deleteTuplesForExpression: parseAllowExpression fails for invalid expression",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"wrong:path:expression"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "deleteTuplesForExpression orgs: GetAllClient fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				lister.EXPECT().List(mock.Anything, mock.Anything).Return(assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "deleteTuplesForExpression orgs: List AccountInfo fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				lister.EXPECT().List(mock.Anything, mock.Anything).Return(assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "deleteTuplesForExpression orgs: storeIDGetter fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ol client.ObjectList, _ ...client.ListOption) error {
					list := ol.(*corev1alpha1.AccountInfoList)
					list.Items = []corev1alpha1.AccountInfo{
						{Spec: corev1alpha1.AccountInfoSpec{Organization: corev1alpha1.AccountLocation{Name: "org1"}}},
					}
					return nil
				})
				storeIDGetter.EXPECT().Get(mock.Anything, "org1").Return("", assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "deleteTuplesForExpression orgs: fga.Write (Delete) fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:*"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ol client.ObjectList, _ ...client.ListOption) error {
					list := ol.(*corev1alpha1.AccountInfoList)
					list.Items = []corev1alpha1.AccountInfo{
						{Spec: corev1alpha1.AccountInfoSpec{Organization: corev1alpha1.AccountLocation{Name: "org1"}}},
					}
					return nil
				})
				storeIDGetter.EXPECT().Get(mock.Anything, "org1").Return("store-id", nil)
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "deleteTuplesForExpression non-orgs: NewForLogicalCluster fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(nil, assert.AnError).Once()
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "deleteTuplesForExpression non-orgs: storeIDGetter fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				targetClient := fake.NewClientBuilder().WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{Name: "account"},
						Spec:       corev1alpha1.AccountInfoSpec{Organization: corev1alpha1.AccountLocation{Name: "acme-org"}},
					}).Build()
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil).Once()
				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("", assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
		{
			name: "deleteTuplesForExpression non-orgs: fga.Write (Delete) fails",
			policy: &corev1alpha1.APIExportPolicy{
				Spec: corev1alpha1.APIExportPolicySpec{
					APIExportRef:         corev1alpha1.APIExportRef{Name: "my-export", ClusterPath: "root:providers:my-provider"},
					AllowPathExpressions: []string{"root:orgs:acme"},
				},
			},
			setupMocks: func(t *testing.T, fga *mocks.MockOpenFGAServiceClient, storeIDGetter *mocks.MockStoreIDGetter, kcpClientGetter *mocks.MockKCPClientGetter, lister *mocks.MockLister) {
				scheme := getAPIExportPolicyTestScheme()
				providerClient := newProviderClient(scheme)
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:providers:my-provider"))).Return(providerClient, nil)
				targetClient := fake.NewClientBuilder().WithScheme(scheme).
					WithObjects(&corev1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{Name: "account"},
						Spec:       corev1alpha1.AccountInfoSpec{Organization: corev1alpha1.AccountLocation{Name: "acme-org"}},
					}).Build()
				kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, string(config.MultiProviderName(config.CoreProviderName, "root:orgs:acme"))).Return(targetClient, nil).Once()
				storeIDGetter.EXPECT().Get(mock.Anything, "acme-org").Return("store-id", nil)
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, assert.AnError)
			},
			cfg:         &config.Config{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			storeIDGetter := mocks.NewMockStoreIDGetter(t)
			lister := mocks.NewMockLister(t)
			kcpClientGetter := mocks.NewMockKCPClientGetter(t)

			if tt.setupMocks != nil {
				tt.setupMocks(t, fga, storeIDGetter, kcpClientGetter, lister)
			}

			l := testlogger.New()
			ctx := l.WithContext(context.Background())

			sub := subroutine.NewAPIExportPolicySubroutine(fga, tt.cfg, storeIDGetter, lister, kcpClientGetter)

			_, err := sub.Finalize(ctx, tt.policy)

			if tt.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
