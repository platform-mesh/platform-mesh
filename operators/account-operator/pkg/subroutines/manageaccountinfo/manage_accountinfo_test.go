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

package manageaccountinfo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"go.platform-mesh.io/account-operator/pkg/subroutines/manageaccountinfo"
	"go.platform-mesh.io/account-operator/pkg/subroutines/mocks"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

var _ multicluster.Provider = &Provider{}

// Provider is a provider that only embeds clusters.Clusters.
type Provider struct {
	clusters map[string]cluster.Cluster
}

// Get implements multicluster.Provider.
func (p *Provider) Get(_ context.Context, clusterName multicluster.ClusterName) (cluster.Cluster, error) {
	cluster, ok := p.clusters[string(clusterName)]
	if !ok {
		return nil, fmt.Errorf("cluster not found: %s", clusterName)
	}
	return cluster, nil
}

// IndexField implements multicluster.Provider.
func (p *Provider) IndexField(_ context.Context, _ ctrlruntimeclient.Object, _ string, _ ctrlruntimeclient.IndexerFunc) error {
	return nil
}

func TestManageAccountInfoGetName(t *testing.T) {
	assert.Equal(t, manageaccountinfo.ManageAccountInfoSubroutineName, (&manageaccountinfo.ManageAccountInfoSubroutine{}).GetName())
}

