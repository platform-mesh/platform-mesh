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

package e2e_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/config"
	gwhttp "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/http"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/controllers/reconciler"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/schemahandler"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const testNamespace = "gateway-test-ns"

// GatewayE2ETestSuite tests the gateway end-to-end using real listener schema generation
type GatewayE2ETestSuite struct {
	suite.Suite

	env    *envtest.Environment
	client ctrlruntimeclient.Client
	cancel context.CancelFunc

	// Temp directories
	schemasDir string

	// Envtest credentials for test schemas
	envtestHost       string
	envtestCAData     []byte
	envtestCertData   []byte
	envtestKeyData    []byte
	envtestKubeconfig []byte
	envtestConfig     *rest.Config

	// Listener schema generation (reuse real listener code)
	schemaReconciler *reconciler.Reconciler
	schemaHandler    schemahandler.Handler

	// Gateway components
	gatewayService *gateway.Service
	testServer     *httptest.Server

	// Auth token for tests
	testToken string
}

// GraphQLResponse represents the response from a GraphQL query
type GraphQLResponse struct {
	Data       map[string]any `json:"data"`
	Errors     []GraphQLError `json:"errors"`
	StatusCode int            `json:"-"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message string `json:"message"`
}

func TestGatewayE2ETestSuite(t *testing.T) {
	suite.Run(t, new(GatewayE2ETestSuite))
}

func (suite *GatewayE2ETestSuite) SetupSuite() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	log.SetLogger(klog.NewKlogr())

	// Start envtest with CRDs
	suite.env = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := suite.env.Start()
	suite.Require().NoError(err, "failed to start test environment")

	// Extract envtest credentials
	suite.envtestHost = cfg.Host
	suite.envtestCAData = cfg.CAData
	suite.envtestCertData = cfg.CertData
	suite.envtestKeyData = cfg.KeyData
	suite.envtestKubeconfig = suite.env.KubeConfig
	suite.envtestConfig = cfg

	// Create temp directory for schemas
	suite.schemasDir = suite.T().TempDir()

	// Create test scheme
	testScheme := runtime.NewScheme()
	err = clientgoscheme.AddToScheme(testScheme)
	suite.Require().NoError(err, "failed to add client-go scheme")
	err = pmgatewayv1alpha1.AddToScheme(testScheme)
	suite.Require().NoError(err, "failed to add v1alpha1 scheme")

	// Create client
	suite.client, err = ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: testScheme})
	suite.Require().NoError(err, "failed to create client")

	ctx, cancel := context.WithCancel(suite.T().Context())
	suite.cancel = cancel

	// Create test namespace
	suite.createTestNamespace()

	// Generate test token for authentication
	suite.generateTestToken()

	// Initialize listener schema handler (FileHandler)
	suite.schemaHandler, err = schemahandler.NewFileHandler(suite.schemasDir)
	suite.Require().NoError(err, "failed to create file handler")

	// Initialize listener schema reconciler
	suite.schemaReconciler = reconciler.NewReconciler(suite.schemaHandler)

	// Initialize gateway service
	suite.initGateway(ctx)
}

func (suite *GatewayE2ETestSuite) TearDownSuite() {
	suite.cancel()
	if suite.testServer != nil {
		suite.testServer.Close()
	}
	err := suite.env.Stop()
	suite.Require().NoError(err, "failed to stop test environment")
}

// initGateway initializes the gateway service with file watcher
func (suite *GatewayE2ETestSuite) initGateway(ctx context.Context) {
	// Create gateway configuration
	gatewayCfg := config.Gateway{
		SchemaHandler:   "file",
		SchemaDirectory: suite.schemasDir,
		GraphQL: config.GraphQL{
			Pretty:            true,
			PlaygroundEnabled: false,
			GraphiQL:          false,
		},
		Limits:              config.Limits{},
		TokenReviewCacheTTL: 30 * time.Second,
	}

	// Create gateway service
	var err error
	suite.gatewayService, err = gateway.New(gatewayCfg)
	suite.Require().NoError(err, "failed to create gateway service")

	// Start gateway in background
	go func() {
		err := suite.gatewayService.Run(ctx)
		if err != nil && ctx.Err() == nil {
			suite.T().Logf("Gateway stopped with error: %v", err)
		}
	}()

	// Wait for gateway to be ready
	readyCtx, readyCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readyCancel()
	err = suite.gatewayService.WaitForReady(readyCtx)
	suite.Require().NoError(err, "gateway failed to become ready")

	// Create HTTP server configuration
	httpCfg := gwhttp.ServerConfig{
		Gateway:    suite.gatewayService,
		Addr:       ":0", // Let system assign port
		CORSConfig: gwhttp.CORSConfig{},
	}

	httpServer, err := gwhttp.NewServer(httpCfg)
	suite.Require().NoError(err, "failed to create HTTP server")

	// Use httptest for easier testing
	suite.testServer = httptest.NewServer(httpServer.Server.Handler)
}

// createTestNamespace creates the test namespace
func (suite *GatewayE2ETestSuite) createTestNamespace() {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
	}
	err := suite.client.Create(suite.T().Context(), ns)
	suite.Require().NoError(err, "failed to create test namespace")
}

// generateTestToken creates a service account and generates a token for testing
func (suite *GatewayE2ETestSuite) generateTestToken() {
	// Create ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sa",
			Namespace: testNamespace,
		},
	}
	err := suite.client.Create(suite.T().Context(), sa)
	suite.Require().NoError(err, "failed to create service account")

	// Grant cluster-admin role
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sa-cluster-admin",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "test-sa",
				Namespace: testNamespace,
			},
		},
	}
	err = suite.client.Create(suite.T().Context(), binding)
	suite.Require().NoError(err, "failed to create cluster role binding")

	// Generate token
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To[int64](3600),
		},
	}

	err = suite.client.SubResource("token").Create(suite.T().Context(), sa, tokenRequest)
	suite.Require().NoError(err, "failed to create token request")

	suite.testToken = tokenRequest.Status.Token
}

// generateSchema uses the real listener reconciler to generate a schema
func (suite *GatewayE2ETestSuite) generateSchema(clusterName string, metadata *pmgatewayv1alpha1.ClusterMetadata) {
	ctx := suite.T().Context()
	err := suite.schemaReconciler.Reconcile(
		ctx,
		[]string{clusterName},
		suite.envtestConfig,
		metadata,
	)
	suite.Require().NoError(err, "failed to generate schema for %s", clusterName)
}

// waitForSchemaLoaded waits for the gateway to load a schema
func (suite *GatewayE2ETestSuite) waitForSchemaLoaded(clusterName string) {
	suite.Eventually(func() bool {
		_, exists := suite.gatewayService.Registry().GetEndpoint(clusterName)
		return exists
	}, 10*time.Second, 500*time.Millisecond,
		"expected schema to be loaded for cluster %s", clusterName)
}

// executeGraphQLQuery executes a GraphQL query against the gateway
func (suite *GatewayE2ETestSuite) executeGraphQLQuery(
	clusterName string,
	query string,
	variables map[string]any,
	token string,
) *GraphQLResponse {
	url := fmt.Sprintf("%s/api/clusters/%s", suite.testServer.URL, clusterName)

	requestBody := map[string]any{
		"query":     query,
		"variables": variables,
	}
	body, err := json.Marshal(requestBody)
	suite.Require().NoError(err, "failed to marshal request body")

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	suite.Require().NoError(err, "failed to create request")

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	suite.Require().NoError(err, "failed to execute request")
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	suite.Require().NoError(err, "failed to read response body")

	var gqlResp GraphQLResponse
	if len(respBody) > 0 {
		err = json.Unmarshal(respBody, &gqlResp)
		if err != nil {
			// Response might not be JSON (e.g., 404 text response)
			gqlResp.Errors = append(gqlResp.Errors, GraphQLError{Message: string(respBody)})
		}
	}

	gqlResp.StatusCode = resp.StatusCode
	return &gqlResp
}

// buildClusterMetadata creates ClusterMetadata for a given auth type
func (suite *GatewayE2ETestSuite) buildClusterMetadata(authType pmgatewayv1alpha1.AuthenticationType) *pmgatewayv1alpha1.ClusterMetadata {
	metadata := &pmgatewayv1alpha1.ClusterMetadata{
		Host: suite.envtestHost,
		CA: &pmgatewayv1alpha1.CAMetadata{
			Data: base64.StdEncoding.EncodeToString(suite.envtestCAData),
		},
	}

	switch authType {
	case pmgatewayv1alpha1.AuthTypeToken:
		metadata.Auth = &pmgatewayv1alpha1.AuthMetadata{
			Type:  pmgatewayv1alpha1.AuthTypeToken,
			Token: base64.StdEncoding.EncodeToString([]byte(suite.testToken)),
		}
	case pmgatewayv1alpha1.AuthTypeKubeconfig:
		metadata.Auth = &pmgatewayv1alpha1.AuthMetadata{
			Type:       pmgatewayv1alpha1.AuthTypeKubeconfig,
			Kubeconfig: base64.StdEncoding.EncodeToString(suite.envtestKubeconfig),
		}
	case pmgatewayv1alpha1.AuthTypeClientCert:
		metadata.Auth = &pmgatewayv1alpha1.AuthMetadata{
			Type:     pmgatewayv1alpha1.AuthTypeClientCert,
			CertData: base64.StdEncoding.EncodeToString(suite.envtestCertData),
			KeyData:  base64.StdEncoding.EncodeToString(suite.envtestKeyData),
		}
	case pmgatewayv1alpha1.AuthTypeServiceAccount:
		metadata.Auth = &pmgatewayv1alpha1.AuthMetadata{
			Type:        pmgatewayv1alpha1.AuthTypeServiceAccount,
			Token:       base64.StdEncoding.EncodeToString([]byte(suite.testToken)),
			SAName:      "test-sa",
			SANamespace: testNamespace,
		}
	}

	return metadata
}

// ============================================================================
// Authentication Tests
// ============================================================================

// TestKubeconfigAuth tests Gateway with kubeconfig authentication
func (suite *GatewayE2ETestSuite) TestKubeconfigAuth() {
	clusterName := "kubeconfig-test-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	resp := suite.executeGraphQLQuery(clusterName, `
		query {
			v1 {
				Namespaces {
					items {
						metadata { name }
					}
				}
			}
		}
	`, nil, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)
	suite.NotNil(resp.Data)
}

// TestTokenAuth tests Gateway with token authentication
func (suite *GatewayE2ETestSuite) TestTokenAuth() {
	clusterName := "token-test-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeToken)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	resp := suite.executeGraphQLQuery(clusterName, `
		query {
			v1 {
				Namespaces {
					items {
						metadata { name }
					}
				}
			}
		}
	`, nil, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)
}

// TestClientCertAuth tests Gateway with client certificate authentication
func (suite *GatewayE2ETestSuite) TestClientCertAuth() {
	clusterName := "clientcert-test-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeClientCert)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	resp := suite.executeGraphQLQuery(clusterName, `
		query {
			v1 {
				Namespaces {
					items {
						metadata { name }
					}
				}
			}
		}
	`, nil, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)
}

// TestServiceAccountAuth tests Gateway with service account authentication
func (suite *GatewayE2ETestSuite) TestServiceAccountAuth() {
	clusterName := "serviceaccount-test-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeServiceAccount)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	resp := suite.executeGraphQLQuery(clusterName, `
		query {
			v1 {
				Namespaces {
					items {
						metadata { name }
					}
				}
			}
		}
	`, nil, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)
}

// ============================================================================
// Token Validation Tests (TokenReview middleware)
// ============================================================================

// TestMissingAuthorizationHeader verifies that a request without an
// Authorization header is rejected with 401 at the HTTP layer before
// reaching the gateway.
func (suite *GatewayE2ETestSuite) TestMissingAuthorizationHeader() {
	clusterName := "missing-auth-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	// executeGraphQLQuery skips the Authorization header when token is ""
	resp := suite.executeGraphQLQuery(clusterName, `
		query {
			v1 {
				Namespaces {
					items { metadata { name } }
				}
			}
		}
	`, nil, "")

	suite.Equal(401, resp.StatusCode, "missing Authorization header should return 401")
}

// TestInvalidToken verifies that a request with a fabricated token that
// fails TokenReview validation is rejected with 401.
func (suite *GatewayE2ETestSuite) TestInvalidToken() {
	clusterName := "invalid-token-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	resp := suite.executeGraphQLQuery(clusterName, `
		query {
			v1 {
				Namespaces {
					items { metadata { name } }
				}
			}
		}
	`, nil, "garbage-token-that-should-not-validate")

	suite.Equal(401, resp.StatusCode, "invalid token should return 401 after TokenReview rejection")
}

// ============================================================================
// GraphQL Operation Tests
// ============================================================================

// TestListQuery tests list operations
func (suite *GatewayE2ETestSuite) TestListQuery() {
	clusterName := "list-test-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	// Create test ConfigMaps
	for i := range 3 {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("list-test-cm-%d", i),
				Namespace: testNamespace,
				Labels:    map[string]string{"test": "list"},
			},
			Data: map[string]string{"key": fmt.Sprintf("value-%d", i)},
		}
		err := suite.client.Create(suite.T().Context(), cm)
		suite.Require().NoError(err)
	}

	resp := suite.executeGraphQLQuery(clusterName, `
		query($ns: String!, $labelSelector: String) {
			v1 {
				ConfigMaps(namespace: $ns, labelselector: $labelSelector) {
					items {
						metadata { name namespace }
						data
					}
				}
			}
		}
	`, map[string]any{
		"ns":            testNamespace,
		"labelSelector": "test=list",
	}, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)

	// Verify response contains ConfigMaps
	v1Data := resp.Data["v1"].(map[string]any)
	configMaps := v1Data["ConfigMaps"].(map[string]any)
	items := configMaps["items"].([]any)
	suite.Len(items, 3)
}

// TestGetQuery tests get single item operations
func (suite *GatewayE2ETestSuite) TestGetQuery() {
	clusterName := "get-test-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	// Create test ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "get-test-cm",
			Namespace: testNamespace,
		},
		Data: map[string]string{"key": "value"},
	}
	err := suite.client.Create(suite.T().Context(), cm)
	suite.Require().NoError(err)

	resp := suite.executeGraphQLQuery(clusterName, `
		query($name: String!, $ns: String!) {
			v1 {
				ConfigMap(name: $name, namespace: $ns) {
					metadata { name namespace }
					data
				}
			}
		}
	`, map[string]any{
		"name": "get-test-cm",
		"ns":   testNamespace,
	}, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)

	v1Data := resp.Data["v1"].(map[string]any)
	configMap := v1Data["ConfigMap"].(map[string]any)
	cmMetadata := configMap["metadata"].(map[string]any)
	suite.Equal("get-test-cm", cmMetadata["name"])
}

// TestCreateMutation tests create operations
func (suite *GatewayE2ETestSuite) TestCreateMutation() {
	clusterName := "create-test-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	resp := suite.executeGraphQLQuery(clusterName, `
		mutation($ns: String!, $object: V1ConfigMap_Input!) {
			v1 {
				createConfigMap(namespace: $ns, object: $object) {
					metadata { name namespace }
				}
			}
		}
	`, map[string]any{
		"ns": testNamespace,
		"object": map[string]any{
			"metadata": map[string]any{
				"name": "created-cm",
			},
			"data": map[string]any{
				"created": "true",
			},
		},
	}, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)

	// Verify ConfigMap was created
	cm := &corev1.ConfigMap{}
	err := suite.client.Get(suite.T().Context(), ctrlruntimeclient.ObjectKey{
		Name:      "created-cm",
		Namespace: testNamespace,
	}, cm)
	suite.Require().NoError(err)
	suite.Equal("true", cm.Data["created"])
}

// TestUpdateMutation tests update operations
func (suite *GatewayE2ETestSuite) TestUpdateMutation() {
	clusterName := "update-test-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	// Create ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "update-test-cm",
			Namespace: testNamespace,
		},
		Data: map[string]string{"key": "original"},
	}
	err := suite.client.Create(suite.T().Context(), cm)
	suite.Require().NoError(err)

	resp := suite.executeGraphQLQuery(clusterName, `
		mutation($name: String!, $ns: String!, $object: V1ConfigMap_Input!) {
			v1 {
				updateConfigMap(name: $name, namespace: $ns, object: $object) {
					metadata { name }
					data
				}
			}
		}
	`, map[string]any{
		"name": "update-test-cm",
		"ns":   testNamespace,
		"object": map[string]any{
			"data": map[string]any{
				"key": "updated",
			},
		},
	}, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)

	// Verify update
	err = suite.client.Get(suite.T().Context(), ctrlruntimeclient.ObjectKey{
		Name:      "update-test-cm",
		Namespace: testNamespace,
	}, cm)
	suite.Require().NoError(err)
	suite.Equal("updated", cm.Data["key"])
}

// TestDeleteMutation tests delete operations
func (suite *GatewayE2ETestSuite) TestDeleteMutation() {
	clusterName := "delete-test-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	// Create ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-test-cm",
			Namespace: testNamespace,
		},
	}
	err := suite.client.Create(suite.T().Context(), cm)
	suite.Require().NoError(err)

	resp := suite.executeGraphQLQuery(clusterName, `
		mutation($name: String!, $ns: String!) {
			v1 {
				deleteConfigMap(name: $name, namespace: $ns)
			}
		}
	`, map[string]any{
		"name": "delete-test-cm",
		"ns":   testNamespace,
	}, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)

	// Verify deletion
	err = suite.client.Get(suite.T().Context(), ctrlruntimeclient.ObjectKey{
		Name:      "delete-test-cm",
		Namespace: testNamespace,
	}, cm)
	suite.True(apierrors.IsNotFound(err))
}

// ============================================================================
// applyYaml Mutation Tests
// ============================================================================

func (suite *GatewayE2ETestSuite) TestApplyYamlCreate() {
	clusterName := "apply-yaml-create-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	yamlInput := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: apply-yaml-created
  namespace: %s
data:
  key: value`, testNamespace)

	resp := suite.executeGraphQLQuery(clusterName, `
		mutation($yaml: String!) {
			applyYaml(yaml: $yaml)
		}
	`, map[string]any{
		"yaml": yamlInput,
	}, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors, "expected no errors, got: %v", resp.Errors)
	suite.NotNil(resp.Data["applyYaml"])

	cm := &corev1.ConfigMap{}
	err := suite.client.Get(suite.T().Context(), ctrlruntimeclient.ObjectKey{
		Name:      "apply-yaml-created",
		Namespace: testNamespace,
	}, cm)
	suite.Require().NoError(err)
	suite.Equal("value", cm.Data["key"])
}

