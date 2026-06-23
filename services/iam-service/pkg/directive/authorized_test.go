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

package directive

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	accountsv1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/jwt"
	"go.platform-mesh.io/golang-commons/logger"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	accountinfomocks "go.platform-mesh.io/iam-service/pkg/accountinfo/mocks"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	fgamocks "go.platform-mesh.io/iam-service/pkg/fga/mocks"
	"go.platform-mesh.io/iam-service/pkg/graph"
)

type mockWSClient struct {
	client client.Client
}

func (m *mockWSClient) New(_ context.Context, _ multicluster.ClusterName) (client.Client, error) {
	return m.client, nil
}

func createTestWebToken() jwt.WebToken {
	return jwt.WebToken{
		ParsedAttributes: jwt.ParsedAttributes{
			Mail: "test@example.com",
		},
	}
}

func createTestAccountInfo() *accountsv1alpha1.AccountInfo {
	return &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Organization: accountsv1alpha1.AccountLocation{
				Name: "test-org",
			},
			Account: accountsv1alpha1.AccountLocation{
				GeneratedClusterId: "generated-cluster-456",
				OriginClusterId:    "origin-cluster-123",
			},
		},
	}
}

func createTestResourceContext() *graph.ResourceContext {
	return &graph.ResourceContext{
		Group:       "apps",
		Kind:        "Deployment",
		AccountPath: "root:orgs:test",
		Resource: &graph.Resource{
			Name:      "test-deployment",
			Namespace: ptr.To("test-namespace"),
		},
	}
}

func setupTestContext() (context.Context, *logger.Logger) {
	ctx := context.Background()
	log, _ := logger.New(logger.DefaultConfig())
	ctx = logger.SetLoggerInContext(ctx, log)
	return ctx, log
}

func setupFakeClient(t *testing.T, objects ...client.Object) client.Client {
	rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{accountsv1alpha1.GroupVersion})
	rm.Add(schema.GroupVersionKind{
		Group:   accountsv1alpha1.GroupVersion.Group,
		Version: accountsv1alpha1.GroupVersion.Version,
		Kind:    "AccountInfo",
	}, meta.RESTScopeNamespace)

	scheme := runtime.NewScheme()
	require.NoError(t, accountsv1alpha1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRESTMapper(rm)

	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
	}

	return builder.Build()
}

func TestAuthorized_HappyPath(t *testing.T) {
	ctx, log := setupTestContext()

	// Setup mocks
	fgaClient := fgamocks.NewOpenFGAServiceClient(t)
	accountInfoRetriever := accountinfomocks.NewRetriever(t)

	// Setup expectations
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   "store-123",
				Name: "test-org",
			},
		},
	}
	fgaClient.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

	checkResponse := &openfgav1.CheckResponse{
		Allowed: true,
	}
	fgaClient.EXPECT().Check(mock.Anything, mock.Anything).Return(checkResponse, nil)

	ai := createTestAccountInfo()
	accountInfoRetriever.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(ai, nil)

	// Setup fake workspace client
	fakeClient := setupFakeClient(t, ai)
	wsClient := &mockWSClient{client: fakeClient}

	// Create directive
	directive := NewAuthorizedDirective(fgaClient, accountInfoRetriever, 5*time.Minute, wsClient, log)

	// Setup context with WebToken
	token := createTestWebToken()
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, token)

	// Setup KCP context
	kcpCtx := appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	}
	ctx = appcontext.SetKCPContext(ctx, kcpCtx)

	// Setup GraphQL field context
	fieldCtx := &graphql.FieldContext{
		Args: map[string]any{
			"context": map[string]any{
				"group":       "core.platform-mesh.io",
				"kind":        "AccountInfo",
				"accountPath": "root:orgs:test",
				"resource": map[string]any{
					"name": "account",
				},
			},
		},
	}
	ctx = graphql.WithFieldContext(ctx, fieldCtx)

	// Mock next resolver
	nextCalled := false
	next := func(ctx context.Context) (any, error) {
		nextCalled = true
		// Verify cluster ID was set in context
		clusterId, err := appcontext.GetClusterId(ctx)
		assert.NoError(t, err)
		assert.NotEmpty(t, clusterId)
		return "success", nil
	}

	// Execute test
	result, err := directive.Authorized(ctx, nil, next, "read")

	// Verify results
	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.True(t, nextCalled)
}

