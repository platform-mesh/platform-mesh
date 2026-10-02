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

	"github.com/99designs/gqlgen/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/jwt"
	"go.platform-mesh.io/golang-commons/logger"
	accountinfomocks "go.platform-mesh.io/iam-service/pkg/accountinfo/mocks"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	"go.platform-mesh.io/iam-service/pkg/graph"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

const testAuthHeader = "Bearer test-token"

type mockWSClient struct {
	client ctrlruntimeclient.Client
}

func (m *mockWSClient) New(_ context.Context, _ multicluster.ClusterName) (ctrlruntimeclient.Client, error) {
	return m.client, nil
}

// recordingReviewer is a fake selfSubjectAccessReviewer that records the
// arguments it was called with and returns a canned decision/error.
type recordingReviewer struct {
	allowed bool
	err     error

	calls         int
	gotWorkspace  string
	gotAuthHeader string
	gotAttrs      *authorizationv1.ResourceAttributes
}

func (r *recordingReviewer) review(_ context.Context, workspacePath, authHeader string, attrs *authorizationv1.ResourceAttributes) (bool, error) {
	r.calls++
	r.gotWorkspace = workspacePath
	r.gotAuthHeader = authHeader
	r.gotAttrs = attrs
	return r.allowed, r.err
}

func createTestWebToken() jwt.WebToken {
	return jwt.WebToken{
		ParsedAttributes: jwt.ParsedAttributes{
			Mail: "test@example.com",
		},
	}
}

func createTestAccountInfo() *pmcorev1alpha1.AccountInfo {
	return &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Organization: pmcorev1alpha1.AccountLocation{
				Name: "test-org",
			},
			Account: pmcorev1alpha1.AccountLocation{
				GeneratedClusterId: "generated-cluster-456",
				OriginClusterId:    "origin-cluster-123",
			},
		},
	}
}

