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

package contextual_test

import (
	"context"
	"slices"
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/clustercache"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/contextual"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/mocks"
	"google.golang.org/grpc"

	v1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

func TestHandler(t *testing.T) {
	testCases := []struct {
		name                  string
		req                   authorization.Request
		res                   authorization.Response
		fgaMocks              func(openfga *mocks.OpenFGAServiceClient)
		clusterCacheMocks     func(cc *mocks.ClusterCacheProvider)
		cacheMissTrackerMocks func(tracker *mocks.Tracker[string])
	}{
		{
			name: "should skip processing if clusterKey extra attrs not present",
			req:  authorization.Request{},
			res:  authorization.NoOpinion(),
		},
		{
			name: "should retry processing if cluster not found in cache and cacheMissTracker returns true",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{},
					},
				},
			},
			res: authorization.Retry(time.Second),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				cc.EXPECT().Get(multicluster.ClusterName("a")).Return(clustercache.ClusterInfo{}, false)
			},
			cacheMissTrackerMocks: func(tracker *mocks.Tracker[string]) {
				tracker.EXPECT().ShouldRetry("a").Return(true)
				tracker.EXPECT().Retried("a")
			},
		},
		{
			name: "should skip processing if cluster not found in cache and cacheMissTracker returns false",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{},
					},
				},
			},
			res: authorization.NoOpinion(),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				cc.EXPECT().Get(multicluster.ClusterName("a")).Return(clustercache.ClusterInfo{}, false)
			},
		},
		{
			name: "should skip processing if restmapper cannot resolve GVK",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "unknown.io",
							Version:  "v1",
							Resource: "unknowns",
						},
					},
				},
			},
			res: authorization.NoOpinion(),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
				cc.EXPECT().Get(multicluster.ClusterName("a")).Return(clustercache.ClusterInfo{
					StoreID:         "store-id",
					RESTMapper:      rm,
					AccountName:     "origin-account",
					ParentClusterID: "origin",
				}, true)
			},
		},
		{
			name: "should process request non-parent, non-namespaced successfully",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "test.platform-mesh.io",
							Version:  "v1alpha1",
							Resource: "tests",
							Verb:     "get",
							Name:     "test-sample",
						},
					},
				},
			},
			res: authorization.Allowed(),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{})

				gv := schema.GroupVersion{
					Group:   "test.platform-mesh.io",
					Version: "v1alpha1",
				}

				rm.AddSpecific(
					gv.WithKind("Test"),
					gv.WithResource("tests"),
					gv.WithResource("test"),
					meta.RESTScopeRoot,
				)

				cc.EXPECT().Get(multicluster.ClusterName("a")).Return(clustercache.ClusterInfo{
					StoreID:         "store-id",
					RESTMapper:      rm,
					AccountName:     "origin-account",
					ParentClusterID: "origin",
				}, true)
			},
			fgaMocks: func(openfga *mocks.OpenFGAServiceClient) {
				openfga.EXPECT().Check(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, in *openfgav1.CheckRequest, opts ...grpc.CallOption) (*openfgav1.CheckResponse, error) {

						tuples := in.ContextualTuples.TupleKeys

						contains := slices.ContainsFunc(tuples, func(tk *openfgav1.TupleKey) bool {
							return tk.Object == "test_platform-mesh_io_test:a/test-sample" &&
								tk.Relation == "parent" &&
								tk.User == "core_platform-mesh_io_account:origin/origin-account"
						})

						assert.True(t, contains)

						assert.Equal(t, "store-id", in.StoreId)
						assert.Equal(t, "test_platform-mesh_io_test:a/test-sample", in.TupleKey.Object)
						assert.Equal(t, "get", in.TupleKey.Relation)

						return &openfgav1.CheckResponse{
							Allowed: true,
						}, nil
					},
				)
			},
		},
		{
			name: "should process request non-parent, namespaced successfully",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:     "test.platform-mesh.io",
							Version:   "v1alpha1",
							Resource:  "tests",
							Verb:      "get",
							Name:      "test-sample",
							Namespace: "test-ns",
						},
					},
				},
			},
			res: authorization.Allowed(),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{})

				gv := schema.GroupVersion{
					Group:   "test.platform-mesh.io",
					Version: "v1alpha1",
				}

				rm.AddSpecific(
					gv.WithKind("Test"),
					gv.WithResource("tests"),
					gv.WithResource("test"),
					meta.RESTScopeNamespace,
				)

				cc.EXPECT().Get(multicluster.ClusterName("a")).Return(clustercache.ClusterInfo{
					StoreID:         "store-id",
					RESTMapper:      rm,
					AccountName:     "origin-account",
					ParentClusterID: "origin",
				}, true)
			},
			fgaMocks: func(openfga *mocks.OpenFGAServiceClient) {
				openfga.EXPECT().Check(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, in *openfgav1.CheckRequest, opts ...grpc.CallOption) (*openfgav1.CheckResponse, error) {

						tuples := in.ContextualTuples.TupleKeys

						contains := slices.ContainsFunc(tuples, func(tk *openfgav1.TupleKey) bool {
							return tk.Object == "core_namespace:a/test-ns" &&
								tk.Relation == "parent" &&
								tk.User == "core_platform-mesh_io_account:origin/origin-account"
						})

						assert.True(t, contains)

						contains = slices.ContainsFunc(tuples, func(tk *openfgav1.TupleKey) bool {
							return tk.User == "core_namespace:a/test-ns" &&
								tk.Relation == "parent" &&
								tk.Object == "test_platform-mesh_io_test:a/test-sample"
						})

						assert.True(t, contains)

						assert.Equal(t, "store-id", in.StoreId)
						assert.Equal(t, "test_platform-mesh_io_test:a/test-sample", in.TupleKey.Object)
						assert.Equal(t, "get", in.TupleKey.Relation)

						return &openfgav1.CheckResponse{
							Allowed: true,
						}, nil
					},
				)
			},
		},
		{
			name: "should process request parent, namespaced successfully",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:     "test.platform-mesh.io",
							Version:   "v1alpha1",
							Resource:  "tests",
							Verb:      "list",
							Name:      "test-sample",
							Namespace: "test-ns",
						},
					},
				},
			},
			res: authorization.Allowed(),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{})

				gv := schema.GroupVersion{
					Group:   "test.platform-mesh.io",
					Version: "v1alpha1",
				}

				rm.AddSpecific(
					gv.WithKind("Test"),
					gv.WithResource("tests"),
					gv.WithResource("test"),
					meta.RESTScopeNamespace,
				)

				cc.EXPECT().Get(multicluster.ClusterName("a")).Return(clustercache.ClusterInfo{
					StoreID:         "store-id",
					RESTMapper:      rm,
					AccountName:     "origin-account",
					ParentClusterID: "origin",
				}, true)
			},
			fgaMocks: func(openfga *mocks.OpenFGAServiceClient) {
				openfga.EXPECT().Check(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, in *openfgav1.CheckRequest, opts ...grpc.CallOption) (*openfgav1.CheckResponse, error) {

						tuples := in.ContextualTuples.TupleKeys

						contains := slices.ContainsFunc(tuples, func(tk *openfgav1.TupleKey) bool {
							return tk.Object == "core_namespace:a/test-ns" &&
								tk.Relation == "parent" &&
								tk.User == "core_platform-mesh_io_account:origin/origin-account"
						})

						assert.True(t, contains)

						assert.Equal(t, "store-id", in.StoreId)
						assert.Equal(t, "core_namespace:a/test-ns", in.TupleKey.Object)
						assert.Equal(t, "list_test_platform-mesh_io_tests", in.TupleKey.Relation)

						return &openfgav1.CheckResponse{
							Allowed: true,
						}, nil
					},
				)
			},
		},
		{
			name: "should process request parent, non-namespaced successfully",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "test.platform-mesh.io",
							Version:  "v1alpha1",
							Resource: "tests",
							Verb:     "list",
							Name:     "test-sample",
						},
					},
				},
			},
			res: authorization.Allowed(),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{})

				gv := schema.GroupVersion{
					Group:   "test.platform-mesh.io",
					Version: "v1alpha1",
				}

				rm.AddSpecific(
					gv.WithKind("Test"),
					gv.WithResource("tests"),
					gv.WithResource("test"),
					meta.RESTScopeRoot,
				)

				cc.EXPECT().Get(multicluster.ClusterName("a")).Return(clustercache.ClusterInfo{
					StoreID:         "store-id",
					RESTMapper:      rm,
					AccountName:     "origin-account",
					ParentClusterID: "origin",
				}, true)
			},
			fgaMocks: func(openfga *mocks.OpenFGAServiceClient) {
				openfga.EXPECT().Check(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, in *openfgav1.CheckRequest, opts ...grpc.CallOption) (*openfgav1.CheckResponse, error) {

						tuples := in.ContextualTuples.TupleKeys

						contains := slices.ContainsFunc(tuples, func(tk *openfgav1.TupleKey) bool {
							return tk.Object == "test_platform-mesh_io_test:a/test-sample" &&
								tk.Relation == "parent" &&
								tk.User == "core_platform-mesh_io_account:origin/origin-account"
						})

						assert.True(t, contains)

						assert.Equal(t, "store-id", in.StoreId)
						assert.Equal(t, "core_platform-mesh_io_account:origin/origin-account", in.TupleKey.Object)
						assert.Equal(t, "list_test_platform-mesh_io_tests", in.TupleKey.Relation)

						return &openfgav1.CheckResponse{
							Allowed: true,
						}, nil
					},
				)
			},
		},
		{
			name: "should process bind verb authorization successfully",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						User: "system:anonymous",
						Groups: []string{
							"system:authenticated",
							"system:cluster:consumer-cluster-id",
						},
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"provider-cluster-id"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "apis.kcp.io",
							Version:  "v1alpha1",
							Resource: "apiexports",
							Verb:     "bind",
							Name:     "test-export",
						},
					},
				},
			},
			res: authorization.Allowed(),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				consumerRM := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
				consumerGV := schema.GroupVersion{
					Group:   "apis.kcp.io",
					Version: "v1alpha1",
				}
				consumerRM.AddSpecific(
					consumerGV.WithKind("APIExport"),
					consumerGV.WithResource("apiexports"),
					consumerGV.WithResource("apiexport"),
					meta.RESTScopeRoot,
				)

				cc.EXPECT().Get(multicluster.ClusterName("consumer-cluster-id")).Return(clustercache.ClusterInfo{
					StoreID:         "consumer-store-id",
					RESTMapper:      consumerRM,
					AccountName:     "consumer-account",
					ParentClusterID: "consumer-parent",
				}, true)
			},
			fgaMocks: func(openfga *mocks.OpenFGAServiceClient) {
				openfga.EXPECT().Check(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, in *openfgav1.CheckRequest, opts ...grpc.CallOption) (*openfgav1.CheckResponse, error) {
						assert.Equal(t, "consumer-store-id", in.StoreId)
						assert.Equal(t, "core_platform-mesh_io_account:consumer-parent/consumer-account", in.TupleKey.Object)
						assert.Equal(t, "bind", in.TupleKey.Relation)
						assert.Equal(t, "apis_kcp_io_apiexport:provider-cluster-id/test-export", in.TupleKey.User)
						assert.Nil(t, in.ContextualTuples)

						return &openfgav1.CheckResponse{
							Allowed: true,
						}, nil
					},
				)
			},
		},
		{
			name: "should return no opinion if bind verb and consumer cluster not found",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						User: "system:anonymous",
						Groups: []string{
							"system:authenticated",
							"system:cluster:consumer-cluster-id",
						},
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"provider-cluster-id"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "apis.kcp.io",
							Version:  "v1alpha1",
							Resource: "apiexports",
							Verb:     "bind",
							Name:     "test-export",
						},
					},
				},
			},
			res: authorization.NoOpinion(),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				cc.EXPECT().Get(multicluster.ClusterName("consumer-cluster-id")).Return(clustercache.ClusterInfo{}, false)
			},
		},
		{
			name: "should return no opinion if bind verb and consumer cluster not in groups",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						User: "system:anonymous",
						Groups: []string{
							"system:authenticated",
						},
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"provider-cluster-id"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "apis.kcp.io",
							Version:  "v1alpha1",
							Resource: "apiexports",
							Verb:     "bind",
							Name:     "test-export",
						},
					},
				},
			},
			res: authorization.NoOpinion(),
		},
		{
			name: "should return no opinion if bind verb and provider cluster not in extra",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						User: "system:anonymous",
						Groups: []string{
							"system:authenticated",
							"system:cluster:consumer-cluster-id",
						},
						Extra: map[string]v1.ExtraValue{},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "apis.kcp.io",
							Version:  "v1alpha1",
							Resource: "apiexports",
							Verb:     "bind",
							Name:     "test-export",
						},
					},
				},
			},
			res: authorization.NoOpinion(),
		},
		{
			name: "should retry if bind verb and consumer cluster not found in cache and cacheMissTracker returns true",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						User: "system:anonymous",
						Groups: []string{
							"system:authenticated",
							"system:cluster:consumer-cluster-id",
						},
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"provider-cluster-id"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "apis.kcp.io",
							Version:  "v1alpha1",
							Resource: "apiexports",
							Verb:     "bind",
							Name:     "test-export",
						},
					},
				},
			},
			res: authorization.Retry(time.Second),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				cc.EXPECT().Get(multicluster.ClusterName("consumer-cluster-id")).Return(clustercache.ClusterInfo{}, false)
			},
			cacheMissTrackerMocks: func(tracker *mocks.Tracker[string]) {
				tracker.EXPECT().ShouldRetry("consumer-cluster-id").Return(true)
				tracker.EXPECT().Retried("consumer-cluster-id")
			},
		},
		{
			name: "should not process bind verb if group is not apis.kcp.io",
			req: authorization.Request{
				SubjectAccessReview: v1.SubjectAccessReview{
					Spec: v1.SubjectAccessReviewSpec{
						User: "system:anonymous",
						Groups: []string{
							"system:authenticated",
							"system:cluster:consumer-cluster-id",
						},
						Extra: map[string]v1.ExtraValue{
							"authorization.kubernetes.io/cluster-name": {"a"},
						},
						ResourceAttributes: &v1.ResourceAttributes{
							Group:    "other.io",
							Version:  "v1",
							Resource: "tests",
							Verb:     "bind",
							Name:     "test-sample",
						},
					},
				},
			},
			res: authorization.Allowed(),
			clusterCacheMocks: func(cc *mocks.ClusterCacheProvider) {
				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{})

				gv := schema.GroupVersion{
					Group:   "other.io",
					Version: "v1",
				}

				rm.AddSpecific(
					gv.WithKind("Test"),
					gv.WithResource("tests"),
					gv.WithResource("test"),
					meta.RESTScopeRoot,
				)

				cc.EXPECT().Get(multicluster.ClusterName("a")).Return(clustercache.ClusterInfo{
					StoreID:         "store-id",
					RESTMapper:      rm,
					AccountName:     "origin-account",
					ParentClusterID: "origin",
				}, true)
			},
			fgaMocks: func(openfga *mocks.OpenFGAServiceClient) {
				openfga.EXPECT().Check(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, in *openfgav1.CheckRequest, opts ...grpc.CallOption) (*openfgav1.CheckResponse, error) {
						// Should process as regular authorization, not bind
						assert.Equal(t, "store-id", in.StoreId)
						assert.Equal(t, "other_io_test:a/test-sample", in.TupleKey.Object)
						assert.Equal(t, "bind", in.TupleKey.Relation)

						return &openfgav1.CheckResponse{
							Allowed: true,
						}, nil
					},
				)
			},
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			cc := mocks.NewClusterCacheProvider(t)
			if test.clusterCacheMocks != nil {
				test.clusterCacheMocks(cc)
			}

			openfga := mocks.NewOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(openfga)
			}

			cacheMissTracker := mocks.NewTracker[string](t)
			if test.cacheMissTrackerMocks != nil {
				test.cacheMissTrackerMocks(cacheMissTracker)
			} else {
				// Default: ShouldRetry returns false so handler proceeds when cluster is in cache
				cacheMissTracker.EXPECT().ShouldRetry(mock.Anything).Return(false).Maybe()
			}

			h := contextual.New(openfga, cc, "authorization.kubernetes.io/cluster-name", cacheMissTracker, time.Second)

			ctx := t.Context()

			res := h.Handle(ctx, test.req)

			assert.Equal(t, test.res, res)
		})
	}
}