func TestAuthorized_ErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		setupContext  func() context.Context
		setupMocks    func(*fgamocks.OpenFGAServiceClient, *accountinfomocks.Retriever)
		expectedError string
	}{
		{
			name: "missing web token",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				return ctx
			},
			setupMocks:    func(*fgamocks.OpenFGAServiceClient, *accountinfomocks.Retriever) {},
			expectedError: "failed to get web token from context",
		},
		{
			name: "missing KCP context",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				token := createTestWebToken()
				ctx = context.WithValue(ctx, keys.WebTokenCtxKey, token)
				return ctx
			},
			setupMocks:    func(*fgamocks.OpenFGAServiceClient, *accountinfomocks.Retriever) {},
			expectedError: "failed to get kcp user context",
		},
		{
			name: "invalid GraphQL field context",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				token := createTestWebToken()
				ctx = context.WithValue(ctx, keys.WebTokenCtxKey, token)

				kcpCtx := appcontext.KCPContext{
					IDMTenant:        "test-tenant",
					OrganizationName: "test-org",
				}
				ctx = appcontext.SetKCPContext(ctx, kcpCtx)

				fieldCtx := &graphql.FieldContext{
					Args: map[string]any{
						"invalid": "context",
					},
				}
				ctx = graphql.WithFieldContext(ctx, fieldCtx)
				return ctx
			},
			setupMocks:    func(*fgamocks.OpenFGAServiceClient, *accountinfomocks.Retriever) {},
			expectedError: "unable to extract param from request",
		},
		{
			name: "account info retrieval error",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				token := createTestWebToken()
				ctx = context.WithValue(ctx, keys.WebTokenCtxKey, token)

				kcpCtx := appcontext.KCPContext{
					IDMTenant:        "test-tenant",
					OrganizationName: "test-org",
				}
				ctx = appcontext.SetKCPContext(ctx, kcpCtx)

				fieldCtx := &graphql.FieldContext{
					Args: map[string]any{
						"context": map[string]any{
							"group":       "apps",
							"kind":        "Deployment",
							"accountPath": "root:orgs:test",
							"resource": map[string]any{
								"name":      "test-deployment",
								"namespace": "test-namespace",
							},
						},
					},
				}
				ctx = graphql.WithFieldContext(ctx, fieldCtx)
				return ctx
			},
			setupMocks: func(fgaClient *fgamocks.OpenFGAServiceClient, air *accountinfomocks.Retriever) {
				air.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(nil, fmt.Errorf("account not found"))
			},
			expectedError: "failed to get account info from kcp context",
		},
		{
			name: "organization mismatch",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				token := createTestWebToken()
				ctx = context.WithValue(ctx, keys.WebTokenCtxKey, token)

				kcpCtx := appcontext.KCPContext{
					IDMTenant:        "test-tenant",
					OrganizationName: "different-org", // Mismatch
				}
				ctx = appcontext.SetKCPContext(ctx, kcpCtx)

				fieldCtx := &graphql.FieldContext{
					Args: map[string]any{
						"context": map[string]any{
							"group":       "apps",
							"kind":        "Deployment",
							"accountPath": "root:orgs:test",
							"resource": map[string]any{
								"name":      "test-deployment",
								"namespace": "test-namespace",
							},
						},
					},
				}
				ctx = graphql.WithFieldContext(ctx, fieldCtx)
				return ctx
			},
			setupMocks: func(fgaClient *fgamocks.OpenFGAServiceClient, air *accountinfomocks.Retriever) {
				ai := createTestAccountInfo()
				air.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(ai, nil)
			},
			expectedError: "unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, log := setupTestContext()

			// Setup mocks
			fgaClient := fgamocks.NewOpenFGAServiceClient(t)
			accountInfoRetriever := accountinfomocks.NewRetriever(t)
			tt.setupMocks(fgaClient, accountInfoRetriever)

			// Setup workspace client
			fakeClient := setupFakeClient(t)
			wsClient := &mockWSClient{client: fakeClient}

			// Create directive
			directive := NewAuthorizedDirective(fgaClient, accountInfoRetriever, 5*time.Minute, wsClient, log)

			// Setup context
			ctx := tt.setupContext()

			// Mock next resolver
			next := func(ctx context.Context) (any, error) {
				return "success", nil
			}

			// Execute test
			result, err := directive.Authorized(ctx, nil, next, "read")

			// Verify results
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestAuthorized_ResourceNotExists(t *testing.T) {
	ctx, log := setupTestContext()

	// Setup mocks
	fgaClient := fgamocks.NewOpenFGAServiceClient(t)
	accountInfoRetriever := accountinfomocks.NewRetriever(t)

	ai := createTestAccountInfo()
	accountInfoRetriever.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(ai, nil)

	// Setup fake workspace client without the resource
	fakeClient := setupFakeClient(t) // Empty client, resource doesn't exist
	wsClient := &mockWSClient{client: fakeClient}

	// Create directive
	directive := NewAuthorizedDirective(fgaClient, accountInfoRetriever, 5*time.Minute, wsClient, log)

	// Setup context
	token := createTestWebToken()
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, token)

	kcpCtx := appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	}
	ctx = appcontext.SetKCPContext(ctx, kcpCtx)

	fieldCtx := &graphql.FieldContext{
		Args: map[string]any{
			"context": map[string]any{
				"group":       "core.platform-mesh.io",
				"kind":        "AccountInfo",
				"accountPath": "root:orgs:test",
				"resource": map[string]any{
					"name": "nonexistent-resource",
				},
			},
		},
	}
	ctx = graphql.WithFieldContext(ctx, fieldCtx)

	next := func(ctx context.Context) (any, error) {
		return "success", nil
	}

	// Execute test
	result, err := directive.Authorized(ctx, nil, next, "read")

	// Verify results
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "resource does not exist")
}

