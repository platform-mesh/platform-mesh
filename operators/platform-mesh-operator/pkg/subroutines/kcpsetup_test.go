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

package subroutines

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	kcpmanifests "go.platform-mesh.io/apis/static-assets/kcp"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// ManifestStructureTest is the kcp manifest tree the kcpsetup tests apply: the
// embedded manifests/kcp, i.e. what the operator applies at runtime.
var ManifestStructureTest = kcpmanifests.Resources

func defaultTestOperatorConfig() *config.OperatorConfig {
	cfg := &config.OperatorConfig{}
	cfg.Subroutines.KcpSetup.DomainCertificateCASecretName = "domain-certificate"
	cfg.Subroutines.KcpSetup.DomainCertificateCASecretKey = "tls.crt"
	return cfg
}

type KcpsetupTestSuite struct {
	suite.Suite
	clientMock *mocks.Client
	helperMock *mocks.KcpHelper
	testObj    *KcpsetupSubroutine
	log        *logger.Logger
}

func TestKcpsetupTestSuite(t *testing.T) {
	suite.Run(t, new(KcpsetupTestSuite))
}

func (s *KcpsetupTestSuite) SetupTest() {
	s.clientMock = new(mocks.Client)
	s.helperMock = new(mocks.KcpHelper)
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "KcpsetupTestSuite"
	s.log, _ = logger.New(cfg)
	s.testObj = NewKcpsetupSubroutine(s.clientMock, s.helperMock, defaultTestOperatorConfig(), ManifestStructureTest, "https://kcp.example.com")
}

func (s *KcpsetupTestSuite) TearDownTest() {
	s.clientMock = nil
	s.helperMock = nil
	s.testObj = nil
}

func (s *KcpsetupTestSuite) Test_Constructor() {
	// create new logger
	s.log, _ = logger.New(logger.DefaultConfig())

	// create new mock client
	s.clientMock = new(mocks.Client)
	helper := &Helper{}

	// create new test object
	s.testObj = NewKcpsetupSubroutine(s.clientMock, helper, defaultTestOperatorConfig(), ManifestStructureTest, "")
}

func (s *KcpsetupTestSuite) Test_applyDirStructure() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	kcpClientMock := new(mocks.Client)
	// Expect NewKcpClient to be called multiple times for different workspaces (flexible count)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(kcpClientMock, nil).Maybe()
	inventory := map[string]any{
		"apiExportRootTenancyKcpIoIdentityHash":  "hash1",
		"apiExportRootShardsKcpIoIdentityHash":   "hash2",
		"apiExportRootTopologyKcpIoIdentityHash": "hash3",
		"registrationAllowed":                    true,
	}

	// Expect multiple Apply calls for applying manifests (flexible count)
	kcpClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Mock unstructured object lookups (for general manifest objects - flexible count)
	kcpClientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			unstructuredObj := obj.(*unstructured.Unstructured)
			unstructuredObj.Object = map[string]any{
				"status": map[string]any{
					"phase": "Ready",
				},
			}
			return nil
		})

	// Mock workspace lookups for waitForWorkspace calls (multiple calls for polling)
	kcpClientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			ws := obj.(*kcptenancyv1alpha1.Workspace)
			ws.Status.Phase = "Ready"
			return nil
		})

	// Mock APIExport lookups
	kcpClientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.APIExport")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			export := obj.(*kcpapisv1alpha1.APIExport)
			export.Status.IdentityHash = "test-hash"
			return nil
		})

	err := ApplyDirStructure(ctx, kcpmanifests.Resources, ".", "root", &rest.Config{}, inventory, &pmcorev1alpha1.PlatformMesh{}, s.helperMock, nil)

	s.Assert().Nil(err)
}

func (s *KcpsetupTestSuite) Test_getCABundleInventory() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	expectedCaData := []byte("test-ca-data")

	// Test case 1: Success case
	// Mock the mutating webhook secret lookup (called once due to caching)
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      DEFAULT_WEBHOOK_CONFIGURATION.SecretRef.Name,
			Namespace: DEFAULT_WEBHOOK_CONFIGURATION.SecretRef.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				DEFAULT_WEBHOOK_CONFIGURATION.SecretData: expectedCaData,
			}
			return nil
		}).
		Once() // Only called once due to caching

	// Mock the validating webhook secret lookup (called once due to caching)
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Name,
			Namespace: DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION.SecretData: expectedCaData,
			}
			return nil
		}).
		Once() // Only called once due to caching

	// Mock the identity provider validating webhook secret lookup (called once due to caching)
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Name,
			Namespace: DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.SecretData: expectedCaData,
			}
			return nil
		}).
		Once() // Only called once due to caching

	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      "domain-certificate",
			Namespace: "platform-mesh-system",
		}, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"ca.crt":  []byte("test-ca-data"),
				"tls.crt": []byte("test-tls-crt"),
				"tls.key": []byte("test-tls-key"),
			}
			return nil
		})

	// First call should fetch from secrets
	inventory, err := s.testObj.GetCABundleInventory(ctx, &pmcorev1alpha1.PlatformMesh{})
	s.Assert().NoError(err)
	s.Assert().NotNil(inventory)

	// Check mutating webhook CA bundle
	mutatingKey := DEFAULT_WEBHOOK_CONFIGURATION.WebhookRef.Name + ".ca-bundle"
	s.Assert().Contains(inventory, mutatingKey)
	expectedB64 := "dGVzdC1jYS1kYXRh" // base64 encoding of "test-ca-data"
	s.Assert().Equal(expectedB64, inventory[mutatingKey])

	// Check validating webhook CA bundle
	validatingKey := DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION.WebhookRef.Name + ".ca-bundle"
	s.Assert().Contains(inventory, validatingKey)
	s.Assert().Equal(expectedB64, inventory[validatingKey])

	// Check identity provider validating webhook CA bundle
	ipdValidatingKey := DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.WebhookRef.Name + ".ca-bundle"
	s.Assert().Contains(inventory, ipdValidatingKey)
	s.Assert().Equal(expectedB64, inventory[ipdValidatingKey])

	// Second call should use cache (no additional mock calls expected)
	inventory2, err2 := s.testObj.GetCABundleInventory(ctx, &pmcorev1alpha1.PlatformMesh{})
	s.Assert().NoError(err2)
	s.Assert().NotNil(inventory2)
	s.Assert().Contains(inventory2, mutatingKey)
	s.Assert().Contains(inventory2, validatingKey)
	s.Assert().Contains(inventory2, ipdValidatingKey)
	s.Assert().Equal(expectedB64, inventory2[mutatingKey])
	s.Assert().Equal(expectedB64, inventory2[validatingKey])
	s.Assert().Equal(expectedB64, inventory2[ipdValidatingKey])

	s.clientMock.AssertExpectations(s.T())

	// Test case 2: Secret not found
	// Create a new instance to clear the cache
	s.testObj = NewKcpsetupSubroutine(s.clientMock, s.helperMock, defaultTestOperatorConfig(), ManifestStructureTest, "")

	// Mock the mutating webhook secret lookup to return error
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      DEFAULT_WEBHOOK_CONFIGURATION.SecretRef.Name,
			Namespace: DEFAULT_WEBHOOK_CONFIGURATION.SecretRef.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		Return(errors.New("secret not found")).
		Once()

	inventory, err = s.testObj.GetCABundleInventory(ctx, &pmcorev1alpha1.PlatformMesh{})
	s.Assert().Error(err)
	s.Assert().Nil(inventory)
	s.Assert().Contains(err.Error(), "Failed to get CA bundle")
	s.clientMock.AssertExpectations(s.T())
}

