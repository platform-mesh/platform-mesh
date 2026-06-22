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

	"go.platform-mesh.io/account-operator/pkg/subroutines/finalizeaccountinfo"
	"go.platform-mesh.io/account-operator/pkg/subroutines/mocks"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
)

var _ multicluster.Provider = &Provider{}

type Provider struct {
	clusters map[string]cluster.Cluster
}

func (p *Provider) Get(_ context.Context, clusterName multicluster.ClusterName) (cluster.Cluster, error) {
	cluster, ok := p.clusters[string(clusterName)]
	if !ok {
		return nil, fmt.Errorf("cluster not found: %s", clusterName)
	}
	return cluster, nil
}

func (p *Provider) IndexField(_ context.Context, _ client.Object, _ string, _ client.IndexerFunc) error {
	return nil
}

func TestFinalizeAccountInfoGetName(t *testing.T) {
	s, err := finalizeaccountinfo.New(nil)
	assert.NoError(t, err)
	assert.Equal(t, finalizeaccountinfo.FinalizeAccountInfoSubroutineName, s.GetName())
}

func TestFinalizeAccountInfoFinalizers(t *testing.T) {
	s, err := finalizeaccountinfo.New(nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{finalizeaccountinfo.AccountInfoFinalizer}, s.Finalizers(nil))
}

func TestFinalizeAccountInfoFinalize(t *testing.T) {
	testCases := []struct {
		name          string
		obj           *corev1alpha1.AccountInfo
		clusters      map[string]cluster.Cluster
		expectError   bool
		expectRequeue bool
	}{
		{
			name: "should requeue if child accounts exist",
			obj: &corev1alpha1.AccountInfo{
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
							accountList := list.(*corev1alpha1.AccountList)
							accountList.Items = []corev1alpha1.Account{{}}
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
			obj: &corev1alpha1.AccountInfo{
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
							accountList := list.(*corev1alpha1.AccountList)
							accountList.Items = []corev1alpha1.Account{}
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
			obj: &corev1alpha1.AccountInfo{
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

			s, err := finalizeaccountinfo.New(mgr)
			assert.NoError(t, err)
			ctx := t.Context()
			log := testlogger.New()
			ctx = logger.SetLoggerInContext(ctx, log.Logger)
			ctx = mccontext.WithCluster(ctx, "test-cluster")

			result, processErr := s.Finalize(ctx, tc.obj)
			if tc.expectError {
				assert.Error(t, processErr)
			} else {
				assert.NoError(t, processErr)
			}
			if tc.expectRequeue {
				assert.True(t, result.Requeue() > 0)
			}
		})
	}
}