func TestAuthorized_NotAllowed(t *testing.T) {
	ctx, log := setupTestContext()

	// Setup mocks
	fgaClient := fgamocks.NewOpenFGAServiceClient(t)
	accountInfoRetriever := accountinfomocks.NewRetriever(t)

	// Setup expectations
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   "store-123",
				Name: "test-org",
			},
		},
	}
	fgaClient.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

	checkResponse := &openfgav1.CheckResponse{
		Allowed: false, // Not allowed
	}
	fgaClient.EXPECT().Check(mock.Anything, mock.Anything).Return(checkResponse, nil)

	ai := createTestAccountInfo()
	accountInfoRetriever.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(ai, nil)

	// Setup fake workspace client with resource
	fakeClient := setupFakeClient(t, ai)
	wsClient := &mockWSClient{client: fakeClient}

	// Create directive
	directive := NewAuthorizedDirective(fgaClient, accountInfoRetriever, 5*time.Minute, wsClient, log)

	// Setup context
	token := createTestWebToken()
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, token)

	kcpCtx := appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	}
	ctx = appcontext.SetKCPContext(ctx, kcpCtx)

	fieldCtx := &graphql.FieldContext{
		Args: map[string]any{
			"context": map[string]any{
				"group":       "core.platform-mesh.io",
				"kind":        "AccountInfo",
				"accountPath": "root:orgs:test",
				"resource": map[string]any{
					"name": "account",
				},
			},
		},
	}
	ctx = graphql.WithFieldContext(ctx, fieldCtx)

	next := func(ctx context.Context) (any, error) {
		return "success", nil
	}

	// Execute test
	result, err := directive.Authorized(ctx, nil, next, "read")

	// Verify results
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unauthorized")
}