func (s *KcpsetupTestSuite) Test_getCABundleInventory_CustomSecretNameAndKey() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	customSecretName := "my-custom-ca-secret"
	customSecretKey := "ca.pem"
	customCfg := defaultTestOperatorConfig()
	customCfg.Subroutines.KcpSetup.DomainCertificateCASecretName = customSecretName
	customCfg.Subroutines.KcpSetup.DomainCertificateCASecretKey = customSecretKey

	clientMock := new(mocks.Client)
	s.testObj = NewKcpsetupSubroutine(clientMock, s.helperMock, customCfg, ManifestStructureTest, "")

	// Mock the mutating webhook secret lookup
	clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      DEFAULT_WEBHOOK_CONFIGURATION.SecretRef.Name,
			Namespace: DEFAULT_WEBHOOK_CONFIGURATION.SecretRef.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				DEFAULT_WEBHOOK_CONFIGURATION.SecretData: []byte("test-ca-data"),
			}
			return nil
		}).Once()

	// Mock the validating webhook secret lookup
	clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Name,
			Namespace: DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION.SecretData: []byte("test-ca-data"),
			}
			return nil
		}).Once()

	// Mock the identity provider validating webhook secret lookup
	clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Name,
			Namespace: DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.SecretData: []byte("test-ca-data"),
			}
			return nil
		}).Once()

	// Mock the custom-named domain CA secret lookup with custom key
	clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      customSecretName,
			Namespace: "platform-mesh-system",
		}, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				customSecretKey: []byte("custom-ca-data"),
			}
			return nil
		}).Once()

	inventory, err := s.testObj.GetCABundleInventory(ctx, &pmcorev1alpha1.PlatformMesh{})
	s.Assert().NoError(err)
	s.Assert().NotNil(inventory)
	s.Assert().Contains(inventory, "domainCA")
	s.Assert().Contains(inventory, "domainCADec")
	s.Assert().Equal("custom-ca-data", inventory["domainCADec"])

	clientMock.AssertExpectations(s.T())
}

func (s *KcpsetupTestSuite) Test_GetCaBundle() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	webhookConfig := &pmcorev1alpha1.WebhookConfiguration{
		SecretRef: pmcorev1alpha1.SecretReference{
			Name:      "ca-secret",
			Namespace: "default",
		},
		SecretData: "ca.crt",
	}
	expectedCaData := []byte("test-ca-data")

	// Test case 1: Successful retrieval
	s.clientMock.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "ca-secret", Namespace: "default"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"ca.crt": expectedCaData,
			}
			return nil
		}).Once()

	caData, err := s.testObj.GetCaBundle(ctx, webhookConfig)
	s.Assert().NoError(err)
	s.Assert().Equal(expectedCaData, caData)
	s.clientMock.AssertExpectations(s.T())

	// Test case 2: Secret not found
	s.clientMock.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "ca-secret", Namespace: "default"}, mock.AnythingOfType("*v1.Secret")).
		Return(errors.New("secret not found")).Once()

	caData, err = s.testObj.GetCaBundle(ctx, webhookConfig)
	s.Assert().Error(err)
	s.Assert().Nil(caData)
	s.Assert().Contains(err.Error(), "Failed to get ca secret")
	s.clientMock.AssertExpectations(s.T())

	// Test case 3: Secret data key not found
	s.clientMock.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "ca-secret", Namespace: "default"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"wrong-key": []byte("some data"),
			}
			return nil
		}).Once()

	caData, err = s.testObj.GetCaBundle(ctx, webhookConfig)
	s.Assert().Error(err)
	s.Assert().Nil(caData)
	s.Assert().Contains(err.Error(), "failed to get caData from secret")
	s.clientMock.AssertExpectations(s.T())
}

