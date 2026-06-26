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

package clusteraccess_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/controllers/clusteraccess"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/options"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const testNamespace = "test-ns"

type ClusterAccessControllerTestSuite struct {
	suite.Suite

	env         *envtest.Environment
	listenerCfg *listener.Config
	cancel      context.CancelFunc
	client      ctrlruntimeclient.Client

	// Store envtest credentials for creating test secrets
	envtestHost       string
	envtestCAData     []byte
	envtestCertData   []byte
	envtestKeyData    []byte
	envtestKubeconfig []byte
}

func TestClusterAccessControllerTestSuite(t *testing.T) {
	suite.Run(t, new(ClusterAccessControllerTestSuite))
}

func (suite *ClusterAccessControllerTestSuite) SetupSuite() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	log.SetLogger(klog.NewKlogr())

	suite.env = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd"),
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

	tmpDir := suite.T().TempDir()

	// Write the kubeconfig bytes to a temp file for the listener config
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err = os.WriteFile(kubeconfigPath, suite.env.KubeConfig, 0600)
	suite.Require().NoError(err, "failed to write kubeconfig")

	opts := options.NewOptions()
	opts.Common.Kubeconfig = kubeconfigPath
	opts.Common.Metrics.BindAddress = "0"
	opts.Common.HealthProbeBindAddress = "0"
	opts.SchemasDir = filepath.Join(tmpDir, "schemas")

	completedOpts, err := opts.Complete()
	suite.Require().NoError(err, "failed to complete options")

	listenerConfig, err := listener.NewConfig(completedOpts)
	suite.Require().NoError(err, "failed to create listener config")

	r, err := clusteraccess.NewClusterAccessReconciler(
		suite.T().Context(),
		listenerConfig.Manager,
		controller.TypedOptions[mcreconcile.Request]{},
		listenerConfig.SchemaHandler,
	)
	suite.Require().NoError(err, "failed to create clusteraccess reconciler")

	err = r.SetupWithManager(listenerConfig.Manager)
	suite.Require().NoError(err, "failed to setup clusteraccess reconciler with manager")

	suite.listenerCfg = listenerConfig

	// Create a client directly from the envtest config
	testScheme := runtime.NewScheme()
	err = clientgoscheme.AddToScheme(testScheme)
	suite.Require().NoError(err, "failed to add client-go scheme")
	err = pmgatewayv1alpha1.AddToScheme(testScheme)
	suite.Require().NoError(err, "failed to add v1alpha1 scheme")

	suite.client, err = ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: testScheme})
	suite.Require().NoError(err, "failed to create client")

	ctx, cancel := context.WithCancel(suite.T().Context())
	suite.cancel = cancel

	go func() {
		err = listenerConfig.Manager.Start(ctx)
		suite.Require().NoError(err, "failed to start multi-cluster manager")
	}()

	// Wait for manager to be ready
	time.Sleep(500 * time.Millisecond)

	// Create test namespace
	suite.createTestNamespace()

	// Create shared CA secret
	suite.createCASecret()
}

func (suite *ClusterAccessControllerTestSuite) TearDownSuite() {
	suite.cancel()
	err := suite.env.Stop()
	suite.Require().NoError(err, "failed to stop test environment")
}

// createTestNamespace creates the test namespace for secrets
func (suite *ClusterAccessControllerTestSuite) createTestNamespace() {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
	}
	err := suite.client.Create(context.Background(), ns)
	suite.Require().NoError(err, "failed to create test namespace")
}

// createCASecret creates a secret with the envtest CA data
func (suite *ClusterAccessControllerTestSuite) createCASecret() {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca-secret",
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			"ca.crt": suite.envtestCAData,
		},
	}
	err := suite.client.Create(context.Background(), secret)
	suite.Require().NoError(err, "failed to create CA secret")
}

// grantClusterAdminToSA grants cluster-admin role to a ServiceAccount
func (suite *ClusterAccessControllerTestSuite) grantClusterAdminToSA(saName, saNamespace string) {
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: saName + "-cluster-admin",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: saNamespace,
			},
		},
	}
	err := suite.client.Create(context.Background(), binding)
	suite.Require().NoError(err, "failed to create cluster role binding for %s", saName)
}