func TestExtractResourceContextFromArguments(t *testing.T) {
	tests := []struct {
		name          string
		args          map[string]any
		expected      *graph.ResourceContext
		expectedError string
	}{
		{
			name: "valid context with namespace",
			args: map[string]any{
				"context": map[string]any{
					"group":       "apps",
					"kind":        "Deployment",
					"accountPath": "root:orgs:test",
					"resource": map[string]any{
						"name":      "test-deployment",
						"namespace": "test-namespace",
					},
				},
			},
			expected: &graph.ResourceContext{
				Group:       "apps",
				Kind:        "Deployment",
				AccountPath: "root:orgs:test",
				Resource: &graph.Resource{
					Name:      "test-deployment",
					Namespace: ptr.To("test-namespace"),
				},
			},
		},
		{
			name: "valid context without namespace",
			args: map[string]any{
				"context": map[string]any{
					"group":       "rbac.authorization.k8s.io",
					"kind":        "ClusterRole",
					"accountPath": "root:orgs:prod",
					"resource": map[string]any{
						"name": "cluster-admin",
					},
				},
			},
			expected: &graph.ResourceContext{
				Group:       "rbac.authorization.k8s.io",
				Kind:        "ClusterRole",
				AccountPath: "root:orgs:prod",
				Resource: &graph.Resource{
					Name:      "cluster-admin",
					Namespace: nil,
				},
			},
		},
		{
			name: "missing context parameter",
			args: map[string]any{
				"other": "value",
			},
			expectedError: "unable to extract param from request",
		},
		{
			name:          "empty args",
			args:          map[string]any{},
			expectedError: "unable to extract param from request",
		},
		{
			name: "invalid context structure",
			args: map[string]any{
				"context": "not-a-map",
			},
			expectedError: "failed to unmarshal param to ResourceContext",
		},
		{
			name: "context with extra fields",
			args: map[string]any{
				"context": map[string]any{
					"group":       "networking.istio.io",
					"kind":        "VirtualService",
					"accountPath": "root:orgs:staging",
					"resource": map[string]any{
						"name":      "my-virtual-service",
						"namespace": "istio-system",
					},
					"extraField": "ignored",
				},
				"otherParam": "should-be-ignored",
			},
			expected: &graph.ResourceContext{
				Group:       "networking.istio.io",
				Kind:        "VirtualService",
				AccountPath: "root:orgs:staging",
				Resource: &graph.Resource{
					Name:      "my-virtual-service",
					Namespace: ptr.To("istio-system"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractResourceContextFromArguments(tt.args)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.Group, result.Group)
				assert.Equal(t, tt.expected.Kind, result.Kind)
				assert.Equal(t, tt.expected.AccountPath, result.AccountPath)
				assert.Equal(t, tt.expected.Resource.Name, result.Resource.Name)
				if tt.expected.Resource.Namespace != nil {
					require.NotNil(t, result.Resource.Namespace)
					assert.Equal(t, *tt.expected.Resource.Namespace, *result.Resource.Namespace)
				} else {
					assert.Nil(t, result.Resource.Namespace)
				}
			}
		})
	}
}