func (s *KcpsetupTestSuite) TestProcess() {
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	operatorCfg.KCP.RootShardName = "kcp"
	operatorCfg.KCP.Namespace = "default"
	operatorCfg.KCP.FrontProxyName = "kcp-front-proxy"
	operatorCfg.KCP.ClusterAdminSecretName = "kcp-cluster-admin"

	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg)

	// Mock the Helm release lookup
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp", Namespace: "default"}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			release := obj.(*unstructured.Unstructured)
			release.Object = map[string]any{
				"status": map[string]any{
					"conditions": []any{
						map[string]any{
							"type":   "Available",
							"status": "True",
						},
					},
				},
			}
			return nil
		})
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-front-proxy", Namespace: "default"}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			release := obj.(*unstructured.Unstructured)
			release.Object = map[string]any{
				"status": map[string]any{
					"conditions": []any{
						map[string]any{
							"type":   "Available",
							"status": "True",
						},
					},
				},
			}
			return nil
		})

	// Mock the kubeconfig secret lookup
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      "kcp-cluster-admin-client-cert",
			Namespace: "platform-mesh-system",
		}, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"ca.crt":  []byte("test-ca-data"),
				"tls.crt": []byte("test-tls-crt"),
				"tls.key": []byte("test-tls-key"),
			}
			return nil
		})
	fakeKubeconfig := []byte(`apiVersion: v1
clusters:
- cluster:
    server: https://kcp.example.com
  name: kcp
contexts:
- context:
    cluster: kcp
    user: admin
  name: kcp
current-context: kcp
kind: Config
users:
- name: admin
  user:
    token: fake-token
`)
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      "kcp-cluster-admin",
			Namespace: "default",
		}, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"kubeconfig": fakeKubeconfig,
			}
			return nil
		})
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      "domain-certificate",
			Namespace: "platform-mesh-system",
		}, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"ca.crt":  []byte("test-ca-data"),
				"tls.crt": []byte("test-tls-crt"),
				"tls.key": []byte("test-tls-key"),
			}
			return nil
		})

	// Mock the webhook server cert lookup (called once since we cache results)
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      DEFAULT_WEBHOOK_CONFIGURATION.SecretRef.Name,
			Namespace: DEFAULT_WEBHOOK_CONFIGURATION.SecretRef.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				DEFAULT_WEBHOOK_CONFIGURATION.SecretData: []byte("test-ca-data"),
			}
			return nil
		}).Once() // Only called once due to caching

	// Mock the identity provider validating webhook CA secret lookup
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Name,
			Namespace: DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.SecretRef.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION.SecretData: []byte("test-ca-data"),
			}
			return nil
		}).Once()

	// Mock the secondary webhook server cert lookup (called once since we cache results)
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      "account-operator-webhook-server-cert",
			Namespace: "platform-mesh-system",
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"ca.crt": []byte("test-ca-data"),
			}
			return nil
		})

	// Create mock KCP client for APIExport lookups
	mockKcpClient := new(mocks.Client)
	// The fga split migration lists APIBindings in root:orgs first; an
	// empty list means nothing to migrate.
	mockKcpClient.EXPECT().List(mock.Anything, mock.Anything).Return(nil).Maybe()

	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, "root").
		Return(mockKcpClient, nil)

	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:platform-mesh").
		Return(mockKcpClient, nil)

	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:platform-mesh-system").
		Return(mockKcpClient, nil)

	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:orgs").
		Return(mockKcpClient, nil)

	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:orgs:default").
		Return(mockKcpClient, nil)

	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers").
		Return(mockKcpClient, nil)

	// Mock APIExport lookups
	apiexport := &kcpapisv1alpha1.APIExport{
		Status: kcpapisv1alpha1.APIExportStatus{
			IdentityHash: "test-hash",
		},
	}

	// Mock all APIExport lookups
	mockKcpClient.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "tenancy.kcp.io"}, mock.AnythingOfType("*v1alpha1.APIExport")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			export := obj.(*kcpapisv1alpha1.APIExport)
			export.Status = apiexport.Status
			return nil
		})

	mockKcpClient.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "shards.core.kcp.io"}, mock.AnythingOfType("*v1alpha1.APIExport")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			export := obj.(*kcpapisv1alpha1.APIExport)
			export.Status = apiexport.Status
			return nil
		})

	mockKcpClient.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "topology.kcp.io"}, mock.AnythingOfType("*v1alpha1.APIExport")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			export := obj.(*kcpapisv1alpha1.APIExport)
			export.Status = apiexport.Status
			return nil
		})

	// Mock workspace lookups and patch calls
	mockKcpClient.EXPECT().
		Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			ws := obj.(*kcptenancyv1alpha1.Workspace)
			ws.Status.Phase = "Ready"
			return nil
		})

	mockKcpClient.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "orgs"}, mock.AnythingOfType("*v1alpha1.Workspace")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			ws := obj.(*kcptenancyv1alpha1.Workspace)
			ws.Status.Phase = "Ready"
			return nil
		})

	// Mock unstructured object lookups for manifest files (flexible count)
	mockKcpClient.EXPECT().
		Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			unstructuredObj := obj.(*unstructured.Unstructured)
			unstructuredObj.Object = map[string]any{
				"status": map[string]any{
					"phase": "Ready",
				},
			}
			return nil
		})

	// Mock apply calls for applying manifests (flexible count)
	mockKcpClient.EXPECT().
		Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	// Call Process
	result, err := s.testObj.Process(ctx, &pmcorev1alpha1.PlatformMesh{})

	// Assertions
	s.Assert().Nil(err)
	s.Assert().Equal(subroutines.OK(), result)

	// Test error case - create a new instance to clear the cache
	s.testObj = NewKcpsetupSubroutine(s.clientMock, s.helperMock, defaultTestOperatorConfig(), ManifestStructureTest, "https://kcp.example.com")
}

