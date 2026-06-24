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

package clustercache_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/clustercache"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/mocks"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

func TestNew(t *testing.T) {
	mgr := mocks.NewManager(t)

	cc, err := clustercache.New(mgr)
	assert.NoError(t, err)
	assert.NotNil(t, cc)
}

func TestClusterCache_Engage(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		ownerCluster    string
		setupOrgsClient func(*mocks.Client)
		setupCluster    func(*mocks.Cluster)
		lcGetErr        error
		wantCached      bool
		wantAccountName string
		wantErr         bool
	}{
		{
			name:         "caches cluster info successfully",
			path:         "root:orgs:myorg:ws:child",
			ownerCluster: "parent-cluster-id",
			setupOrgsClient: func(c *mocks.Client) {
				setupStoreGet(c, "myorg", "myorg-store-id")
			},
			setupCluster:    func(c *mocks.Cluster) { c.EXPECT().GetConfig().Return(&rest.Config{Host: "https://example.com"}) },
			wantCached:      true,
			wantAccountName: "child",
		},
		{
			name:    "returns error when owner missing",
			path:    "root:orgs:myorg",
			wantErr: true,
		},
		{
			name:       "skips non-org path",
			path:       "root:platform-mesh-system",
			wantCached: false,
		},
		{
			name:     "returns error on logical cluster get failure",
			path:     "root:orgs:myorg",
			lcGetErr: errors.New("connection refused"),
			wantErr:  true,
		},
		{
			name:         "returns error when storeId missing",
			path:         "root:orgs:myorg",
			ownerCluster: "parent-cluster",
			setupOrgsClient: func(c *mocks.Client) {
				setupStoreGetMissing(c, "myorg")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := mocks.NewCluster(t)
			k8sClient := mocks.NewClient(t)
			mgr := mocks.NewManager(t)
			orgsCluster := mocks.NewCluster(t)
			orgsClient := mocks.NewClient(t)

			cl.EXPECT().GetClient().Return(k8sClient)

			ctx := t.Context()
			if tt.lcGetErr != nil {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			k8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything, mock.Anything).
				Run(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
					lc := obj.(*unstructured.Unstructured)
					lc.SetAnnotations(map[string]string{"kcp.io/path": tt.path})
					if tt.ownerCluster != "" {
						lc.Object["spec"] = map[string]any{
							"owner": map[string]any{"cluster": tt.ownerCluster},
						}
					}
				}).
				Return(tt.lcGetErr)

			if tt.setupOrgsClient != nil {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).Return(orgsCluster, nil)
				orgsCluster.EXPECT().GetClient().Return(orgsClient)
				tt.setupOrgsClient(orgsClient)
			}
			if tt.setupCluster != nil {
				tt.setupCluster(cl)
			}

			cc, err := clustercache.New(mgr)
			assert.NoError(t, err)
			err = cc.Engage(ctx, multicluster.ClusterName("test-cluster"), cl)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			info, found := cc.Get(multicluster.ClusterName("test-cluster"))
			assert.Equal(t, tt.wantCached, found)
			if tt.wantCached {
				assert.Equal(t, tt.wantAccountName, info.AccountName)
				assert.Equal(t, tt.ownerCluster, info.ParentClusterID)
				assert.NotNil(t, info.RESTMapper)
			}
		})
	}
}

func TestClusterCache_Get_NotFound(t *testing.T) {
	mgr := mocks.NewManager(t)
	cc, err := clustercache.New(mgr)
	assert.NoError(t, err)
	info, found := cc.Get(multicluster.ClusterName("non-existing"))
	assert.False(t, found)
	assert.Empty(t, info.StoreID)
}

func setupStoreGet(c *mocks.Client, orgName, storeID string) {
	c.EXPECT().Get(mock.Anything, types.NamespacedName{Name: orgName}, mock.Anything, mock.Anything).
		Run(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
			obj.(*unstructured.Unstructured).Object = map[string]any{
				"status": map[string]any{"storeId": storeID},
			}
		}).
		Return(nil)
}

func setupStoreGetMissing(c *mocks.Client, orgName string) {
	c.EXPECT().Get(mock.Anything, types.NamespacedName{Name: orgName}, mock.Anything, mock.Anything).
		Run(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
			obj.(*unstructured.Unstructured).Object = map[string]any{
				"status": map[string]any{},
			}
		}).
		Return(nil)
}
