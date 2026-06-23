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

package workspace

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	accountmocks "go.platform-mesh.io/account-operator/pkg/subroutines/mocks"
	"go.platform-mesh.io/golang-commons/logger"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// Provider is a test provider for the multicluster manager
type Provider struct {
	clusters map[multicluster.ClusterName]cluster.Cluster
}

func (p *Provider) Get(ctx context.Context, clusterName multicluster.ClusterName) (cluster.Cluster, error) {
	cluster, ok := p.clusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("cluster not found: %s", clusterName)
	}
	return cluster, nil
}

func (p *Provider) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	return nil
}

func TestNewClientFactory(t *testing.T) {
	emptyConfig := &rest.Config{
		Host: "https://test-host.example.com",
	}
	testProvider := &Provider{clusters: map[multicluster.ClusterName]cluster.Cluster{}}

	mgr, err := mcmanager.New(emptyConfig, testProvider, mcmanager.Options{})
	require.NoError(t, err)

	factory := NewClientFactory(mgr)

	assert.NotNil(t, factory)
	assert.Equal(t, mgr, factory.mgr)
}

func TestKCPClient_New_Success(t *testing.T) {
	tests := []struct {
		name        string
		accountPath string
	}{
		{
			name:        "valid account path",
			accountPath: "root:org:account",
		},
		{
			name:        "account path with special characters",
			accountPath: "root:my-org:my-account-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			mockCluster := accountmocks.NewCluster(t)
			mockCluster.On("GetClient").Return(fakeClient)

			testProvider := &Provider{
				clusters: map[multicluster.ClusterName]cluster.Cluster{
					multicluster.ClusterName(tt.accountPath): mockCluster,
				},
			}
			emptyConfig := &rest.Config{
				Host: "https://test-host.example.com",
			}

			mgr, err := mcmanager.New(emptyConfig, testProvider, mcmanager.Options{})
			require.NoError(t, err)

			log, err := logger.New(logger.Config{Level: "info"})
			require.NoError(t, err)
			ctx := logger.SetLoggerInContext(context.Background(), log)

			factory := NewClientFactory(mgr)

			result, err := factory.New(ctx, multicluster.ClusterName(tt.accountPath))

			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, fakeClient, result)
		})
	}
}

func TestKCPClient_New_Error(t *testing.T) {
	accountPath := "root:nonexistent:account"

	testProvider := &Provider{
		clusters: map[multicluster.ClusterName]cluster.Cluster{},
	}
	emptyConfig := &rest.Config{
		Host: "https://test-host.example.com",
	}

	mgr, err := mcmanager.New(emptyConfig, testProvider, mcmanager.Options{})
	require.NoError(t, err)

	log, err := logger.New(logger.Config{Level: "info"})
	require.NoError(t, err)
	ctx := logger.SetLoggerInContext(context.Background(), log)

	factory := NewClientFactory(mgr)

	result, err := factory.New(ctx, multicluster.ClusterName(accountPath))

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "cluster not found")
}

func TestKCPClient_New_MultipleClients(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient1 := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeClient2 := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockCluster1 := accountmocks.NewCluster(t)
	mockCluster1.On("GetClient").Return(fakeClient1)

	mockCluster2 := accountmocks.NewCluster(t)
	mockCluster2.On("GetClient").Return(fakeClient2)

	testProvider := &Provider{
		clusters: map[multicluster.ClusterName]cluster.Cluster{
			multicluster.ClusterName("root:org1:account1"): mockCluster1,
			multicluster.ClusterName("root:org2:account2"): mockCluster2,
		},
	}
	emptyConfig := &rest.Config{
		Host: "https://test-host.example.com",
	}

	mgr, err := mcmanager.New(emptyConfig, testProvider, mcmanager.Options{})
	require.NoError(t, err)

	log, err := logger.New(logger.Config{Level: "info"})
	require.NoError(t, err)
	ctx := logger.SetLoggerInContext(context.Background(), log)

	factory := NewClientFactory(mgr)

	client1, err := factory.New(ctx, "root:org1:account1")
	require.NoError(t, err)
	require.NotNil(t, client1)

	client2, err := factory.New(ctx, "root:org2:account2")
	require.NoError(t, err)
	require.NotNil(t, client2)

	// Verify that both clients were created successfully and are different
	assert.NotEqual(t, client1, client2)
	assert.Equal(t, fakeClient1, client1)
	assert.Equal(t, fakeClient2, client2)
}