func (suite *GatewayE2ETestSuite) TestApplyYamlUpdate() {
	clusterName := "apply-yaml-update-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	yamlCreate := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: apply-yaml-updated
  namespace: %s
data:
  key: original`, testNamespace)

	resp := suite.executeGraphQLQuery(clusterName, `
		mutation($yaml: String!) {
			applyYaml(yaml: $yaml)
		}
	`, map[string]any{
		"yaml": yamlCreate,
	}, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors)

	yamlUpdate := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: apply-yaml-updated
  namespace: %s
data:
  key: updated`, testNamespace)

	resp = suite.executeGraphQLQuery(clusterName, `
		mutation($yaml: String!) {
			applyYaml(yaml: $yaml)
		}
	`, map[string]any{
		"yaml": yamlUpdate,
	}, suite.testToken)

	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors)

	cm := &corev1.ConfigMap{}
	err := suite.client.Get(suite.T().Context(), ctrlruntimeclient.ObjectKey{
		Name:      "apply-yaml-updated",
		Namespace: testNamespace,
	}, cm)
	suite.Require().NoError(err)
	suite.Equal("updated", cm.Data["key"])
}

// ============================================================================
// Error Handling Tests
// ============================================================================

// TestClusterNotFound tests requests to non-existent clusters
func (suite *GatewayE2ETestSuite) TestClusterNotFound() {
	url := fmt.Sprintf("%s/api/clusters/nonexistent-cluster", suite.testServer.URL)

	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{"query": "{}"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+suite.testToken)

	resp, err := http.DefaultClient.Do(req)
	suite.Require().NoError(err)
	defer resp.Body.Close() //nolint:errcheck

	suite.Equal(404, resp.StatusCode)
}