func (s *KcpsetupTestSuite) Test_getAPIExportHashInventory() {
	ctx := s.T().Context()

	// mocks
	mockKcpClient := new(mocks.Client)
	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(mockKcpClient, nil).Times(3)
	s.testObj = NewKcpsetupSubroutine(s.clientMock, mockedKcpHelper, defaultTestOperatorConfig(), ManifestStructureTest, "")

	apiexport := &kcpapisv1alpha1.APIExport{
		Status: kcpapisv1alpha1.APIExportStatus{
			IdentityHash: "hash1",
		},
	}
	mockKcpClient.EXPECT().Get(
		mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*kcpapisv1alpha1.APIExport) = *apiexport
			return nil
		}).Times(2)
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*kcpapisv1alpha1.APIExport) = *apiexport
			return errors.New("error")
		}).Once()

	inventory, err := s.testObj.GetAPIExportHashInventory(ctx, &rest.Config{})
	s.Assert().Error(err)
	s.Assert().Equal(map[string]string{
		"apiExportRootTenancyKcpIoIdentityHash": "hash1",
		"apiExportRootShardsKcpIoIdentityHash":  "hash1",
	}, inventory)

	// test error 2
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*kcpapisv1alpha1.APIExport) = *apiexport
			return nil
		}).Once()
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*kcpapisv1alpha1.APIExport) = *apiexport
			return errors.New("error")
		}).Once()

	inventory, err = s.testObj.GetAPIExportHashInventory(ctx, &rest.Config{})
	s.Assert().Error(err)
	s.Assert().Equal(map[string]string{
		"apiExportRootTenancyKcpIoIdentityHash": "hash1",
	}, inventory)

	// test error 3
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*kcpapisv1alpha1.APIExport) = *apiexport
			return errors.New("error")
		}).Once()

	inventory, err = s.testObj.GetAPIExportHashInventory(ctx, &rest.Config{})
	s.Assert().Error(err)
	s.Assert().Equal(map[string]string{}, inventory)

	// test error 4
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).
		Return(nil, errors.New("Error")).Once()
	inventory, err = s.testObj.GetAPIExportHashInventory(ctx, &rest.Config{})
	s.Assert().Error(err)
	s.Assert().Equal(map[string]string{}, inventory)
}

func (s *KcpsetupTestSuite) TestFinalizers() {
	res := s.testObj.Finalizers(&pmcorev1alpha1.PlatformMesh{})
	s.Assert().Equal(res, []string{KcpsetupSubroutineFinalizer})
}

func (s *KcpsetupTestSuite) TestGetName() {
	res := s.testObj.GetName()
	s.Assert().Equal(res, KcpsetupSubroutineName)
}