func TestTestIfAllowed(t *testing.T) {
	tests := []struct {
		name           string
		setupMocks     func(*fgamocks.OpenFGAServiceClient)
		accountInfo    *accountsv1alpha1.AccountInfo
		resourceCtx    *graph.ResourceContext
		permission     string
		token          jwt.WebToken
		expectedResult bool
		expectedError  string
	}{
		{
			name: "allowed with namespace",
			setupMocks: func(fgaClient *fgamocks.OpenFGAServiceClient) {
				listStoresResponse := &openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Id:   "store-123",
							Name: "test-org",
						},
					},
				}
				fgaClient.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

				fgaClient.EXPECT().Check(mock.Anything, mock.MatchedBy(func(req *openfgav1.CheckRequest) bool {
					return req.StoreId == "store-123" &&
						req.TupleKey.Relation == "read" &&
						req.TupleKey.User == "user:test@example.com" &&
						req.TupleKey.Object == "apps_deployment:generated-cluster-456/test-namespace/test-deployment"
				})).Return(&openfgav1.CheckResponse{Allowed: true}, nil)
			},
			accountInfo:    createTestAccountInfo(),
			resourceCtx:    createTestResourceContext(),
			permission:     "read",
			token:          createTestWebToken(),
			expectedResult: true,
		},
		{
			name: "allowed without namespace (cluster-scoped)",
			setupMocks: func(fgaClient *fgamocks.OpenFGAServiceClient) {
				listStoresResponse := &openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Id:   "store-123",
							Name: "test-org",
						},
					},
				}
				fgaClient.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

				fgaClient.EXPECT().Check(mock.Anything, mock.MatchedBy(func(req *openfgav1.CheckRequest) bool {
					return req.StoreId == "store-123" &&
						req.TupleKey.Relation == "read" &&
						req.TupleKey.User == "user:test@example.com" &&
						req.TupleKey.Object == "rbac_authorization_k8s_io_clusterrole:generated-cluster-456/cluster-admin"
				})).Return(&openfgav1.CheckResponse{Allowed: true}, nil)
			},
			accountInfo: createTestAccountInfo(),
			resourceCtx: &graph.ResourceContext{
				Group:       "rbac.authorization.k8s.io",
				Kind:        "ClusterRole",
				AccountPath: "root:orgs:test",
				Resource: &graph.Resource{
					Name:      "cluster-admin",
					Namespace: nil,
				},
			},
			permission:     "read",
			token:          createTestWebToken(),
			expectedResult: true,
		},
		{
			name: "not allowed",
			setupMocks: func(fgaClient *fgamocks.OpenFGAServiceClient) {
				listStoresResponse := &openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Id:   "store-123",
							Name: "test-org",
						},
					},
				}
				fgaClient.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

				fgaClient.EXPECT().Check(mock.Anything, mock.Anything).Return(&openfgav1.CheckResponse{Allowed: false}, nil)
			},
			accountInfo:    createTestAccountInfo(),
			resourceCtx:    createTestResourceContext(),
			permission:     "write",
			token:          createTestWebToken(),
			expectedResult: false,
		},
		{
			name: "account resource uses origin cluster ID",
			setupMocks: func(fgaClient *fgamocks.OpenFGAServiceClient) {
				listStoresResponse := &openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Id:   "store-123",
							Name: "test-org",
						},
					},
				}
				fgaClient.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

				fgaClient.EXPECT().Check(mock.Anything, mock.MatchedBy(func(req *openfgav1.CheckRequest) bool {
					return req.StoreId == "store-123" &&
						req.TupleKey.Relation == "read" &&
						req.TupleKey.User == "user:test@example.com" &&
						req.TupleKey.Object == "core_platform-mesh_io_account:origin-cluster-123/test-account"
				})).Return(&openfgav1.CheckResponse{Allowed: true}, nil)
			},
			accountInfo: createTestAccountInfo(),
			resourceCtx: &graph.ResourceContext{
				Group:       "core.platform-mesh.io",
				Kind:        "Account",
				AccountPath: "root:orgs:test",
				Resource: &graph.Resource{
					Name:      "test-account",
					Namespace: nil,
				},
			},
			permission:     "read",
			token:          createTestWebToken(),
			expectedResult: true,
		},
		{
			name: "store helper error",
			setupMocks: func(fgaClient *fgamocks.OpenFGAServiceClient) {
				fgaClient.EXPECT().ListStores(mock.Anything, mock.Anything).Return(nil, fmt.Errorf("store not found"))
			},
			accountInfo:   createTestAccountInfo(),
			resourceCtx:   createTestResourceContext(),
			permission:    "read",
			token:         createTestWebToken(),
			expectedError: "failed to get store ID",
		},
		{
			name: "FGA check error",
			setupMocks: func(fgaClient *fgamocks.OpenFGAServiceClient) {
				listStoresResponse := &openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Id:   "store-123",
							Name: "test-org",
						},
					},
				}
				fgaClient.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

				fgaClient.EXPECT().Check(mock.Anything, mock.Anything).Return(nil, fmt.Errorf("FGA check failed"))
			},
			accountInfo:   createTestAccountInfo(),
			resourceCtx:   createTestResourceContext(),
			permission:    "read",
			token:         createTestWebToken(),
			expectedError: "failed to check permission with openfga",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, log := setupTestContext()

			// Setup mocks
			fgaClient := fgamocks.NewOpenFGAServiceClient(t)
			accountInfoRetriever := accountinfomocks.NewRetriever(t)
			tt.setupMocks(fgaClient)

			// Setup workspace client (not used in testIfAllowed)
			fakeClient := setupFakeClient(t)
			wsClient := &mockWSClient{client: fakeClient}

			// Create directive
			directive := NewAuthorizedDirective(fgaClient, accountInfoRetriever, 5*time.Minute, wsClient, log)

			// Execute test
			result, err := directive.testIfAllowed(ctx, tt.accountInfo, tt.resourceCtx, tt.permission, tt.token)

			// Verify results
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.False(t, result)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResult, result)
			}
		})
	}
}