// waitForSchemaFile waits for schema file to be generated
func (suite *ClusterAccessControllerTestSuite) waitForSchemaFile(name string) {
	schemaFilePath := filepath.Join(suite.listenerCfg.Options.SchemasDir, name)
	suite.Eventually(func() bool {
		_, err := os.Stat(schemaFilePath)
		return err == nil
	}, 10*time.Second, 500*time.Millisecond,
		"expected schema file to be generated for %s", name)
}

// verifySchemaMetadata reads and validates the schema file
func (suite *ClusterAccessControllerTestSuite) verifySchemaMetadata(
	name string,
	expectedAuthType pmgatewayv1alpha1.AuthenticationType,
) {
	schemaFilePath := filepath.Join(suite.listenerCfg.Options.SchemasDir, name)
	raw, err := os.ReadFile(schemaFilePath)
	suite.Require().NoError(err, "failed to read schema file")

	var schemaJSON map[string]any
	err = json.NewDecoder(bytes.NewReader(raw)).Decode(&schemaJSON)
	suite.Require().NoError(err, "failed to decode schema file")

	// The x-cluster-metadata is stored as raw JSON bytes in the schema
	metadataRaw, ok := schemaJSON["x-cluster-metadata"]
	suite.Require().True(ok, "schema should have x-cluster-metadata")

	var metadata pmgatewayv1alpha1.ClusterMetadata
	switch v := metadataRaw.(type) {
	case string:
		err = json.Unmarshal([]byte(v), &metadata)
	case []byte:
		err = json.Unmarshal(v, &metadata)
	default:
		// Try to marshal and unmarshal
		data, marshalErr := json.Marshal(v)
		suite.Require().NoError(marshalErr, "failed to marshal metadata")
		err = json.Unmarshal(data, &metadata)
	}
	suite.Require().NoError(err, "failed to decode cluster metadata")

	suite.NotEmpty(metadata.Host, "metadata should have host")
	suite.NotNil(metadata.Auth, "metadata should have auth")
	suite.Equal(expectedAuthType, metadata.Auth.Type, "auth type should match")
}

// TestKubeconfigAuth tests ClusterAccess with kubeconfig authentication
func (suite *ClusterAccessControllerTestSuite) TestKubeconfigAuth() {
	// Create secret with kubeconfig
	kubeconfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubeconfig-secret",
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			"kubeconfig": suite.envtestKubeconfig,
		},
	}
	err := suite.client.Create(context.Background(), kubeconfigSecret)
	suite.Require().NoError(err, "failed to create kubeconfig secret")

	// Create ClusterAccess with kubeconfig auth
	clusterAccess := &pmgatewayv1alpha1.ClusterAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kubeconfig-test",
		},
		Spec: pmgatewayv1alpha1.ClusterAccessSpec{
			Host: suite.envtestHost,
			Auth: &pmgatewayv1alpha1.AuthConfig{
				KubeconfigSecretRef: &pmgatewayv1alpha1.SecretKeyRef{
					SecretReference: corev1.SecretReference{
						Name:      "kubeconfig-secret",
						Namespace: testNamespace,
					},
					Key: "kubeconfig",
				},
			},
		},
	}
	err = suite.client.Create(context.Background(), clusterAccess)
	suite.Require().NoError(err, "failed to create ClusterAccess")

	// Wait for schema file to be generated
	suite.waitForSchemaFile("single-kubeconfig-test")

	// Verify schema metadata
	suite.verifySchemaMetadata("single-kubeconfig-test", pmgatewayv1alpha1.AuthTypeKubeconfig)
}