func (s *KcpsetupTestSuite) TestFinalize() {
	res, err := s.testObj.Finalize(context.Background(), &pmcorev1alpha1.PlatformMesh{})
	s.Assert().Nil(err)
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *KcpsetupTestSuite) TestCreateWorkspaces() {
	// test err1 - expect error when NewKcpClient fails
	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(nil, errors.New("failed to create client"))
	s.testObj = NewKcpsetupSubroutine(s.clientMock, mockedKcpHelper, defaultTestOperatorConfig(), ManifestStructureTest, "")

	err := s.testObj.CreateKcpResources(context.Background(), &rest.Config{}, &pmcorev1alpha1.PlatformMesh{})
	s.Assert().Error(err)
	s.Assert().Contains(err.Error(), "Failed to get APIExport hash inventory")

	// test OK
	mockedK8sClient := new(mocks.Client)
	mockKcpClient := new(mocks.Client)
	mockedKcpHelper = new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(mockKcpClient, nil)
	s.testObj = NewKcpsetupSubroutine(mockedK8sClient, mockedKcpHelper, defaultTestOperatorConfig(), ManifestStructureTest, "")

	// Mock both webhook secret lookups for CA bundle inventory
	webhookConfig := DEFAULT_WEBHOOK_CONFIGURATION
	validatingWebhookConfig := DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION
	ipdValidatingWebhookConfig := DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION

	// Mock the mutating webhook secret lookup (called once due to caching)
	mockedK8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{
		Name:      webhookConfig.SecretRef.Name,
		Namespace: webhookConfig.SecretRef.Namespace,
	}, mock.AnythingOfType("*v1.Secret")).
		Run(func(ctx context.Context, key types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
			sec := obj.(*corev1.Secret)
			sec.Data = map[string][]byte{
				webhookConfig.SecretData: []byte("dummy-ca-data"),
			}
		}).
		Return(nil).
		Once()

	// Mock the domain certificate CA lookup
	mockedK8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{
		Name:      "domain-certificate",
		Namespace: webhookConfig.SecretRef.Namespace,
	}, mock.AnythingOfType("*v1.Secret")).
		Run(func(ctx context.Context, key types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
			sec := obj.(*corev1.Secret)
			sec.Data = map[string][]byte{
				"ca.crt":  []byte("test-ca-data"),
				"tls.crt": []byte("test-tls-crt"),
				"tls.key": []byte("test-tls-key"),
			}
		}).
		Return(nil)

	// Mock the identity provider validating webhook secret lookup (called once due to caching)
	mockedK8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{
		Name:      ipdValidatingWebhookConfig.SecretRef.Name,
		Namespace: ipdValidatingWebhookConfig.SecretRef.Namespace,
	}, mock.AnythingOfType("*v1.Secret")).
		Run(func(ctx context.Context, key types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
			sec := obj.(*corev1.Secret)
			sec.Data = map[string][]byte{
				ipdValidatingWebhookConfig.SecretData: []byte("dummy-ca-data"),
			}
		}).
		Return(nil).
		Once()

	// Mock the validating webhook secret lookup (called once due to caching)
	mockedK8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{
		Name:      validatingWebhookConfig.SecretRef.Name,
		Namespace: validatingWebhookConfig.SecretRef.Namespace,
	}, mock.AnythingOfType("*v1.Secret")).
		Run(func(ctx context.Context, key types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
			sec := obj.(*corev1.Secret)
			sec.Data = map[string][]byte{
				validatingWebhookConfig.SecretData: []byte("dummy-ca-data"),
			}
		}).
		Return(nil).
		Once()

	apiexport := &kcpapisv1alpha1.APIExport{
		Status: kcpapisv1alpha1.APIExportStatus{
			IdentityHash: "hash1",
		},
	}
	workspace := &kcptenancyv1alpha1.Workspace{
		Status: kcptenancyv1alpha1.WorkspaceStatus{
			Phase: "Ready",
		},
	}
	// Mock APIExport lookups
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.APIExport")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			*o.(*kcpapisv1alpha1.APIExport) = *apiexport
			return nil
		})

	// Mock workspace lookups (flexible count for polling)
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			*o.(*kcptenancyv1alpha1.Workspace) = *workspace
			return nil
		}).Maybe()

	// Mock unstructured object lookups for manifest files (flexible count)
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			unstructuredObj := o.(*unstructured.Unstructured)
			unstructuredObj.Object = map[string]any{
				"status": map[string]any{
					"phase": "Ready",
				},
			}
			return nil
		})

	// Mock apply calls for applying manifests (flexible count)
	mockKcpClient.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	err = s.testObj.CreateKcpResources(context.Background(), &rest.Config{}, &pmcorev1alpha1.PlatformMesh{})
	s.Assert().Nil(err)

	// test err2 - expect error when Apply fails
	mockKcpClient = new(mocks.Client)
	mockedKcpHelper = new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(mockKcpClient, nil)
	s.testObj = NewKcpsetupSubroutine(mockedK8sClient, mockedKcpHelper, defaultTestOperatorConfig(), ManifestStructureTest, "")

	// Mock both secret lookups again (they should be cached from previous call)
	// Since we're creating a new instance, the cache is cleared, so we need to mock again
	mockedK8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{
		Name:      webhookConfig.SecretRef.Name,
		Namespace: webhookConfig.SecretRef.Namespace,
	}, mock.AnythingOfType("*v1.Secret")).
		Run(func(ctx context.Context, key types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
			sec := obj.(*corev1.Secret)
			sec.Data = map[string][]byte{
				webhookConfig.SecretData: []byte("dummy-ca-data"),
			}
		}).
		Return(nil).
		Once()

	// Mock the domain certificate CA lookup
	mockedK8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{
		Name:      "domain-certificate-ca",
		Namespace: webhookConfig.SecretRef.Namespace,
	}, mock.AnythingOfType("*v1.Secret")).
		Run(func(ctx context.Context, key types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
			sec := obj.(*corev1.Secret)
			sec.Data = map[string][]byte{
				"ca.crt":  []byte("test-ca-data"),
				"tls.crt": []byte("test-tls-crt"),
				"tls.key": []byte("test-tls-key"),
			}
		}).
		Return(nil)

	// Mock the identity provider validating webhook secret lookup (called once due to caching)
	mockedK8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{
		Name:      ipdValidatingWebhookConfig.SecretRef.Name,
		Namespace: ipdValidatingWebhookConfig.SecretRef.Namespace,
	}, mock.AnythingOfType("*v1.Secret")).
		Run(func(ctx context.Context, key types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
			sec := obj.(*corev1.Secret)
			sec.Data = map[string][]byte{
				ipdValidatingWebhookConfig.SecretData: []byte("dummy-ca-data"),
			}
		}).
		Return(nil).
		Once()

	mockedK8sClient.EXPECT().Get(mock.Anything, types.NamespacedName{
		Name:      validatingWebhookConfig.SecretRef.Name,
		Namespace: validatingWebhookConfig.SecretRef.Namespace,
	}, mock.AnythingOfType("*v1.Secret")).
		Run(func(ctx context.Context, key types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
			sec := obj.(*corev1.Secret)
			sec.Data = map[string][]byte{
				validatingWebhookConfig.SecretData: []byte("dummy-ca-data"),
			}
		}).
		Return(nil).
		Once()

	// Mock APIExport lookups
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.APIExport")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			*o.(*kcpapisv1alpha1.APIExport) = *apiexport
			return nil
		})

	// Mock workspace lookups (2 calls for platform-mesh-system and orgs workspaces)
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			*o.(*kcptenancyv1alpha1.Workspace) = *workspace
			return nil
		}).Times(2)

	// Mock unstructured object lookups for manifest files (flexible count)
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			unstructuredObj := o.(*unstructured.Unstructured)
			unstructuredObj.Object = map[string]any{
				"status": map[string]any{
					"phase": "Ready",
				},
			}
			return nil
		})

	// Mock apply calls for applying manifests (flexible count) - but they should fail
	mockKcpClient.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("apply failed"))
	err = s.testObj.CreateKcpResources(context.Background(), &rest.Config{}, &pmcorev1alpha1.PlatformMesh{})
	s.Assert().Error(err)
	s.Assert().Contains(err.Error(), "Failed to apply")
}

func (s *KcpsetupTestSuite) Test_ApplyExtraWorkspaces_Success() {
	// Arrange
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	parentPath := "root:orgs"
	fullPath := parentPath + ":extra-ws"

	kcpClientMock := new(mocks.Client)
	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, parentPath).
		Return(kcpClientMock, nil).Once()

	// Server-side apply - no Get needed
	kcpClientMock.EXPECT().
		Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()

	inst := s.newPlatformMeshWithExtraWorkspaces([]extraWsDef{
		{Path: fullPath, TypeName: "universal", TypePath: "root"},
	})

	// Act
	err := s.testObj.ApplyExtraWorkspaces(ctx, &rest.Config{}, inst)

	// Assert
	s.Assert().NoError(err)
}

func (s *KcpsetupTestSuite) Test_ApplyExtraWorkspaces_InvalidPath_Skipped() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	// No expectation for NewKcpClient because invalid path is skipped
	inst := s.newPlatformMeshWithExtraWorkspaces([]extraWsDef{
		{Path: "invalid-no-colon", TypeName: "universal", TypePath: "root"},
	})

	err := s.testObj.ApplyExtraWorkspaces(ctx, &rest.Config{}, inst)
	s.Assert().NoError(err)
	s.helperMock.AssertNotCalled(s.T(), "NewKcpClient", mock.Anything, mock.Anything)
}

func (s *KcpsetupTestSuite) Test_ApplyExtraWorkspaces_NewKcpClient_Error() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	parentPath := "root:team"
	fullPath := parentPath + ":ws1"

	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, parentPath).
		Return(nil, errors.New("boom")).Once()

	inst := s.newPlatformMeshWithExtraWorkspaces([]extraWsDef{
		{Path: fullPath, TypeName: "typeA", TypePath: "root"},
	})

	err := s.testObj.ApplyExtraWorkspaces(ctx, &rest.Config{}, inst)
	s.Assert().Error(err)
	s.Assert().Contains(err.Error(), "Failed to create kcp client")
}

