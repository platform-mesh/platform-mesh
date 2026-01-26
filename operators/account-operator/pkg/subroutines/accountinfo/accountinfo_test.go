package accountinfo_test

import (
	"context"
	"fmt"
	"testing"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/accountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/platform-mesh/golang-commons/logger/testlogger"
)

var _ multicluster.Provider = &Provider{}

// Provider is a provider that only embeds clusters.Clusters.
type Provider struct {
	clusters map[string]cluster.Cluster
}

// Get implements multicluster.Provider.
func (p *Provider) Get(ctx context.Context, clusterName string) (cluster.Cluster, error) {
	cluster, ok := p.clusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("cluster not found: %s", clusterName)
	}
	return cluster, nil
}

// IndexField implements multicluster.Provider.
func (p *Provider) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	return nil
}

func TestAccountInfoGetName(t *testing.T) {
	assert.Equal(t, accountinfo.AccountInfoSubroutineName, (&accountinfo.AccountInfoSubroutine{}).GetName())
}

func TestAccounInfoFinalizers(t *testing.T) {
	assert.Equal(t, []string{accountinfo.AccountInfoFinalizer}, (&accountinfo.AccountInfoSubroutine{}).Finalizers(nil))
}

func TestAccountInfoFinalize(t *testing.T) {
	testCases := []struct {
		name        string
		obj         runtimeobject.RuntimeObject
		expectError bool
		expectReque bool
	}{
		{
			name: "should requeue if there are other finalizers",
			obj: &v1alpha1.AccountInfo{
				ObjectMeta: metav1.ObjectMeta{
					Name: "account",
					Finalizers: []string{
						"some.other/finalizer",
						accountinfo.AccountInfoFinalizer,
					},
				},
			},
		},
		{
			name: "should not error if only finalizer is accountinfo finalizer",
			obj: &v1alpha1.AccountInfo{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "account",
					Finalizers: []string{accountinfo.AccountInfoFinalizer},
				},
			},
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			s := accountinfo.New(nil, "")

			ctx := mccontext.WithCluster(t.Context(), "test-cluster")

			result, err := s.Finalize(ctx, test.obj)
			if test.expectError {
				assert.Error(t, err.Err())
			} else {
				assert.Nil(t, err)
			}

			if test.expectReque {
				assert.True(t, result.RequeueAfter > 0)
			}
		})
	}
}