// TestInvalidGraphQLQuery tests malformed GraphQL queries
func (suite *GatewayE2ETestSuite) TestInvalidGraphQLQuery() {
	clusterName := "invalid-query-cluster"
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)

	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	resp := suite.executeGraphQLQuery(clusterName, `
		query {
			nonExistentField {
				data
			}
		}
	`, nil, suite.testToken)

	suite.Equal(200, resp.StatusCode) // GraphQL returns 200 with errors in body
	suite.NotEmpty(resp.Errors, "expected GraphQL errors")
}

// ============================================================================
// Schema Lifecycle Tests
// ============================================================================

// TestSchemaReload tests that schema changes are picked up
func (suite *GatewayE2ETestSuite) TestSchemaReload() {
	clusterName := "reload-test-cluster"

	// Initially create schema with kubeconfig auth
	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)
	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	// Verify it works
	resp := suite.executeGraphQLQuery(clusterName, `
		query {
			v1 {
				Namespaces {
					items { metadata { name } }
				}
			}
		}
	`, nil, suite.testToken)
	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors)

	// Update schema file with token auth
	newMetadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeToken)
	suite.generateSchema(clusterName, newMetadata)

	// Wait for reload
	time.Sleep(1 * time.Second)

	// Verify still works
	resp = suite.executeGraphQLQuery(clusterName, `
		query {
			v1 {
				Namespaces {
					items { metadata { name } }
				}
			}
		}
	`, nil, suite.testToken)
	suite.Equal(200, resp.StatusCode)
	suite.Empty(resp.Errors)
}

// TestSchemaDelete tests that schema deletion removes endpoint
func (suite *GatewayE2ETestSuite) TestSchemaDelete() {
	clusterName := "delete-schema-cluster"

	metadata := suite.buildClusterMetadata(pmgatewayv1alpha1.AuthTypeKubeconfig)
	suite.generateSchema(clusterName, metadata)
	suite.waitForSchemaLoaded(clusterName)

	// Delete schema file
	schemaPath := filepath.Join(suite.schemasDir, clusterName)
	err := os.Remove(schemaPath)
	suite.Require().NoError(err)

	// Wait for deletion to be processed
	suite.Eventually(func() bool {
		_, exists := suite.gatewayService.Registry().GetEndpoint(clusterName)
		return !exists
	}, 10*time.Second, 500*time.Millisecond)

	// Verify endpoint is gone
	url := fmt.Sprintf("%s/api/clusters/%s", suite.testServer.URL, clusterName)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{"query": "{}"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+suite.testToken)

	resp, err := http.DefaultClient.Do(req)
	suite.Require().NoError(err)
	defer resp.Body.Close() //nolint:errcheck

	suite.Equal(404, resp.StatusCode)
}
