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

package orgs_test

import (
	"context"
	"errors"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/mocks"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/orgs"

	v1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func TestHandler(t *testing.T) {
	testCases := []struct {
		name              string
		req               authorization.Request
		res               authorization.Response
		fgaMocks          func(openfga *mocks.OpenFGAServiceClient)
		setupManagerMocks func(mgr *mocks.Manager, cluster *mocks.Cluster, orgsClient *mocks.Client)
	}{
		{
			name: "should skip processing if no extra attrs present",
			req:  authorization.Request{},
			res:  authorization.NoOpinion(),
		},
		{
			name: "should skip processing if clusterKey extra attrs not present",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"a": {"b"},
						},
					},
				},
			},
			res: authorization.NoOpinion(),
		},
		{
			name: "should skip processing if clusterKey does not match orgID",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"b"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "a",
							Version:  "b",
							Resource: "c",
						},
					},
				},
			},
			res: authorization.NoOpinion(),
		},
		{
			name: "should skip processing if request does not contain ResourceAttributes",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
					},
				},
			},
			res: authorization.NoOpinion(),
		},
		{
			name: "should skip processing if manager cannot get orgs cluster",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "a",
							Version:  "b",
							Resource: "c",
						},
					},
				},
			},
			res: authorization.NoOpinion(),
			setupManagerMocks: func(mgr *mocks.Manager, cluster *mocks.Cluster, orgsClient *mocks.Client) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).Return(nil, errors.New("cluster lookup failed"))
			},
		},
		{
			name: "should skip processing if logical cluster get fails",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "a",
							Version:  "b",
							Resource: "c",
						},
					},
				},
			},
			res: authorization.NoOpinion(),
			setupManagerMocks: func(mgr *mocks.Manager, cluster *mocks.Cluster, orgsClient *mocks.Client) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(orgsClient)
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything).Return(errors.New("get failed"))
			},
		},
		{
			name: "should skip processing if logical cluster annotation is missing",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "a",
							Version:  "b",
							Resource: "c",
						},
					},
				},
			},
			res: authorization.NoOpinion(),
			setupManagerMocks: func(mgr *mocks.Manager, cluster *mocks.Cluster, orgsClient *mocks.Client) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(orgsClient)
				orgsClient.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything).
					Run(func(ctx context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) {
						lc := obj.(*kcpcorev1alpha.LogicalCluster)
						lc.Annotations = map[string]string{}
					}).
					Return(nil)
			},
		},
		{
			name: "should allow if fga check allows",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "a",
							Version:  "b",
							Resource: "c",
						},
					},
				},
			},
			res: authorization.Allowed(),
			fgaMocks: func(openfga *mocks.OpenFGAServiceClient) {
				openfga.EXPECT().Check(mock.Anything, mock.Anything).
					Return(&openfgav1.CheckResponse{
						Allowed: true,
					}, nil)
			},
		},
		{
			name: "should abort if fga check denies",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "a",
							Version:  "b",
							Resource: "c",
						},
					},
				},
			},
			res: authorization.Aborted(),
			fgaMocks: func(openfga *mocks.OpenFGAServiceClient) {
				openfga.EXPECT().Check(mock.Anything, mock.Anything).
					Return(&openfgav1.CheckResponse{
						Allowed: false,
					}, nil)
			},
		},
		{
			name: "should skip processing if fga check returns an error",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "a",
							Version:  "b",
							Resource: "c",
						},
					},
				},
			},
			res: authorization.NoOpinion(),
			fgaMocks: func(openfga *mocks.OpenFGAServiceClient) {
				openfga.EXPECT().Check(mock.Anything, mock.Anything).Return(nil, errors.New("fga check failed"))
			},
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			openfga := mocks.NewOpenFGAServiceClient(t)
			mgr := mocks.NewManager(t)
			cluster := mocks.NewCluster(t)
			orgsClient := mocks.NewClient(t)

			if test.setupManagerMocks != nil {
				test.setupManagerMocks(mgr, cluster, orgsClient)
			} else {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).Return(cluster, nil).Maybe()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything).
					Run(func(ctx context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) {
						lc := obj.(*kcpcorev1alpha.LogicalCluster)
						lc.Annotations = map[string]string{"kcp.io/cluster": "a"}
					}).
					Return(nil).
					Maybe()
			}

			if test.fgaMocks != nil {
				test.fgaMocks(openfga)
			}

			h := orgs.New(openfga, mgr, "authorization.kubernetes.io/cluster-name", "b")

			ctx := t.Context()

			res := h.Handle(ctx, test.req)
			assert.Equal(t, test.res, res)

		})
	}
}