func (s *KcpsetupTestSuite) Test_ApplyExtraWorkspaces_Apply_Error() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	parentPath := "root:orgs"
	fullPath := parentPath + ":ws3"

	kcpClientMock := new(mocks.Client)
	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, parentPath).
		Return(kcpClientMock, nil).Once()

	// Server-side apply fails - no Get needed
	kcpClientMock.EXPECT().
		Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("apply failed")).Once()

	inst := s.newPlatformMeshWithExtraWorkspaces([]extraWsDef{
		{Path: fullPath, TypeName: "universal", TypePath: "root"},
	})

	err := s.testObj.ApplyExtraWorkspaces(ctx, &rest.Config{}, inst)
	s.Assert().Error(err)
	s.Assert().Contains(err.Error(), "Failed to apply extra workspace")
}

//
// Helpers for constructing PlatformMesh with ExtraWorkspaces.
// These helper type names guess the actual API names; adjust if different.
//

type extraWsDef struct {
	Path     string
	TypeName string
	TypePath string
}

func (s *KcpsetupTestSuite) newPlatformMeshWithExtraWorkspaces(defs []extraWsDef) *pmcorev1alpha1.PlatformMesh {
	pm := &pmcorev1alpha1.PlatformMesh{}
	// Ensure nested structs exist
	pm.Spec.Kcp = pmcorev1alpha1.Kcp{}

	// Attempt to populate using likely field names; ignore if they differ (tests will need adjustment).
	for _, d := range defs {
		pm.Spec.Kcp.ExtraWorkspaces = append(pm.Spec.Kcp.ExtraWorkspaces, pmcorev1alpha1.WorkspaceDeclaration{
			Path: d.Path,
			Type: pmcorev1alpha1.WorkspaceTypeReference{
				Name: d.TypeName,
			},
		})
	}
	return pm
}

func (s *KcpsetupTestSuite) Test_HasFeatureToggle() {
	tests := []struct {
		name           string
		featureToggles []pmcorev1alpha1.FeatureToggle
		toggleName     string
		expected       bool
	}{
		{
			name:           "returns true when feature toggle exists",
			featureToggles: []pmcorev1alpha1.FeatureToggle{{Name: "feature-disable-email-verification"}},
			toggleName:     "feature-disable-email-verification",
			expected:       true,
		},
		{
			name:           "returns false when feature toggle does not exist",
			featureToggles: []pmcorev1alpha1.FeatureToggle{{Name: "feature-enable-getting-started"}},
			toggleName:     "feature-disable-email-verification",
			expected:       false,
		},
		{
			name:           "returns false when feature toggles are empty",
			featureToggles: nil,
			toggleName:     "feature-disable-email-verification",
			expected:       false,
		},
		{
			name: "returns true when toggle is among multiple toggles",
			featureToggles: []pmcorev1alpha1.FeatureToggle{
				{Name: "feature-enable-getting-started"},
				{Name: "feature-disable-email-verification"},
			},
			toggleName: "feature-disable-email-verification",
			expected:   true,
		},
		{
			name:           "returns true for feature-disable-contentconfigurations",
			featureToggles: []pmcorev1alpha1.FeatureToggle{{Name: "feature-disable-contentconfigurations"}},
			toggleName:     "feature-disable-contentconfigurations",
			expected:       true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			inst := &pmcorev1alpha1.PlatformMesh{
				Spec: pmcorev1alpha1.PlatformMeshSpec{
					FeatureToggles: tc.featureToggles,
				},
			}
			result := HasFeatureToggle(inst, tc.toggleName)
			s.Assert().Equal(tc.expected, result)
		})
	}
}

func (s *KcpsetupTestSuite) Test_WorkspaceAuthConfigTemplate_FeatureDisableEmailVerification() {
	templateBytes, err := fs.ReadFile(kcpmanifests.Resources, "workspace-authentication-configuration.yaml")
	s.Require().NoError(err, "Failed to read workspace-authentication-configuration.yaml")

	tests := []struct {
		name                  string
		featureToggleValue    any
		expectClaimValidation bool
		expectedExpression    string
		expectedMessage       string
	}{
		{
			name:                  "includes claimValidationRules when feature is enabled",
			featureToggleValue:    true,
			expectClaimValidation: true,
			expectedExpression:    `claims.?email_verified.orValue(true) == true || claims.?email_verified.orValue(true) == false`,
			expectedMessage:       "Allowing both verified and unverified emails",
		},
		{
			name:                  "excludes claimValidationRules when feature is disabled",
			featureToggleValue:    false,
			expectClaimValidation: false,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			templateData := map[string]any{
				"baseDomainPort":                  "example.com:443",
				"domainCADec":                     "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
				"featureDisableEmailVerification": tc.featureToggleValue,
				"welcomeAudiences":                []string{"test-audience"},
			}

			result, err := ReplaceTemplate(templateData, templateBytes)
			s.Require().NoError(err, "Template rendering should not fail")

			renderedYAML := string(result)

			if tc.expectClaimValidation {
				s.Assert().Contains(renderedYAML, "claimValidationRules:", "Should contain claimValidationRules")
				s.Assert().Contains(renderedYAML, tc.expectedExpression, "Should contain the expected expression")
				s.Assert().Contains(renderedYAML, tc.expectedMessage, "Should contain the expected message")
			} else {
				s.Assert().NotContains(renderedYAML, "claimValidationRules:", "Should NOT contain claimValidationRules")
			}

			// Always verify the basic structure is present
			s.Assert().Contains(renderedYAML, "kind: WorkspaceAuthenticationConfiguration")
			s.Assert().Contains(renderedYAML, "name: orgs-authentication")
			s.Assert().Contains(renderedYAML, "claimMappings:")
		})
	}
}