func TestManageAccountInfoProcess(t *testing.T) {
	accountObj := func(tp pmcorev1alpha1.AccountType) *pmcorev1alpha1.Account {
		return &pmcorev1alpha1.Account{
			ObjectMeta: metav1.ObjectMeta{Name: "acc"},
			Spec:       pmcorev1alpha1.AccountSpec{Type: tp},
		}
	}

	testCases := []struct {
		name          string
		obj           *pmcorev1alpha1.Account
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
						RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
							ws := obj.(*kcptenancyv1alpha1.Workspace)

							*ws = kcptenancyv1alpha1.Workspace{
								Spec: kcptenancyv1alpha1.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:test",
									Cluster: "account-cluster-id",
								},
								Status: kcptenancyv1alpha1.WorkspaceStatus{
									Phase: kcpcorev1alpha1.LogicalClusterPhaseReady,
								},
							}

							return nil
						}).Once()

					cl.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
							pa := obj.(*pmcorev1alpha1.AccountInfo)

							*pa = pmcorev1alpha1.AccountInfo{
								Spec: pmcorev1alpha1.AccountInfoSpec{
									FGA: pmcorev1alpha1.FGAInfo{
										Store: pmcorev1alpha1.StoreInfo{
											Id: "fga-store-id",
										},
									},
									Account: pmcorev1alpha1.AccountLocation{
										Name: "parent-account",
									},
									Organization: pmcorev1alpha1.AccountLocation{
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
						Return(apierrors.NewNotFound(schema.GroupResource{}, "not found"))

					cl.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).Return(nil)

					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-account",
				},
				Spec: pmcorev1alpha1.AccountSpec{
					Type: "account",
				},
			},
		},
		{
			name:        "current cluster get fails",
			clusters:    map[string]cluster.Cluster{}, // context set, but cluster not registered
			obj:         accountObj(pmcorev1alpha1.AccountTypeAccount),
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
			obj:         accountObj(pmcorev1alpha1.AccountTypeAccount),
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
						RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
							ws := obj.(*kcptenancyv1alpha1.Workspace)
							*ws = kcptenancyv1alpha1.Workspace{
								Spec: kcptenancyv1alpha1.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:test",
									Cluster: "acc-cluster-id",
								},
								Status: kcptenancyv1alpha1.WorkspaceStatus{
									Phase: "Scheduling",
								},
							}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:           accountObj(pmcorev1alpha1.AccountTypeAccount),
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
						RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
							ws := obj.(*kcptenancyv1alpha1.Workspace)
							*ws = kcptenancyv1alpha1.Workspace{
								Spec: kcptenancyv1alpha1.WorkspaceSpec{
									URL:     "",
									Cluster: "acc-cluster-id",
								},
								Status: kcptenancyv1alpha1.WorkspaceStatus{
									Phase: kcpcorev1alpha1.LogicalClusterPhaseReady,
								},
							}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:         accountObj(pmcorev1alpha1.AccountTypeAccount),
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
						RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
							ws := obj.(*kcptenancyv1alpha1.Workspace)
							*ws = kcptenancyv1alpha1.Workspace{
								Spec: kcptenancyv1alpha1.WorkspaceSpec{
									URL:     "a",
									Cluster: "acc-cluster-id",
								},
								Status: kcptenancyv1alpha1.WorkspaceStatus{
									Phase: kcpcorev1alpha1.LogicalClusterPhaseReady,
								},
							}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:         accountObj(pmcorev1alpha1.AccountTypeAccount),
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
						RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
							ws := obj.(*kcptenancyv1alpha1.Workspace)
							*ws = kcptenancyv1alpha1.Workspace{
								Spec: kcptenancyv1alpha1.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:child",
									Cluster: "missing-account-cluster",
								},
								Status: kcptenancyv1alpha1.WorkspaceStatus{
									Phase: kcpcorev1alpha1.LogicalClusterPhaseReady,
								},
							}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj:         accountObj(pmcorev1alpha1.AccountTypeAccount),
			expectError: true,
		},
		{
			name: "parent account info not found requeues",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)

					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
							ws := obj.(*kcptenancyv1alpha1.Workspace)
							*ws = kcptenancyv1alpha1.Workspace{
								Spec: kcptenancyv1alpha1.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:child",
									Cluster: "child-cluster-id",
								},
								Status: kcptenancyv1alpha1.WorkspaceStatus{
									Phase: kcpcorev1alpha1.LogicalClusterPhaseReady,
								},
							}
							return nil
						}).Once()

					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						Return(apierrors.NewNotFound(schema.GroupResource{}, "account")).Once()
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
			obj:           accountObj(pmcorev1alpha1.AccountTypeAccount),
			expectRequeue: true,
		},
		{
			name: "org account success",
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().
						Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
							ws := obj.(*kcptenancyv1alpha1.Workspace)
							*ws = kcptenancyv1alpha1.Workspace{
								Spec: kcptenancyv1alpha1.WorkspaceSpec{
									URL:     "https://acme.corp/clusters/root:orgs:orgws",
									Cluster: "org-cluster-id",
								},
								Status: kcptenancyv1alpha1.WorkspaceStatus{
									Phase: kcpcorev1alpha1.LogicalClusterPhaseReady,
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
						Return(apierrors.NewNotFound(schema.GroupResource{}, "account")).Once()
					cl.EXPECT().
						Create(mock.Anything, mock.Anything, mock.Anything).
						Return(nil).Maybe()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			obj: accountObj(pmcorev1alpha1.AccountTypeOrg),
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			testProvider := &Provider{clusters: test.clusters}

			emptyConfig := &rest.Config{}
			mgr, err := mcmanager.New(emptyConfig, testProvider, mcmanager.Options{})
			assert.NoError(t, err)

			s, newErr := manageaccountinfo.New(mgr, "")
			assert.NoError(t, newErr)
			ctx := t.Context()

			log := testlogger.New()
			ctx = logger.SetLoggerInContext(ctx, log.Logger)

			if test.clusters != nil {
				ctx = mccontext.WithCluster(ctx, "test-cluster")
			}

			res, processErr := s.Process(ctx, test.obj)
			if test.expectError {
				assert.Error(t, processErr)
			} else {
				assert.NoError(t, processErr)
			}
			if test.expectRequeue {
				assert.NotZero(t, res.Requeue())
			} else {
				assert.Zero(t, res.Requeue())
			}
		})
	}
}