func createTestResourceContext() *graph.ResourceContext {
	return &graph.ResourceContext{
		Group:       pmcorev1alpha1.GroupName,
		Kind:        "AccountInfo",
		AccountPath: "root:orgs:test",
		Resource: &graph.Resource{
			Name:      "account",
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

// withAuth adds the web token and auth header that the directive requires.
func withAuth(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, createTestWebToken())
	ctx = context.WithValue(ctx, keys.AuthHeaderCtxKey, testAuthHeader)
	return ctx
}

func setupFakeClient(t *testing.T, objects ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{pmcorev1alpha1.SchemeGroupVersion})
	rm.Add(pmcorev1alpha1.SchemeGroupVersion.WithKind("AccountInfo"), meta.RESTScopeNamespace)

	scheme := runtime.NewScheme()
	require.NoError(t, pmcorev1alpha1.AddToScheme(scheme))

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

	accountInfoRetriever := accountinfomocks.NewRetriever(t)

	ai := createTestAccountInfo()
	accountInfoRetriever.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(ai, nil)

	// Setup fake workspace client
	fakeClient := setupFakeClient(t, ai)
	wsClient := &mockWSClient{client: fakeClient}

	reviewer := &recordingReviewer{allowed: true}

	// Create directive
	directive := NewAuthorizedDirectiveWithReviewer(accountInfoRetriever, wsClient, reviewer.review, log)

	// Setup context with WebToken + auth header
	ctx = withAuth(ctx)

	// Setup kcp context
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
	result, err := directive.Authorized(ctx, nil, next, "get_iam_roles")

	// Verify results
	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.True(t, nextCalled)

	// Verify the SSAR was issued against the account's own workspace with the
	// caller's auth header and the permission carried as the verb.
	assert.Equal(t, 1, reviewer.calls)
	assert.Equal(t, "root:orgs:test", reviewer.gotWorkspace)
	assert.Equal(t, testAuthHeader, reviewer.gotAuthHeader)
	require.NotNil(t, reviewer.gotAttrs)
	assert.Equal(t, "get_iam_roles", reviewer.gotAttrs.Verb)
	assert.Equal(t, pmcorev1alpha1.GroupName, reviewer.gotAttrs.Group)
	assert.Equal(t, "accountinfos", reviewer.gotAttrs.Resource)
	assert.Equal(t, "account", reviewer.gotAttrs.Name)
}

func TestAuthorized_ErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		setupContext  func() context.Context
		setupMocks    func(*accountinfomocks.Retriever)
		expectedError string
	}{
		{
			name: "missing web token",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				return ctx
			},
			setupMocks:    func(*accountinfomocks.Retriever) {},
			expectedError: "failed to get web token from context",
		},
		{
			name: "missing auth header",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				ctx = context.WithValue(ctx, keys.WebTokenCtxKey, createTestWebToken())
				return ctx
			},
			setupMocks:    func(*accountinfomocks.Retriever) {},
			expectedError: "failed to get auth header from context",
		},
		{
			name: "missing kcp context",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				return withAuth(ctx)
			},
			setupMocks:    func(*accountinfomocks.Retriever) {},
			expectedError: "failed to get kcp user context",
		},
		{
			name: "invalid GraphQL field context",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				ctx = withAuth(ctx)

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
			setupMocks:    func(*accountinfomocks.Retriever) {},
			expectedError: "unable to extract param from request",
		},
		{
			name: "account info retrieval error",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				ctx = withAuth(ctx)

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
								"name":      "account",
								"namespace": "test-namespace",
							},
						},
					},
				}
				ctx = graphql.WithFieldContext(ctx, fieldCtx)
				return ctx
			},
			setupMocks: func(air *accountinfomocks.Retriever) {
				air.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(nil, fmt.Errorf("account not found"))
			},
			expectedError: "failed to get account info from kcp context",
		},
		{
			name: "organization mismatch",
			setupContext: func() context.Context {
				ctx, _ := setupTestContext()
				ctx = withAuth(ctx)

				kcpCtx := appcontext.KCPContext{
					IDMTenant:        "test-tenant",
					OrganizationName: "different-org", // Mismatch
				}
				ctx = appcontext.SetKCPContext(ctx, kcpCtx)

				fieldCtx := &graphql.FieldContext{
					Args: map[string]any{
						"context": map[string]any{
							"group":       "core.platform-mesh.io",
							"kind":        "AccountInfo",
							"accountPath": "root:orgs:test",
							"resource": map[string]any{
								"name":      "account",
								"namespace": "test-namespace",
							},
						},
					},
				}
				ctx = graphql.WithFieldContext(ctx, fieldCtx)
				return ctx
			},
			setupMocks: func(air *accountinfomocks.Retriever) {
				ai := createTestAccountInfo()
				air.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(ai, nil)
			},
			expectedError: "unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, log := setupTestContext()

			accountInfoRetriever := accountinfomocks.NewRetriever(t)
			tt.setupMocks(accountInfoRetriever)

			// Setup workspace client
			fakeClient := setupFakeClient(t)
			wsClient := &mockWSClient{client: fakeClient}

			reviewer := &recordingReviewer{allowed: true}

			// Create directive
			directive := NewAuthorizedDirectiveWithReviewer(accountInfoRetriever, wsClient, reviewer.review, log)

			// Setup context
			ctx := tt.setupContext()

			// Mock next resolver
			next := func(ctx context.Context) (any, error) {
				return "success", nil
			}

			// Execute test
			result, err := directive.Authorized(ctx, nil, next, "get_iam_roles")

			// Verify results
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestAuthorized_ResourceNotExists(t *testing.T) {
	ctx, log := setupTestContext()

	accountInfoRetriever := accountinfomocks.NewRetriever(t)

	ai := createTestAccountInfo()
	accountInfoRetriever.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(ai, nil)

	// Setup fake workspace client without the resource
	fakeClient := setupFakeClient(t) // Empty client, resource doesn't exist
	wsClient := &mockWSClient{client: fakeClient}

	reviewer := &recordingReviewer{allowed: true}

	// Create directive
	directive := NewAuthorizedDirectiveWithReviewer(accountInfoRetriever, wsClient, reviewer.review, log)

	// Setup context
	ctx = withAuth(ctx)

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
	result, err := directive.Authorized(ctx, nil, next, "get_iam_roles")

	// Verify results
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "resource does not exist")
	assert.Equal(t, 0, reviewer.calls)
}