func (s *KcpsetupTestSuite) Test_ApplyManifestFromFile_SkipsContentConfiguration_WhenToggleEnabled() {
	tests := []struct {
		name               string
		featureToggleValue any
		expectSkipped      bool
	}{
		{
			name:               "skips ContentConfiguration when toggle is enabled",
			featureToggleValue: true,
			expectSkipped:      true,
		},
		{
			name:               "applies ContentConfiguration when toggle is disabled",
			featureToggleValue: false,
			expectSkipped:      false,
		},
		{
			name:               "applies ContentConfiguration when toggle is not present",
			featureToggleValue: nil,
			expectSkipped:      false,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

			kcpClientMock := new(mocks.Client)

			templateData := map[string]any{
				"featureDisableContentConfigurations": tc.featureToggleValue,
			}

			path := "01-platform-mesh-system/contentconfiguration-main-home.yaml"

			if tc.expectSkipped {
				err := ApplyManifestFromFile(ctx, kcpmanifests.Resources, path, kcpClientMock, templateData, "root:platform-mesh-system", &pmcorev1alpha1.PlatformMesh{}, nil)
				s.Assert().NoError(err)
				kcpClientMock.AssertNotCalled(s.T(), "Get", mock.Anything, mock.Anything, mock.Anything)
				kcpClientMock.AssertNotCalled(s.T(), "Apply", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			} else {
				kcpClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

				err := ApplyManifestFromFile(ctx, kcpmanifests.Resources, path, kcpClientMock, templateData, "root:platform-mesh-system", &pmcorev1alpha1.PlatformMesh{}, nil)
				s.Assert().NoError(err)
			}
		})
	}
}

func (s *KcpsetupTestSuite) Test_ApplyManifestFromFile_DoesNotSkipNonContentConfiguration_WhenToggleEnabled() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	kcpClientMock := new(mocks.Client)

	templateData := map[string]any{
		"featureDisableContentConfigurations": true,
	}

	// Use a non-ContentConfiguration file (e.g., a workspace file)
	path := "workspace-platform-mesh-system.yaml"

	// Even with toggle enabled, non-ContentConfiguration files should be applied
	kcpClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	err := ApplyManifestFromFile(ctx, kcpmanifests.Resources, path, kcpClientMock, templateData, "root", &pmcorev1alpha1.PlatformMesh{}, nil)
	s.Assert().NoError(err)
}

func (s *KcpsetupTestSuite) Test_idpWebhookDisabled() {
	tests := []struct {
		name     string
		toggles  []pmcorev1alpha1.FeatureToggle
		expected bool
	}{
		{name: "no toggles", expected: false},
		{name: "idp webhook toggle", toggles: []pmcorev1alpha1.FeatureToggle{{Name: FeatureDisableIDPWebhook}}, expected: true},
		{name: "keycloak toggle implies idp webhook", toggles: []pmcorev1alpha1.FeatureToggle{{Name: FeatureDisableKeycloak}}, expected: true},
		{name: "unrelated toggle", toggles: []pmcorev1alpha1.FeatureToggle{{Name: "feature-accounts-in-accounts"}}, expected: false},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			inst := &pmcorev1alpha1.PlatformMesh{Spec: pmcorev1alpha1.PlatformMeshSpec{FeatureToggles: tc.toggles}}
			s.Assert().Equal(tc.expected, idpWebhookDisabled(inst))
		})
	}
}

