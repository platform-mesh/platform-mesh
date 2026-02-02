package finalizeaccountinfo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/finalizeaccountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/platform-mesh/golang-commons/logger/testlogger"
)

var _ multicluster.Provider = &Provider{}

type Provider struct {
	clusters map[string]cluster.Cluster
}

func (p *Provider) Get(_ context.Context, clusterName string) (cluster.Cluster, error) {
	cluster, ok := p.clusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("cluster not found: %s", clusterName)
	}
	return cluster, nil
}

func (p *Provider) IndexField(_ context.Context, _ client.Object, _ string, _ client.IndexerFunc) error {
	return nil
}

func TestFinalizeAccountInfoGetName(t *testing.T) {
	s := finalizeaccountinfo.New(nil)
	assert.Equal(t, finalizeaccountinfo.FinalizeAccountInfoSubroutineName, s.GetName())
}

func TestFinalizeAccountInfoFinalizers(t *testing.T) {
	s := finalizeaccountinfo.New(nil)
	assert.Equal(t, []string{finalizeaccountinfo.AccountInfoFinalizer}, s.Finalizers(nil))
}

func TestFinalizeAccountInfoProcess(t *testing.T) {
	s := finalizeaccountinfo.New(nil)
	result, err := s.Process(t.Context(), nil)
	assert.Nil(t, err)
	assert.Zero(t, result.RequeueAfter)
}

func TestFinalizeAccountInfoFinalize(t *testing.T) {
	testCases := []struct {
		name          string
		obj           runtimeobject.RuntimeObject
		clusters      map[string]cluster.Cluster
		expectError   bool
		expectRequeue bool
	}{
		{
			name: "should requeue if child accounts exist",
			obj: &v1alpha1.AccountInfo{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "account",
					Finalizers: []string{finalizeaccountinfo.AccountInfoFinalizer},
				},
			},
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
							accountList := list.(*v1alpha1.AccountList)
							accountList.Items = []v1alpha1.Account{{}}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			expectRequeue: true,
		},
		{
			name: "should complete finalization when no accounts and only our finalizer",
			obj: &v1alpha1.AccountInfo{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "account",
					Finalizers: []string{finalizeaccountinfo.AccountInfoFinalizer},
				},
			},
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).
						RunAndReturn(func(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
							accountList := list.(*v1alpha1.AccountList)
							accountList.Items = []v1alpha1.Account{}
							return nil
						}).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			expectRequeue: false,
		},
		{
			name: "should error if listing accounts fails",
			obj: &v1alpha1.AccountInfo{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "account",
					Finalizers: []string{finalizeaccountinfo.AccountInfoFinalizer},
				},
			},
			clusters: map[string]cluster.Cluster{
				"test-cluster": func() cluster.Cluster {
					c := mocks.NewCluster(t)
					cl := mocks.NewClient(t)
					cl.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).
						Return(fmt.Errorf("boom")).Once()
					c.EXPECT().GetClient().Return(cl)
					return c
				}(),
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testProvider := &Provider{clusters: tc.clusters}
			emptyConfig := &rest.Config{}
			mgr, err := mcmanager.New(emptyConfig, testProvider, mcmanager.Options{})
			assert.NoError(t, err)

			s := finalizeaccountinfo.New(mgr)
			ctx := t.Context()
			log := testlogger.New()
			ctx = logger.SetLoggerInContext(ctx, log.Logger)
			ctx = mccontext.WithCluster(ctx, "test-cluster")

			result, processErr := s.Finalize(ctx, tc.obj)
			if tc.expectError {
				assert.Error(t, processErr.Err())
			} else {
				assert.Nil(t, processErr)
			}
			if tc.expectRequeue {
				assert.True(t, result.RequeueAfter > 0)
			}
		})
	}
}