func TestAuthorized_NotAllowed(t *testing.T) {
	ctx, log := setupTestContext()

	accountInfoRetriever := accountinfomocks.NewRetriever(t)

	ai := createTestAccountInfo()
	accountInfoRetriever.EXPECT().Get(mock.Anything, multicluster.ClusterName("root:orgs:test")).Return(ai, nil)

	// Setup fake workspace client with resource
	fakeClient := setupFakeClient(t, ai)
	wsClient := &mockWSClient{client: fakeClient}

	// SSAR denies the request.
	reviewer := &recordingReviewer{allowed: false}

	// Create directive
	directive := NewAuthorizedDirectiveWithReviewer(accountInfoRetriever, wsClient, reviewer.review, log)

	// Setup context
	ctx = withAuth(ctx)

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
	result, err := directive.Authorized(ctx, nil, next, "get_iam_roles")

	// Verify results
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unauthorized")
	assert.Equal(t, 1, reviewer.calls)
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
		name          string
		reviewer      *recordingReviewer
		resourceCtx   *graph.ResourceContext
		permission    string
		workspacePath string

		expectedResult    bool
		expectedError     string
		expectedResource  string
		expectedNamespace string
		expectedVerb      string
	}{
		{
			name:              "allowed with namespace",
			reviewer:          &recordingReviewer{allowed: true},
			resourceCtx:       createTestResourceContext(),
			permission:        "get_iam_roles",
			workspacePath:     "root:orgs:test",
			expectedResult:    true,
			expectedResource:  "accountinfos",
			expectedNamespace: "test-namespace",
			expectedVerb:      "get_iam_roles",
		},
		{
			name:        "denied",
			reviewer:    &recordingReviewer{allowed: false},
			resourceCtx: createTestResourceContext(),
			permission:  "manage_iam_roles",
			// path chosen by caller (Authorized). Here we just assert passthrough.
			workspacePath:     "root:orgs:test",
			expectedResult:    false,
			expectedResource:  "accountinfos",
			expectedNamespace: "test-namespace",
			expectedVerb:      "manage_iam_roles",
		},
		{
			name:          "reviewer error",
			reviewer:      &recordingReviewer{err: fmt.Errorf("ssar failed")},
			resourceCtx:   createTestResourceContext(),
			permission:    "get_iam_roles",
			workspacePath: "root:orgs:test",
			expectedError: "failed to perform self subject access review",
		},
		{
			name:     "rest mapping fails for unknown kind",
			reviewer: &recordingReviewer{allowed: true},
			resourceCtx: &graph.ResourceContext{
				Group:       "nonexistent.api.group",
				Kind:        "InvalidResourceKind",
				AccountPath: "root:orgs:test",
				Resource: &graph.Resource{
					Name: "test-resource",
				},
			},
			permission:    "get_iam_roles",
			workspacePath: "root:orgs:test",
			expectedError: "failed to resolve REST mapping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, log := setupTestContext()

			accountInfoRetriever := accountinfomocks.NewRetriever(t)

			fakeClient := setupFakeClient(t)
			wsClient := &mockWSClient{client: fakeClient}

			directive := NewAuthorizedDirectiveWithReviewer(accountInfoRetriever, wsClient, tt.reviewer.review, log)

			result, err := directive.testIfAllowed(ctx, tt.resourceCtx, tt.permission, tt.workspacePath, testAuthHeader, fakeClient)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.False(t, result)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedResult, result)

			require.Equal(t, 1, tt.reviewer.calls)
			assert.Equal(t, tt.workspacePath, tt.reviewer.gotWorkspace)
			assert.Equal(t, testAuthHeader, tt.reviewer.gotAuthHeader)
			require.NotNil(t, tt.reviewer.gotAttrs)
			assert.Equal(t, tt.expectedVerb, tt.reviewer.gotAttrs.Verb)
			assert.Equal(t, tt.expectedResource, tt.reviewer.gotAttrs.Resource)
			assert.Equal(t, tt.resourceCtx.Resource.Name, tt.reviewer.gotAttrs.Name)
			assert.Equal(t, tt.expectedNamespace, tt.reviewer.gotAttrs.Namespace)
		})
	}
}

func TestTestIfResourceExists(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(t *testing.T) ctrlruntimeclient.Client
		resourceCtx    *graph.ResourceContext
		expectedResult bool
		expectedError  string
	}{
		{
			name: "resource exists - namespaced",
			setupClient: func(t *testing.T) ctrlruntimeclient.Client {
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
			setupClient: func(t *testing.T) ctrlruntimeclient.Client {
				// Setup with cluster-scoped REST mapping
				ai := createTestAccountInfo()

				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{pmcorev1alpha1.SchemeGroupVersion})
				rm.Add(pmcorev1alpha1.SchemeGroupVersion.WithKind("AccountInfo"), meta.RESTScopeRoot) // Cluster-scoped

				scheme := runtime.NewScheme()
				require.NoError(t, pmcorev1alpha1.AddToScheme(scheme))

				return fake.NewClientBuilder().
					WithRESTMapper(rm).
					WithScheme(scheme).
					WithObjects(ai).
					Build()
			},
			resourceCtx: &graph.ResourceContext{
				Group: pmcorev1alpha1.GroupName,
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
			setupClient: func(t *testing.T) ctrlruntimeclient.Client {
				return setupFakeClient(t) // Empty client
			},
			resourceCtx: &graph.ResourceContext{
				Group: pmcorev1alpha1.GroupName,
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
			setupClient: func(t *testing.T) ctrlruntimeclient.Client {
				// Setup with cluster-scoped REST mapping but no objects
				rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{pmcorev1alpha1.SchemeGroupVersion})
				rm.Add(pmcorev1alpha1.SchemeGroupVersion.WithKind("AccountInfo"), meta.RESTScopeRoot) // Cluster-scoped

				scheme := runtime.NewScheme()
				require.NoError(t, pmcorev1alpha1.AddToScheme(scheme))

				return fake.NewClientBuilder().
					WithRESTMapper(rm).
					WithScheme(scheme).
					Build() // No objects
			},
			resourceCtx: &graph.ResourceContext{
				Group: pmcorev1alpha1.GroupName,
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
			setupClient: func(t *testing.T) ctrlruntimeclient.Client {
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

			accountInfoRetriever := accountinfomocks.NewRetriever(t)
			wsClient := &mockWSClient{client: fakeClient}

			reviewer := &recordingReviewer{allowed: true}

			// Create directive
			directive := NewAuthorizedDirectiveWithReviewer(accountInfoRetriever, wsClient, reviewer.review, log)

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