// TestKubeconfigAuthWithoutHost tests ClusterAccess with kubeconfig authentication and no explicit host.
// The host should be derived from the kubeconfig's server URL.
func (suite *ClusterAccessControllerTestSuite) TestKubeconfigAuthWithoutHost() {
	// Create secret with kubeconfig
	kubeconfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubeconfig-nohost-secret",
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			"kubeconfig": suite.envtestKubeconfig,
		},
	}
	err := suite.client.Create(context.Background(), kubeconfigSecret)
	suite.Require().NoError(err, "failed to create kubeconfig secret")

	// Create ClusterAccess without host — should derive it from kubeconfig
	clusterAccess := &pmgatewayv1alpha1.ClusterAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kubeconfig-nohost-test",
		},
		Spec: pmgatewayv1alpha1.ClusterAccessSpec{
			Auth: &pmgatewayv1alpha1.AuthConfig{
				KubeconfigSecretRef: &pmgatewayv1alpha1.SecretKeyRef{
					SecretReference: corev1.SecretReference{
						Name:      "kubeconfig-nohost-secret",
						Namespace: testNamespace,
					},
					Key: "kubeconfig",
				},
			},
		},
	}
	err = suite.client.Create(context.Background(), clusterAccess)
	suite.Require().NoError(err, "failed to create ClusterAccess")

	// Wait for schema file to be generated
	suite.waitForSchemaFile("single-kubeconfig-nohost-test")

	// Verify schema metadata — host should have been derived from kubeconfig
	suite.verifySchemaMetadata("single-kubeconfig-nohost-test", pmgatewayv1alpha1.AuthTypeKubeconfig)
}

// TestTokenAuth tests ClusterAccess with token authentication
func (suite *ClusterAccessControllerTestSuite) TestTokenAuth() {
	// First create a ServiceAccount to generate a token
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "token-test-sa",
			Namespace: testNamespace,
		},
	}
	err := suite.client.Create(context.Background(), sa)
	suite.Require().NoError(err, "failed to create service account")

	// Grant cluster-admin role to the ServiceAccount for API discovery
	suite.grantClusterAdminToSA("token-test-sa", testNamespace)

	// Generate a token using TokenRequest API
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To[int64](3600),
		},
	}

	err = suite.client.SubResource("token").Create(context.Background(), sa, tokenRequest)
	suite.Require().NoError(err, "failed to create token request")

	token := tokenRequest.Status.Token
	suite.Require().NotEmpty(token, "token should not be empty")

	// Create secret with the generated token
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "token-secret",
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			corev1.ServiceAccountTokenKey: []byte(token),
		},
	}
	err = suite.client.Create(context.Background(), tokenSecret)
	suite.Require().NoError(err, "failed to create token secret")

	// Create ClusterAccess with token auth
	clusterAccess := &pmgatewayv1alpha1.ClusterAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name: "token-test",
		},
		Spec: pmgatewayv1alpha1.ClusterAccessSpec{
			Host: suite.envtestHost,
			CA: &pmgatewayv1alpha1.CAConfig{
				SecretRef: &pmgatewayv1alpha1.SecretKeyRef{
					SecretReference: corev1.SecretReference{
						Name:      "ca-secret",
						Namespace: testNamespace,
					},
					Key: "ca.crt",
				},
			},
			Auth: &pmgatewayv1alpha1.AuthConfig{
				TokenSecretRef: &pmgatewayv1alpha1.SecretKeyRef{
					SecretReference: corev1.SecretReference{
						Name:      "token-secret",
						Namespace: testNamespace,
					},
					Key: corev1.ServiceAccountTokenKey,
				},
			},
		},
	}
	err = suite.client.Create(context.Background(), clusterAccess)
	suite.Require().NoError(err, "failed to create ClusterAccess")

	// Wait for schema file to be generated
	suite.waitForSchemaFile("single-token-test")

	// Verify schema metadata
	suite.verifySchemaMetadata("single-token-test", pmgatewayv1alpha1.AuthTypeToken)
}