func TestTestIfResourceExists(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(t *testing.T) client.Client
		resourceCtx    *graph.ResourceContext
		expectedResult bool
		expectedError  string
	}{
		{
			name: "resource exists - namespaced",
			setupClient: func(t *testing.T) client.Client {
				ai := createTestAccountInfo()
				ai.SetNamespace("test-namespace") // Make it namespaced
				return setupFakeClient(t, ai)
			},
			resourceCtx: &graph.ResourceContext{
				Group: "core.platform-mesh.io",
				Kind:  "AccountInfo",
				Resource: &graph.Resource{
					Name:      "account",
					Namespace: ptr.To("test-namespace"),
				},
			},
			expectedResult: true,
		},
		{
			name: "resource exists - cluster scoped",
			setupClient: func(t *testing.T) client.Client {
				// Setup with cluster-scoped REST mapping
				ai := createTestAccountInfo()

				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{accountsv1alpha1.GroupVersion})
				rm.Add(schema.GroupVersionKind{
					Group:   accountsv1alpha1.GroupVersion.Group,
					Version: accountsv1alpha1.GroupVersion.Version,
					Kind:    "AccountInfo",
				}, meta.RESTScopeRoot) // Cluster-scoped

				scheme := runtime.NewScheme()
				require.NoError(t, accountsv1alpha1.AddToScheme(scheme))

				return fake.NewClientBuilder().
					WithRESTMapper(rm).
					WithScheme(scheme).
					WithObjects(ai).
					Build()
			},
			resourceCtx: &graph.ResourceContext{
				Group: "core.platform-mesh.io",
				Kind:  "AccountInfo",
				Resource: &graph.Resource{
					Name:      "account",
					Namespace: nil, // Cluster-scoped
				},
			},
			expectedResult: true,
		},
		{
			name: "resource not found - namespaced",
			setupClient: func(t *testing.T) client.Client {
				return setupFakeClient(t) // Empty client
			},
			resourceCtx: &graph.ResourceContext{
				Group: "core.platform-mesh.io",
				Kind:  "AccountInfo",
				Resource: &graph.Resource{
					Name:      "nonexistent-account",
					Namespace: ptr.To("test-namespace"),
				},
			},
			expectedResult: false,
		},
		{
			name: "resource not found - cluster scoped",
			setupClient: func(t *testing.T) client.Client {
				// Setup with cluster-scoped REST mapping but no objects
				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{accountsv1alpha1.GroupVersion})
				rm.Add(schema.GroupVersionKind{
					Group:   accountsv1alpha1.GroupVersion.Group,
					Version: accountsv1alpha1.GroupVersion.Version,
					Kind:    "AccountInfo",
				}, meta.RESTScopeRoot)

				scheme := runtime.NewScheme()
				require.NoError(t, accountsv1alpha1.AddToScheme(scheme))

				return fake.NewClientBuilder().
					WithRESTMapper(rm).
					WithScheme(scheme).
					Build() // No objects
			},
			resourceCtx: &graph.ResourceContext{
				Group: "core.platform-mesh.io",
				Kind:  "AccountInfo",
				Resource: &graph.Resource{
					Name:      "nonexistent-account",
					Namespace: nil,
				},
			},
			expectedResult: false,
		},
		{
			name: "invalid resource kind - REST mapping fails",
			setupClient: func(t *testing.T) client.Client {
				scheme := runtime.NewScheme()
				return fake.NewClientBuilder().WithScheme(scheme).Build()
			},
			resourceCtx: &graph.ResourceContext{
				Group: "nonexistent.api.group",
				Kind:  "InvalidResourceKind",
				Resource: &graph.Resource{
					Name:      "test-resource",
					Namespace: ptr.To("test-namespace"),
				},
			},
			expectedError: "failed to get GVR for resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, log := setupTestContext()

			// Setup client
			fakeClient := tt.setupClient(t)

			// Setup mocks (not used in testIfResourceExists)
			fgaClient := fgamocks.NewOpenFGAServiceClient(t)
			accountInfoRetriever := accountinfomocks.NewRetriever(t)
			wsClient := &mockWSClient{client: fakeClient}

			// Create directive
			directive := NewAuthorizedDirective(fgaClient, accountInfoRetriever, 5*time.Minute, wsClient, log)

			// Execute test
			result, err := directive.testIfResourceExists(ctx, tt.resourceCtx, fakeClient)

			// Verify results
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.False(t, result)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResult, result)
			}
		})
	}
}