func TestAccountInfoProcess(t *testing.T) {
	accountObj := func(tp v1alpha1.AccountType) *v1alpha1.Account {
		return &v1alpha1.Account{
			ObjectMeta: metav1.ObjectMeta{Name: "acc"},
			Spec:       v1alpha1.AccountSpec{Type: tp},
		}
	}

	testCases := []struct {
		name          string
		obj           runtimeobject.RuntimeObject
		clusters      map[string]cluster.Cluster
		expectError   bool
		expectRequeue bool
	}{
		{
			name: "should successfully create accountinfo object",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)

					cl := mocks.NewClient(t)

					cl.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							ws := obj.(*kcptenancyv1alpha.Workspace)

							*ws = kcptenancyv1alpha.Workspace{
								Spec: kcptenancyv1alpha.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:test",
									Cluster: "account-cluster-id",
								},
								Status: kcptenancyv1alpha.WorkspaceStatus{
									Phase: kcpcorev1alpha.LogicalClusterPhaseReady,
								},
							}

							return nil
						}).Once()

					cl.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							pa := obj.(*v1alpha1.AccountInfo)

							*pa = v1alpha1.AccountInfo{
								Spec: v1alpha1.AccountInfoSpec{
									FGA: v1alpha1.FGAInfo{
										Store: v1alpha1.StoreInfo{
											Id: "fga-store-id",
										},
									},
									Account: v1alpha1.AccountLocation{
										Name: "parent-account",
									},
									Organization: v1alpha1.AccountLocation{
										Name: "org-account",
									},
								},
							}
							return nil
						}).Once()

					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
				"account-cluster-id": func() cluster.Cluster {
					c := mocks.NewCluster(t)

					cl := mocks.NewClient(t)

					cl.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						Return(kerrors.NewNotFound(schema.GroupResource{}, "not found"))

					cl.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).Return(nil)

					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-account",
				},
				Spec: v1alpha1.AccountSpec{
					Type: "account",
				},
			},
		},
		{
			name:        "current cluster get fails",
			clusters:    map[string]cluster.Cluster{}, // context set, but cluster not registered
			obj:         accountObj(v1alpha1.AccountTypeAccount),
			expectError: true,
		},
		{
			name: "workspace retrieval error",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						Return(fmt.Errorf("boom")).
						Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:         accountObj(v1alpha1.AccountTypeAccount),
			expectError: true,
		},
		{
			name: "workspace not ready requeues",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							ws := obj.(*kcptenancyv1alpha.Workspace)
							*ws = kcptenancyv1alpha.Workspace{
								Spec: kcptenancyv1alpha.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:test",
									Cluster: "acc-cluster-id",
								},
								Status: kcptenancyv1alpha.WorkspaceStatus{
									Phase: "Scheduling",
								},
							}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:           accountObj(v1alpha1.AccountTypeAccount),
			expectRequeue: true,
		},
		{
			name: "workspace URL empty",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							ws := obj.(*kcptenancyv1alpha.Workspace)
							*ws = kcptenancyv1alpha.Workspace{
								Spec: kcptenancyv1alpha.WorkspaceSpec{
									URL:     "",
									Cluster: "acc-cluster-id",
								},
								Status: kcptenancyv1alpha.WorkspaceStatus{
									Phase: kcpcorev1alpha.LogicalClusterPhaseReady,
								},
							}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:         accountObj(v1alpha1.AccountTypeAccount),
			expectError: true,
		},
		{
			name: "workspace URL invalid",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							ws := obj.(*kcptenancyv1alpha.Workspace)
							*ws = kcptenancyv1alpha.Workspace{
								Spec: kcptenancyv1alpha.WorkspaceSpec{
									URL:     "a",
									Cluster: "acc-cluster-id",
								},
								Status: kcptenancyv1alpha.WorkspaceStatus{
									Phase: kcpcorev1alpha.LogicalClusterPhaseReady,
								},
							}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:         accountObj(v1alpha1.AccountTypeAccount),
			expectError: true,
		},
		{
			name: "account cluster retrieval fails",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							ws := obj.(*kcptenancyv1alpha.Workspace)
							*ws = kcptenancyv1alpha.Workspace{
								Spec: kcptenancyv1alpha.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:child",
									Cluster: "missing-account-cluster",
								},
								Status: kcptenancyv1alpha.WorkspaceStatus{
									Phase: kcpcorev1alpha.LogicalClusterPhaseReady,
								},
							}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:         accountObj(v1alpha1.AccountTypeAccount),
			expectError: true,
		},
		{
			name: "parent account info not found",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)

					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							ws := obj.(*kcptenancyv1alpha.Workspace)
							*ws = kcptenancyv1alpha.Workspace{
								Spec: kcptenancyv1alpha.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:child",
									Cluster: "child-cluster-id",
								},
								Status: kcptenancyv1alpha.WorkspaceStatus{
									Phase: kcpcorev1alpha.LogicalClusterPhaseReady,
								},
							}
							return nil
						}).Once()

					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						Return(kerrors.NewNotFound(schema.GroupResource{}, "account")).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
				"child-cluster-id": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:         accountObj(v1alpha1.AccountTypeAccount),
			expectError: true,
		},
		{
			name: "org account success",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							ws := obj.(*kcptenancyv1alpha.Workspace)
							*ws = kcptenancyv1alpha.Workspace{
								Spec: kcptenancyv1alpha.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:orgws",
									Cluster: "org-cluster-id",
								},
								Status: kcptenancyv1alpha.WorkspaceStatus{
									Phase: kcpcorev1alpha.LogicalClusterPhaseReady,
								},
							}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
				"org-cluster-id": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						Return(kerrors.NewNotFound(schema.GroupResource{}, "account")).Once()
					cl.EXPECT().
						Create(mock.Anything, mock.Anything, mock.Anything).
						Return(nil).Maybe()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj: accountObj(v1alpha1.AccountTypeOrg),
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			testProvider := &Provider{clusters: test.clusters}

			emptyConfig := &rest.Config{}
			mgr, err := mcmanager.New(emptyConfig, testProvider, mcmanager.Options{})
			assert.NoError(t, err)

			s := accountinfo.New(mgr, "")
			ctx := t.Context()

			log := testlogger.New()
			ctx = logger.SetLoggerInContext(ctx, log.Logger)

			if test.clusters != nil {
				ctx = mccontext.WithCluster(ctx, "test-cluster")
			}

			_, processErr := s.Process(ctx, test.obj)
			if test.expectError {
				assert.Error(t, processErr.Err())
			} else {
				assert.Nil(t, processErr)
			}
		})
	}
}