// TestClientCertAuth tests ClusterAccess with client certificate authentication
func (suite *ClusterAccessControllerTestSuite) TestClientCertAuth() {
	// Create TLS secret with client cert and key
	clientCertSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "client-cert-secret",
			Namespace: testNamespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       suite.envtestCertData,
			corev1.TLSPrivateKeyKey: suite.envtestKeyData,
		},
	}
	err := suite.client.Create(context.Background(), clientCertSecret)
	suite.Require().NoError(err, "failed to create client cert secret")

	// Create ClusterAccess with client cert auth
	clusterAccess := &pmgatewayv1alpha1.ClusterAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name: "clientcert-test",
		},
		Spec: pmgatewayv1alpha1.ClusterAccessSpec{
			Host: suite.envtestHost,
			CA: &pmgatewayv1alpha1.CAConfig{
				SecretRef: &pmgatewayv1alpha1.SecretKeyRef{
					SecretReference: corev1.SecretReference{
						Name:      "ca-secret",
						Namespace: testNamespace,
					},
					Key: "ca.crt",
				},
			},
			Auth: &pmgatewayv1alpha1.AuthConfig{
				ClientCertificateRef: &corev1.SecretReference{
					Name:      "client-cert-secret",
					Namespace: testNamespace,
				},
			},
		},
	}
	err = suite.client.Create(context.Background(), clusterAccess)
	suite.Require().NoError(err, "failed to create ClusterAccess")

	// Wait for schema file to be generated
	suite.waitForSchemaFile("single-clientcert-test")

	// Verify schema metadata
	suite.verifySchemaMetadata("single-clientcert-test", pmgatewayv1alpha1.AuthTypeClientCert)
}

// TestServiceAccountAuth tests ClusterAccess with service account authentication
func (suite *ClusterAccessControllerTestSuite) TestServiceAccountAuth() {
	// Create a ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sa",
			Namespace: testNamespace,
		},
	}
	err := suite.client.Create(context.Background(), sa)
	suite.Require().NoError(err, "failed to create service account")

	// Grant cluster-admin role to the ServiceAccount for API discovery
	suite.grantClusterAdminToSA("test-sa", testNamespace)

	// Create ClusterAccess with service account auth
	// The reconciler generates a token and stores SA details in metadata
	clusterAccess := &pmgatewayv1alpha1.ClusterAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name: "serviceaccount-test",
		},
		Spec: pmgatewayv1alpha1.ClusterAccessSpec{
			Host: suite.envtestHost,
			CA: &pmgatewayv1alpha1.CAConfig{
				SecretRef: &pmgatewayv1alpha1.SecretKeyRef{
					SecretReference: corev1.SecretReference{
						Name:      "ca-secret",
						Namespace: testNamespace,
					},
					Key: "ca.crt",
				},
			},
			Auth: &pmgatewayv1alpha1.AuthConfig{
				ServiceAccountRef: &pmgatewayv1alpha1.ServiceAccountRef{
					Name:      "test-sa",
					Namespace: testNamespace,
				},
			},
		},
	}
	err = suite.client.Create(context.Background(), clusterAccess)
	suite.Require().NoError(err, "failed to create ClusterAccess")

	// Wait for schema file to be generated
	suite.waitForSchemaFile("single-serviceaccount-test")

	// Verify schema metadata has SA auth type and details
	schemaFilePath := filepath.Join(suite.listenerCfg.Options.SchemasDir, "single-serviceaccount-test")
	raw, err := os.ReadFile(schemaFilePath)
	suite.Require().NoError(err, "failed to read schema file")

	var schemaJSON map[string]any
	err = json.NewDecoder(bytes.NewReader(raw)).Decode(&schemaJSON)
	suite.Require().NoError(err, "failed to decode schema file")

	metadataRaw, ok := schemaJSON["x-cluster-metadata"]
	suite.Require().True(ok, "schema should have x-cluster-metadata")

	var metadata pmgatewayv1alpha1.ClusterMetadata
	switch v := metadataRaw.(type) {
	case string:
		err = json.Unmarshal([]byte(v), &metadata)
	case []byte:
		err = json.Unmarshal(v, &metadata)
	default:
		data, marshalErr := json.Marshal(v)
		suite.Require().NoError(marshalErr, "failed to marshal metadata")
		err = json.Unmarshal(data, &metadata)
	}
	suite.Require().NoError(err, "failed to decode cluster metadata")

	suite.NotNil(metadata.Auth, "metadata should have auth")
	suite.Equal(pmgatewayv1alpha1.AuthTypeServiceAccount, metadata.Auth.Type, "auth type should be serviceAccount")
	suite.Equal("test-sa", metadata.Auth.SAName, "SA name should match")
	suite.Equal(testNamespace, metadata.Auth.SANamespace, "SA namespace should match")
	suite.NotEmpty(metadata.Auth.Token, "SA auth should have generated token")
}