// Test_ApplyManifestFromFile_FeatureDisableKeycloak locks in that the
// Keycloak-backed manifests render to nothing (and are not applied) when the
// toggle is "true", and are applied for "false" and for a missing key.
func (s *KcpsetupTestSuite) Test_ApplyManifestFromFile_FeatureDisableKeycloak() {
	manifests := []string{
		"03-platform-mesh-system/welcome-ipc.yaml",
		"03-platform-mesh-system/welcome-ipc-legacy.yaml",
		"workspace-authentication-configuration.yaml",
	}
	tests := []struct {
		name          string
		templateData  map[string]any
		expectSkipped bool
	}{
		{name: "skipped when disabled", templateData: map[string]any{"featureDisableKeycloak": true}, expectSkipped: true},
		{name: "applied when enabled", templateData: map[string]any{"featureDisableKeycloak": false}},
		{name: "applied when key missing", templateData: map[string]any{}},
	}

	for _, manifest := range manifests {
		for _, tc := range tests {
			s.Run(manifest+" "+tc.name, func() {
				ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
				kcpClientMock := new(mocks.Client)
				templateData := map[string]any{
					"baseDomainPort":                  "example.com:443",
					"registrationAllowed":             false,
					"welcomeAudiences":                []string{},
					"featureDisableEmailVerification": false,
					"userClaim":                       "email",
				}
				for k, v := range tc.templateData {
					templateData[k] = v
				}
				if !tc.expectSkipped {
					kcpClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
				}

				err := ApplyManifestFromFile(ctx, kcpmanifests.Resources, manifest, kcpClientMock, templateData, "root", &pmcorev1alpha1.PlatformMesh{}, nil)

				s.Require().NoError(err)
				if tc.expectSkipped {
					kcpClientMock.AssertNotCalled(s.T(), "Apply", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
				}
			})
		}
	}
}

func (s *KcpsetupTestSuite) Test_OrgsWorkspaceTypeTemplate_FeatureDisableKeycloak() {
	templateBytes, err := fs.ReadFile(kcpmanifests.Resources, "02-root/01-workspace-type-orgs.yaml")
	s.Require().NoError(err)

	disabled, err := ReplaceTemplate(map[string]any{"featureDisableKeycloak": true}, templateBytes)
	s.Require().NoError(err)
	s.Assert().NotContains(string(disabled), "authenticationConfigurations")
	s.Assert().Contains(string(disabled), "kind: WorkspaceType")

	enabled, err := ReplaceTemplate(map[string]any{"featureDisableKeycloak": false}, templateBytes)
	s.Require().NoError(err)
	s.Assert().Contains(string(enabled), "orgs-authentication")
}

// Test_IDPWebhookTemplate_FeatureDisableIDPWebhook locks in the fix for the
// webhook never rendering: HasFeatureToggle yields false, which
// `not` treated as truthy.
func (s *KcpsetupTestSuite) Test_IDPWebhookTemplate_FeatureDisableIDPWebhook() {
	templateBytes, err := fs.ReadFile(kcpmanifests.Resources, "04-platform-mesh-system/idpvalidatingwebhookconfiguration-admissionregistration.k8s.io.yaml")
	s.Require().NoError(err)
	caKey := "identityproviderconfiguration-validator.webhooks.core.platform-mesh.io.ca-bundle"

	enabled, err := ReplaceTemplate(map[string]any{"featureDisableIDPWebhook": false, caKey: "Y2E="}, templateBytes)
	s.Require().NoError(err)
	s.Assert().Contains(string(enabled), "kind: ValidatingWebhookConfiguration")

	disabled, err := ReplaceTemplate(map[string]any{"featureDisableIDPWebhook": true, caKey: "Y2E="}, templateBytes)
	s.Require().NoError(err)
	s.Assert().NotContains(string(disabled), "kind: ValidatingWebhookConfiguration")
}

// Test_OpenFGAManifests_FeatureDisableOpenFGA locks in that every OpenFGA-backed
// manifest renders to nothing (or without its OpenFGA parts) when the toggle is
// "true", and stays complete otherwise.
func (s *KcpsetupTestSuite) Test_OpenFGAManifests_FeatureDisableOpenFGA() {
	render := func(file string, toggle bool) map[string]any {
		templateBytes, err := fs.ReadFile(kcpmanifests.Resources, file)
		s.Require().NoError(err, file)
		out, err := ReplaceTemplate(map[string]any{
			"featureDisableOpenFGA":                  toggle,
			"featureDisableKeycloak":                 false,
			"featureEnableTerminalControllerManager": false,
			"apiExportRootTenancyKcpIoIdentityHash":  "hash",
		}, templateBytes)
		s.Require().NoError(err, file)
		var obj map[string]any
		s.Require().NoError(yaml.Unmarshal(out, &obj), "%s renders invalid YAML:\n%s", file, out)
		return obj
	}

	for _, file := range []string{
		"01-platform-mesh-system/apiexport-fga.platform-mesh.io.yaml",
		"01-platform-mesh-system/apiexport-provider-fga.platform-mesh.io.yaml",
		"01-platform-mesh-system/apiexportendpointslice-fga.platform-mesh.io.yaml",
		"01-platform-mesh-system/apiexportendpointslice-provider-fga.platform-mesh.io.yaml",
		"01-platform-mesh-system/apiexport-openfga.platform-mesh.io.yaml",
		"01-platform-mesh-system/apiexport-provider-openfga.platform-mesh.io.yaml",
		"01-platform-mesh-system/apiexportendpointslice-openfga.platform-mesh.io.yaml",
		"01-platform-mesh-system/apiexportendpointslice-provider-openfga.platform-mesh.io.yaml",
		"01-platform-mesh-system/apibinding-openfga.platform-mesh.io.yaml",
		"03-orgs/apibinding-fga.platform-mesh.io.yaml",
		"03-orgs/apibinding-openfga.platform-mesh.io.yaml",
		"03-orgs/store.yaml",
	} {
		s.Assert().Nil(render(file, true), "%s must render to nothing without OpenFGA", file)
		s.Assert().NotNil(render(file, false), "%s must render with OpenFGA", file)
	}

	names := func(obj map[string]any, path ...string) []string {
		items, _, _ := unstructured.NestedSlice(obj, path...)
		var out []string
		for _, item := range items {
			m, _ := item.(map[string]any)
			if n, ok := m["name"].(string); ok {
				out = append(out, n)
			}
			if n, ok := m["export"].(string); ok {
				out = append(out, n)
			}
		}
		return out
	}
	for _, file := range []string{"02-root/workspace-type-org.yaml", "02-root/workspace-type-account.yaml"} {
		s.Assert().NotContains(names(render(file, true), "spec", "extend", "with"), "security", file)
		s.Assert().Contains(names(render(file, false), "spec", "extend", "with"), "security", file)
	}
	for _, export := range []string{"fga.platform-mesh.io", "openfga.platform-mesh.io"} {
		s.Assert().NotContains(names(render("02-root/01-workspace-type-orgs.yaml", true), "spec", "defaultAPIBindings"), export)
		s.Assert().Contains(names(render("02-root/01-workspace-type-orgs.yaml", false), "spec", "defaultAPIBindings"), export)
	}
	for _, export := range []string{"provider-fga.platform-mesh.io", "provider-openfga.platform-mesh.io"} {
		s.Assert().NotContains(names(render("02-root/workspace-type-provider.yaml", true), "spec", "defaultAPIBindings"), export)
		s.Assert().Contains(names(render("02-root/workspace-type-provider.yaml", false), "spec", "defaultAPIBindings"), export)
	}
}

// Test_OrgWorkspaceType_NoExtensions locks in that `org` renders without an
// empty extend.with (kcp rejects null) when both Keycloak and OpenFGA are off.
func (s *KcpsetupTestSuite) Test_OrgWorkspaceType_NoExtensions() {
	templateBytes, err := fs.ReadFile(kcpmanifests.Resources, "02-root/workspace-type-org.yaml")
	s.Require().NoError(err)

	out, err := ReplaceTemplate(map[string]any{
		"featureDisableOpenFGA":                  true,
		"featureDisableKeycloak":                 true,
		"featureEnableTerminalControllerManager": false,
	}, templateBytes)
	s.Require().NoError(err)

	var obj map[string]any
	s.Require().NoError(yaml.Unmarshal(out, &obj))
	_, found, _ := unstructured.NestedFieldNoCopy(obj, "spec", "extend")
	s.Assert().False(found, "extend must be absent, not null")
}
