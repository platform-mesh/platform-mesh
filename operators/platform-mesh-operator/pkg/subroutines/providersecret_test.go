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
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
)

var secretKubeconfigData, _ = os.ReadFile("test/kubeconfig.yaml")

type fakeHelm struct{ ready bool }

func (f fakeHelm) GetRelease(ctx context.Context, cli ctrlruntimeclient.Client, name, ns string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{"ready": f.ready},
	}}
	return u, nil
}

func wantProviderKubeconfigServer(t *testing.T, inst *pmcorev1alpha1.PlatformMesh, op config.OperatorConfig, pc pmcorev1alpha1.ProviderConnection, kcpURL string) string {
	t.Helper()
	scheme := "https"
	if inst.Spec.Exposure != nil && inst.Spec.Exposure.Protocol != "" {
		scheme = inst.Spec.Exposure.Protocol
	}
	host := strings.TrimPrefix(strings.TrimPrefix(kcpURL, "https://"), "http://")
	var base string
	if pc.External {
		if inst.Spec.Exposure != nil && inst.Spec.Exposure.BaseDomain != "" {
			port := inst.Spec.Exposure.Port
			if port == 0 {
				port = 443
			}
			base = fmt.Sprintf("%s://kcp.api.%s:%d", scheme, inst.Spec.Exposure.BaseDomain, port)
		} else {
			base = scheme + "://" + host
		}
	} else {
		base = fmt.Sprintf("%s://%s-front-proxy.%s:%s", scheme, op.KCP.FrontProxyName, op.KCP.Namespace, op.KCP.FrontProxyPort)
	}
	switch {
	case ptr.Deref(pc.RawPath, "") != "":
		u, err := url.JoinPath(base, ptr.Deref(pc.RawPath, ""))
		if err != nil {
			t.Fatalf("join kubeconfig server URL: %v", err)
		}
		return u
	case pc.EndpointSliceName != nil && strings.TrimSpace(*pc.EndpointSliceName) != "":
		n := strings.TrimSpace(*pc.EndpointSliceName)
		u, err := url.JoinPath(base, "/services/apiexport/stubhash/"+n)
		if err != nil {
			t.Fatalf("join kubeconfig server URL: %v", err)
		}
		return u
	default:
		u, err := url.JoinPath(base, "clusters", pc.Path)
		if err != nil {
			t.Fatalf("join kubeconfig server URL: %v", err)
		}
		return u
	}
}

type ProvidersecretTestSuite struct {
	suite.Suite
	testObj *ProvidersecretSubroutine
	// mocks
	clientMock *mocks.Client
	scheme     *runtime.Scheme
	log        *logger.Logger
}

func TestProvidersecretTestSuite(t *testing.T) {
	suite.Run(t, new(ProvidersecretTestSuite))
}

func (s *ProvidersecretTestSuite) SetupTest() {
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "ProvidersecretTestSuite"
	s.log, _ = logger.New(cfg)

	s.clientMock = new(mocks.Client)

	s.scheme = runtime.NewScheme()
	_ = corev1.AddToScheme(s.scheme)
	_ = pmcorev1alpha1.AddToScheme(s.scheme)
	_ = kcpapisv1alpha1.AddToScheme(s.scheme)

	s.clientMock.EXPECT().Scheme().Return(s.scheme).Maybe()

	s.testObj = NewProviderSecretSubroutine(s.clientMock, &Helper{}, fakeHelm{ready: true}, "")
}

func (s *ProvidersecretTestSuite) TearDownTest() {
	// clear mocks
	s.clientMock = nil

	// clear test object
	s.testObj = nil
}

func (s *ProvidersecretTestSuite) TestProcess() {
	instance := &pmcorev1alpha1.PlatformMesh{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PlatformMesh",
			APIVersion: "core.platform-mesh.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Kcp: pmcorev1alpha1.Kcp{
				ProviderConnections: []pmcorev1alpha1.ProviderConnection{
					{
						EndpointSliceName: ptr.To("test-endpoint"),
						Path:              "root:platform-mesh-system",
						Secret:            "provider-secret",
					},
				},
			},
		},
		Status: pmcorev1alpha1.PlatformMeshStatus{
			KcpWorkspaces: []pmcorev1alpha1.KcpWorkspace{
				{Name: "root:platform-mesh-system", Phase: "Ready"},
				{Name: "root:orgs", Phase: "Ready"},
			},
		},
	}

	kubeconfig, err := clientcmd.Load(secretKubeconfigData)
	s.Require().NoError(err)

	kubeconfig.Contexts["custom-context"] = &clientcmdapi.Context{
		AuthInfo: "test-user",
		Cluster:  "custom-cluster",
	}

	if _, exists := kubeconfig.Clusters["custom-cluster"]; !exists {
		kubeconfig.Clusters["custom-cluster"] = &clientcmdapi.Cluster{}
	}
	kubeconfig.Clusters["custom-cluster"].Server = "http://dummy-url" // value replaced below
	kubeconfig.Contexts["custom-context"] = &clientcmdapi.Context{
		AuthInfo: "test-user",
		Cluster:  "custom-cluster",
	}
	kubeconfig.CurrentContext = "custom-context"

	patchedData, err := clientcmd.Write(*kubeconfig)
	s.Require().NoError(err)

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": patchedData,
		},
	}

	s.clientMock.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key types.NamespacedName) bool {
		return key.Name == "test-secret" && key.Namespace == "default"
	}), mock.Anything).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = secret
			return nil
		}).Once()

	s.clientMock.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key types.NamespacedName) bool {
		return key.Name == "provider-secret" && key.Namespace == "default"
	}), mock.Anything).
		Return(apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Secret"}, "provider-secret")).
		Once()

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).Return(nil)

	s.clientMock.EXPECT().Create(
		mock.Anything,
		mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
			secret, ok := obj.(*corev1.Secret)
			if !ok {
				s.log.Error().Msg("Object is not a Secret")
				return false
			}

			if secret.Name != "provider-secret" {
				s.log.Error().Msgf("Secret name mismatch: expected 'provider-secret', got '%s'", secret.Name)
				return false
			}
			if secret.Namespace != "default" {
				s.log.Error().Msgf("Secret namespace mismatch: expected 'default', got '%s'", secret.Namespace)
				return false
			}

			kubeconfigData, exists := secret.Data["kubeconfig"]
			if !exists {
				s.log.Error().Msg("kubeconfig data not found in secret")
				return false
			}

			kubeconfig, err := clientcmd.Load(kubeconfigData)
			if err != nil {
				s.log.Error().Msgf("Failed to parse kubeconfig: %v", err)
				return false
			}

			currentContext := kubeconfig.Contexts[kubeconfig.CurrentContext]
			cluster := kubeconfig.Clusters[currentContext.Cluster]

			// Test that the URL is passed correctly form the endpoint slice
			expectedURL := "http://example.com/clusters/root:platform-mesh-system"
			if cluster.Server != expectedURL {
				s.log.Error().Msgf("Server URL mismatch: expected '%s', got '%s'", expectedURL, cluster.Server)
				return false
			}
			return true
		}),
		mock.Anything,
	).
		RunAndReturn(func(ctx context.Context, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
			providerSecret := obj.(*corev1.Secret)
			err := controllerutil.SetOwnerReference(instance, providerSecret, s.clientMock.Scheme())
			s.NoError(err)
			return nil
		}).
		Once()

	scheme := runtime.NewScheme()
	err = pmcorev1alpha1.AddToScheme(scheme)
	s.Require().NoError(err)
	s.clientMock.EXPECT().Scheme().Return(scheme).Once()

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{
				{URL: "http://example.com"},
			},
		},
	}

	mockKcpClient := new(mocks.Client)
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*kcpapisv1alpha1.APIExportEndpointSlice) = *slice
			return nil
		}).Once()

	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(mockKcpClient, nil).Once()
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = corev1.Secret{
				Data: map[string][]byte{
					"kubeconfig": patchedData,
				},
			}
			return nil
		},
	).Once()

	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "")

	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}

	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg)
	res, opErr := s.testObj.Process(ctx, instance)
	s.Assert().Nil(opErr)
	s.Assert().True(res.IsStopWithRequeue())
}

func (s *ProvidersecretTestSuite) TestWrongScheme() {
	instance := &pmcorev1alpha1.PlatformMesh{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PlatformMesh",
			APIVersion: "core.platform-mesh.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Kcp: pmcorev1alpha1.Kcp{
				ProviderConnections: []pmcorev1alpha1.ProviderConnection{
					{
						EndpointSliceName: ptr.To("test-endpoint"),
						Path:              "root:platform-mesh-system",
						Secret:            "test-secret",
					},
				},
			},
		},
		Status: pmcorev1alpha1.PlatformMeshStatus{
			KcpWorkspaces: []pmcorev1alpha1.KcpWorkspace{
				{
					Name:  "root:platform-mesh-system",
					Phase: "Ready",
				},
				{
					Name:  "root:orgs",
					Phase: "Ready",
				},
			},
		},
	}

	// mocks
	mockK8sClient := new(mocks.Client)
	mockK8sClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	mockK8sClient.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	// return nil scheme
	mockK8sClient.EXPECT().Scheme().Return(nil).Maybe()

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{
				{
					URL: "http://url",
				},
			},
		},
	}

	mockK8sClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			switch obj := o.(type) {
			case *kcpapisv1alpha1.APIExportEndpointSlice:
				*obj = *slice
				return nil
			case *unstructured.Unstructured:
				// do nothing
				return nil
			default:
				return fmt.Errorf("unexpected type %T", o)
			}
		}).Once()

	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).
		Return(mockK8sClient, nil).Once()
	mockK8sClient.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).Return(nil)
	mockK8sClient.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = corev1.Secret{
				Data: map[string][]byte{
					"kubeconfig": secretKubeconfigData,
				},
			}
			return nil
		},
	).Once()

	// s.testObj.kcpHelper = mockedKcpHelper
	s.testObj = NewProviderSecretSubroutine(mockK8sClient, mockedKcpHelper, fakeHelm{ready: true}, "")

	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}

	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg)
	res, opErr := s.testObj.Process(ctx, instance)
	s.Require().Nil(opErr)
	s.Assert().True(res.IsStopWithRequeue())
	s.Assert().Contains(res.Message(), "scheme")
}

func (s *ProvidersecretTestSuite) TestErrorCreatingSecret() {
	instance := &pmcorev1alpha1.PlatformMesh{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PlatformMesh",
			APIVersion: "core.platform-mesh.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Kcp: pmcorev1alpha1.Kcp{
				ProviderConnections: []pmcorev1alpha1.ProviderConnection{
					{
						EndpointSliceName: ptr.To("test-endpoint"),
						Path:              "root:platform-mesh-system",
						Secret:            "test-secret",
					},
				},
			},
		},
		Status: pmcorev1alpha1.PlatformMeshStatus{
			KcpWorkspaces: []pmcorev1alpha1.KcpWorkspace{
				{Name: "root:platform-mesh-system", Phase: "Ready"},
			},
		},
	}

	// Setup test secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": secretKubeconfigData,
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{
				{URL: "http://url"},
			},
		},
	}

	// Mocks
	mockClient := new(mocks.Client)
	mockScheme := runtime.NewScheme()

	// Expect scheme call for SetOwnerReference
	mockClient.EXPECT().
		Scheme().
		Return(mockScheme).
		Maybe()

	// Simulate that secret doesn't exist, so Create is triggered
	mockClient.EXPECT().
		Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
			return key.Name == "test-secret"
		}), mock.Anything).
		Return(apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Secret"}, "test-secret")).
		Once()

	mockClient.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()
	mockClient.EXPECT().
		Get(mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				if key.Namespace == "platform-mesh-system" {
					switch key.Name {
					case "account-operator-kubeconfig",
						"rebac-authz-webhook-kubeconfig",
						"security-operator-kubeconfig",
						"kubernetes-graphql-gateway-kubeconfig",
						"extension-manager-operator-kubeconfig",
						"portal-kubeconfig",
						"cluster-admin-secret":
						return true
					}
				}
				return false
			}),
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *secret
			return nil
		})

	// Simulate error on Create
	mockClient.EXPECT().
		Create(mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("error creating secret")).
		Once()

	// Mock KCP client and its Get call for EndpointSlice
	mockedKcpClient := new(mocks.Client)
	mockedKcpClient.EXPECT().
		Get(mock.Anything, mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
			_, ok := obj.(*kcpapisv1alpha1.APIExportEndpointSlice)
			return ok
		})).
		RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			*obj.(*kcpapisv1alpha1.APIExportEndpointSlice) = *slice
			return nil
		}).
		Once()

	// Mock KcpHelper
	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).
		Return(mockedKcpClient, nil).Once()
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).Return(nil)
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = corev1.Secret{
				Data: map[string][]byte{
					"kubeconfig": secretKubeconfigData,
				},
			}
			return nil
		},
	).Once()

	// Run
	s.testObj = NewProviderSecretSubroutine(mockClient, mockedKcpHelper, fakeHelm{ready: true}, "example.com")

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	operatorCfg.KCP.ClusterAdminSecretName = "cluster-admin-secret"
	operatorCfg.KCP.Namespace = "platform-mesh-system"

	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg) // Add this line
	res, opErr := s.testObj.Process(ctx, instance)

	// Asserts
	s.Require().NotNil(opErr, "expected opErr to not be nil")
	s.Assert().Error(opErr, "expected error from operator")
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *ProvidersecretTestSuite) TestFailedBuilidingKubeconfig() {
	instance := &pmcorev1alpha1.PlatformMesh{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PlatformMesh",
			APIVersion: "core.platform-mesh.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Kcp: pmcorev1alpha1.Kcp{
				ProviderConnections: []pmcorev1alpha1.ProviderConnection{
					{
						EndpointSliceName: ptr.To("test-endpoint"),
						Path:              "root:platform-mesh-system",
						Secret:            "test-secret",
					},
				},
			},
		},
		Status: pmcorev1alpha1.PlatformMeshStatus{
			KcpWorkspaces: []pmcorev1alpha1.KcpWorkspace{
				{
					Name:  "root:platform-mesh-system",
					Phase: "Ready",
				},
				{
					Name:  "root:orgs",
					Phase: "Ready",
				},
			},
		},
	}

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()

	// mocks
	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{
				{
					URL: "http://url",
				},
			},
		},
	}

	mockKcpClient := new(mocks.Client)
	mockKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			*obj.(*kcpapisv1alpha1.APIExportEndpointSlice) = *slice
			return nil
		}).Once()

	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(mockKcpClient, nil).Once()
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).Return(nil)
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = corev1.Secret{
				Data: map[string][]byte{
					"kubeconfig": []byte("invalid"),
				},
			}
			return nil
		},
	).Once()

	// s.testObj.kcpHelper = mockedKcpHelper
	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "")

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}

	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg) // Add this line
	res, opErr := s.testObj.Process(ctx, instance)
	_ = opErr
	_ = res

	// assert
	s.Assert().Error(opErr, "Failed to build config from kubeconfig string")
	s.Assert().Equal(res, subroutines.OK())
}

func (s *ProvidersecretTestSuite) TestErrorGettingSecret() {
	instance := &pmcorev1alpha1.PlatformMesh{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PlatformMesh",
			APIVersion: "core.platform-mesh.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Kcp: pmcorev1alpha1.Kcp{
				ProviderConnections: []pmcorev1alpha1.ProviderConnection{
					{
						EndpointSliceName: ptr.To("test-endpoint"),
						Path:              "root:platform-mesh-system",
						Secret:            "test-secret",
					},
				},
			},
		},
		Status: pmcorev1alpha1.PlatformMeshStatus{
			KcpWorkspaces: []pmcorev1alpha1.KcpWorkspace{
				{
					Name:  "root:platform-mesh-system",
					Phase: "Ready",
				},
				{
					Name:  "root:orgs",
					Phase: "Ready",
				},
			},
		},
	}

	// mock client.Get
	secret := corev1.Secret{
		Data: map[string][]byte{
			"kubeconfig": []byte("invalid"),
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}

	s.clientMock.EXPECT().Get(
		mock.Anything, mock.Anything, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = secret
			return errors.New("error getting secret")
		}).Once()

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(context.Background(), keys.ConfigCtxKey, operatorCfg) // Add this line
	ctx = context.WithValue(ctx, keys.LoggerCtxKey, s.log)
	res, opErr := s.testObj.Process(ctx, instance)

	// assert
	s.Assert().Error(opErr, "Failed to build kubeconfig")
	s.Assert().Equal(res, subroutines.OK())
}

func (s *ProvidersecretTestSuite) TestFinalizers() {
	res := s.testObj.Finalizers(s.getBaseInstance())
	s.Assert().Equal(res, []string{ProvidersecretSubroutineFinalizer})
}

func (s *ProvidersecretTestSuite) TestGetName() {
	res := s.testObj.GetName()
	s.Assert().Equal(res, ProvidersecretSubroutineName)
}

func (s *ProvidersecretTestSuite) TestConstructor() {
	helper := &Helper{}
	s.testObj = NewProviderSecretSubroutine(s.clientMock, helper, fakeHelm{ready: true}, "")
}

func (s *ProvidersecretTestSuite) TestFinalize() {
	res, err := s.testObj.Finalize(context.Background(), nil)
	s.Assert().Nil(err)
	s.Assert().Equal(res, subroutines.OK())
}

func (s *ProvidersecretTestSuite) getBaseInstance() *pmcorev1alpha1.PlatformMesh {
	return &pmcorev1alpha1.PlatformMesh{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PlatformMesh",
			APIVersion: "core.platform-mesh.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Kcp: pmcorev1alpha1.Kcp{
				ProviderConnections: []pmcorev1alpha1.ProviderConnection{
					{
						EndpointSliceName: ptr.To("test-endpoint"),
						Path:              "root:platform-mesh-system",
						Secret:            "provider-secret",
					},
				},
			},
		},
	}
}

func (s *ProvidersecretTestSuite) TestInvalidKubeconfig() {
	instance := s.getBaseInstance()
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": []byte("invalid kubeconfig data"),
		},
	}

	s.clientMock.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key types.NamespacedName) bool {
		return key.Name == "test-secret" && key.Namespace == "default"
	}), mock.Anything).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = secret
			return nil
		}).Once()

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()

	mockedKcpHelper := new(mocks.KcpHelper)
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = secret
			return nil
		},
	).Once()

	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "")

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(context.Background(), keys.ConfigCtxKey, operatorCfg) // Add this line
	ctx = context.WithValue(ctx, keys.LoggerCtxKey, s.log)
	res, opErr := s.testObj.Process(ctx, instance)
	s.Require().NotNil(opErr)
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *ProvidersecretTestSuite) TestErrorLoadingKubeconfig() {
	instance := s.getBaseInstance()
	badSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": []byte("invalid kubeconfig data"),
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": secretKubeconfigData,
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				if key.Namespace == "" {
					switch key.Name {
					case "account-operator-kubeconfig",
						"rebac-authz-webhook-kubeconfig",
						"security-operator-kubeconfig",
						"kubernetes-graphql-gateway-kubeconfig",
						"extension-manager-operator-kubeconfig",
						"portal-kubeconfig",
						"external-kubeconfig":
						return true
					}
				}
				return false
			}),
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *secret
			return nil
		})
	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *badSecret
			return nil
		})

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(context.Background(), keys.ConfigCtxKey, operatorCfg) // Add this line
	ctx = context.WithValue(ctx, keys.LoggerCtxKey, s.log)

	res, opErr := s.testObj.Process(ctx, instance)
	s.Require().NotNil(opErr)
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *ProvidersecretTestSuite) TestErrorCreatingKCPClient() {
	instance := s.getBaseInstance()
	badKubeconfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": secretKubeconfigData,
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": secretKubeconfigData,
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				if key.Namespace == "platform-mesh-system" {
					switch key.Name {
					case "account-operator-kubeconfig",
						"rebac-authz-webhook-kubeconfig",
						"security-operator-kubeconfig",
						"kubernetes-graphql-gateway-kubeconfig",
						"extension-manager-operator-kubeconfig",
						"portal-kubeconfig",
						"external-kubeconfig":
						return true
					}
				}
				return false
			}),
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *secret
			return nil
		})

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *badKubeconfigSecret
			return nil
		}).Once()

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()

	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).
		Return(nil, errors.New("failed to create KCP client")).Once()
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = *badKubeconfigSecret
			return nil
		},
	).Once()

	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "")

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(context.Background(), keys.ConfigCtxKey, operatorCfg) // Add this line
	ctx = context.WithValue(ctx, keys.LoggerCtxKey, s.log)

	res, opErr := s.testObj.Process(ctx, instance)

	s.Require().NotNil(opErr)
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *ProvidersecretTestSuite) TestErrorGettingAPIExportEndpointSlice() {
	instance := s.getBaseInstance()
	// mock getting rootShard and frontproxy
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": secretKubeconfigData,
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}
	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()
	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				if key.Namespace == "platform-mesh-system" {
					switch key.Name {
					case "account-operator-kubeconfig",
						"rebac-authz-webhook-kubeconfig",
						"security-operator-kubeconfig",
						"kubernetes-graphql-gateway-kubeconfig",
						"extension-manager-operator-kubeconfig",
						"portal-kubeconfig",
						"external-kubeconfig":
						return true
					}
				}
				return false
			}),
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			// *obj.(*corev1.Secret) = *secret
			return nil
		})

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *secret
			return nil
		}).Once()

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).Return(nil)

	mockedKcpClient := new(mocks.Client)
	mockedKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("failed to get APIExportEndpointSlice")).Once()

	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).
		Return(mockedKcpClient, nil).Once()
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = *secret
			return nil
		},
	).Once()

	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "")

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(context.Background(), keys.ConfigCtxKey, operatorCfg) // Add this line
	ctx = context.WithValue(ctx, keys.LoggerCtxKey, s.log)

	res, opErr := s.testObj.Process(ctx, instance)

	s.Require().NotNil(opErr)
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *ProvidersecretTestSuite) TestEmptyAPIExportEndpoints() {
	instance := s.getBaseInstance()
	// mock getting rootShard and frontproxy
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": secretKubeconfigData,
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}
	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()
	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				if key.Namespace == "platform-mesh-system" {
					switch key.Name {
					case "account-operator-kubeconfig",
						"rebac-authz-webhook-kubeconfig",
						"security-operator-kubeconfig",
						"kubernetes-graphql-gateway-kubeconfig",
						"extension-manager-operator-kubeconfig",
						"portal-kubeconfig",
						"external-kubeconfig":
						return true
					}
				}
				return false
			}),
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			// *obj.(*corev1.Secret) = *secret
			return nil
		})

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *secret
			return nil
		}).Once()

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).Return(nil)

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{},
		},
	}

	mockedKcpClient := new(mocks.Client)
	mockedKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*kcpapisv1alpha1.APIExportEndpointSlice) = *slice
			return nil
		}).Once()

	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).
		Return(mockedKcpClient, nil).Once()
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = *secret
			return nil
		},
	).Once()

	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "")

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(context.Background(), keys.ConfigCtxKey, operatorCfg) // Add this line
	ctx = context.WithValue(ctx, keys.LoggerCtxKey, s.log)

	res, opErr := s.testObj.Process(ctx, instance)
	s.Require().NotNil(opErr)
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *ProvidersecretTestSuite) TestInvalidEndpointURL() {
	instance := s.getBaseInstance()
	// mock getting rootShard and frontproxy
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": secretKubeconfigData,
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}
	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()
	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				if key.Namespace == "platform-mesh-system" {
					switch key.Name {
					case "account-operator-kubeconfig",
						"rebac-authz-webhook-kubeconfig",
						"security-operator-kubeconfig",
						"kubernetes-graphql-gateway-kubeconfig",
						"extension-manager-operator-kubeconfig",
						"portal-kubeconfig",
						"external-kubeconfig":
						return true
					}
				}
				return false
			}),
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			// *obj.(*corev1.Secret) = *secret
			return nil
		})

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *secret
			return nil
		}).Once()

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).Return(nil)

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{
				{URL: "://invalid-url"},
			},
		},
	}

	mockedKcpClient := new(mocks.Client)
	mockedKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*kcpapisv1alpha1.APIExportEndpointSlice) = *slice
			return nil
		}).Once()

	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).
		Return(mockedKcpClient, nil).Once()
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = *secret
			return nil
		},
	).Once()

	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "")

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(context.Background(), keys.ConfigCtxKey, operatorCfg) // Add this line
	ctx = context.WithValue(ctx, keys.LoggerCtxKey, s.log)

	res, opErr := s.testObj.Process(ctx, instance)
	s.Require().NotNil(opErr)
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *ProvidersecretTestSuite) TestContextNotFoundInKubeconfig() {
	instance := s.getBaseInstance()
	kubeconfig := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"test-cluster": {
				Server: "https://test-server",
			},
		},
		Contexts:       map[string]*clientcmdapi.Context{},
		CurrentContext: "non-existent-context",
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"test-user": {},
		},
	}

	kubeconfigBytes, err := clientcmd.Write(*kubeconfig)
	s.Require().NoError(err)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": kubeconfigBytes,
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *secret
			return nil
		}).Once()

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()
	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				if key.Namespace == "platform-mesh-system" {
					switch key.Name {
					case "account-operator-kubeconfig",
						"rebac-authz-webhook-kubeconfig",
						"security-operator-kubeconfig",
						"kubernetes-graphql-gateway-kubeconfig",
						"extension-manager-operator-kubeconfig",
						"portal-kubeconfig",
						"external-kubeconfig":
						return true
					}
				}
				return false
			}),
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			// *obj.(*corev1.Secret) = *secret
			return nil
		})

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{
				{URL: "http://example.com"},
			},
		},
	}

	mockedKcpClient := new(mocks.Client)
	mockedKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*kcpapisv1alpha1.APIExportEndpointSlice) = *slice
			return nil
		}).Once()

	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).
		Return(mockedKcpClient, nil).Once()
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = *secret
			return nil
		},
	).Once()

	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "")

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(context.Background(), keys.ConfigCtxKey, operatorCfg) // Add this line
	ctx = context.WithValue(ctx, keys.LoggerCtxKey, s.log)

	res, opErr := s.testObj.Process(ctx, instance)
	s.Require().NotNil(opErr)
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *ProvidersecretTestSuite) TestClusterNotFoundInKubeconfig() {
	instance := s.getBaseInstance()
	kubeconfig := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{},
		Contexts: map[string]*clientcmdapi.Context{
			"test-context": {
				Cluster:  "non-existent-cluster",
				AuthInfo: "test-user",
			},
		},
		CurrentContext: "test-context",
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"test-user": {},
		},
	}

	kubeconfigBytes, err := clientcmd.Write(*kubeconfig)
	s.Require().NoError(err)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": kubeconfigBytes,
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}

	// Mock the Helm release lookup
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp", Namespace: "default"}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			release := obj.(*unstructured.Unstructured)
			release.Object = map[string]any{
				"status": map[string]any{
					"conditions": []any{
						map[string]any{
							"type":   "Ready",
							"status": "True",
						},
					},
				},
			}
			return nil
		})

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *secret
			return nil
		}).Once()

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()
	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				if key.Namespace == "platform-mesh-system" {
					switch key.Name {
					case "account-operator-kubeconfig",
						"rebac-authz-webhook-kubeconfig",
						"security-operator-kubeconfig",
						"kubernetes-graphql-gateway-kubeconfig",
						"extension-manager-operator-kubeconfig",
						"portal-kubeconfig",
						"external-kubeconfig":
						return true
					}
				}
				return false
			}),
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			// *obj.(*corev1.Secret) = *secret
			return nil
		})

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{
				{URL: "http://example.com"},
			},
		},
	}

	mockedKcpClient := new(mocks.Client)
	mockedKcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*kcpapisv1alpha1.APIExportEndpointSlice) = *slice
			return nil
		}).Once()

	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).
		Return(mockedKcpClient, nil).Once()
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = *secret
			return nil
		},
	).Once()

	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "")

	// Add the missing operator config context
	operatorCfg := config.OperatorConfig{
		KCP: config.OperatorConfig{}.KCP,
	}
	ctx := context.WithValue(context.Background(), keys.ConfigCtxKey, operatorCfg) // Add this line
	ctx = context.WithValue(ctx, keys.LoggerCtxKey, s.log)

	res, opErr := s.testObj.Process(ctx, instance)
	s.Require().NotNil(opErr)
	s.Assert().Equal(subroutines.OK(), res)
}

func (s *ProvidersecretTestSuite) TestHandleProviderConnections() {
	// Setup test instance
	instance := s.getBaseInstance()
	// Exercise admin kubeconfig wiring only: defaults may use scoped kubeconfig for some secrets.
	adminDefaults := make([]pmcorev1alpha1.ProviderConnection, len(DefaultProviderConnections))
	for i, pc := range DefaultProviderConnections {
		pc.AdminAuth = ptr.To(true)
		adminDefaults[i] = pc
	}
	instance.Spec.Kcp.ProviderConnections = adminDefaults
	instance.Spec.Kcp.ExtraProviderConnections = []pmcorev1alpha1.ProviderConnection{
		{
			AdminAuth:         ptr.To(true),
			EndpointSliceName: ptr.To(""),
			Path:              "root:platform-mesh-system",
			Secret:            "external-kubeconfig",
			External:          true,
			Namespace:         ptr.To("test"),
		},
	}
	instance.Spec.Exposure = &pmcorev1alpha1.ExposureConfig{
		BaseDomain: "example.com",
		Port:       8443,
		Protocol:   "https",
	}

	// Setup test secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"kubeconfig": secretKubeconfigData,
			"ca.crt":     []byte("ZHVtbXlkYXRhCg=="),
			"tls.crt":    []byte("ZHVtbXlkYXRhCg=="),
			"tls.key":    []byte("ZHVtbXlkYXRhCg=="),
		},
	}

	// Setup mock expectations for Get
	s.clientMock.
		EXPECT().
		Get(
			mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				return key.Name == "test-secret" && key.Namespace == "test"
			}),
			mock.Anything,
		).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = *secret
			return nil
		}).
		Once()

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.Anything,
			mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			rootShard := &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}
			*obj.(*unstructured.Unstructured) = *rootShard
			return nil
		}).
		Twice()

	// Build expected secret keys dynamically from DefaultProviderConnections
	expectedSecretKeys := make(map[types.NamespacedName]bool)
	for _, pc := range DefaultProviderConnections {
		ns := "platform-mesh-system"
		if ptr.Deref(pc.Namespace, "") != "" {
			ns = *pc.Namespace
		}
		expectedSecretKeys[types.NamespacedName{Name: pc.Secret, Namespace: ns}] = true
	}
	expectedSecretKeys[types.NamespacedName{Name: "external-kubeconfig", Namespace: "test"}] = true

	s.clientMock.EXPECT().
		Get(mock.Anything,
			mock.MatchedBy(func(key types.NamespacedName) bool {
				return expectedSecretKeys[key]
			}),
			mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			return nil
		})
	s.clientMock.EXPECT().
		Update(mock.Anything,
			mock.Anything).
		RunAndReturn(func(_ context.Context, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.UpdateOption) error {
			sec := obj.(*corev1.Secret)
			if sec.Name == "external-kubeconfig" {
				if data, ok := sec.Data["kubeconfig"]; ok {
					cfg, err := clientcmd.Load(data)
					if err != nil {
						s.log.Error().Msgf("failed to parse kubeconfig: %v", err)
					} else {
						for _, c := range cfg.Clusters {
							if c != nil {
								if c.Server == "https://kcp.api.example.com:8443/clusters/root:platform-mesh-system" {
									return nil
								}
								return fmt.Errorf("unexpected server URL: %s", c.Server)
							}
							break
						}
					}
				}
			}
			return nil
		})

	// Setup mock KCP client
	mockedKcpClient := new(mocks.Client)
	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{
				{URL: "http://example.com"},
			},
		},
	}
	mockedKcpClient.
		EXPECT().
		Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*kcpapisv1alpha1.APIExportEndpointSlice) = *slice
			return nil
		}).
		Times(len(DefaultProviderConnections))

	// Setup mock KCP helper
	mockedKcpHelper := new(mocks.KcpHelper)
	mockedKcpHelper.
		EXPECT().
		NewKcpClient(mock.Anything, mock.Anything).
		Return(mockedKcpClient, nil).
		Times(len(DefaultProviderConnections))
	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, &corev1.Secret{}).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption,
		) error {
			*o.(*corev1.Secret) = *secret
			return nil
		},
	).Once()
	s.clientMock.EXPECT().Get(mock.Anything,
		types.NamespacedName{Name: "root-ca", Namespace: "platform-mesh-system"},
		mock.AnythingOfType("*v1.Secret")).
		Return(apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "secrets"}, "root-ca")).
		Times(len(DefaultProviderConnections) + 1) // default providers + one extra connection

	opCfg := config.NewOperatorConfig()
	s.clientMock.EXPECT().Get(mock.Anything,
		types.NamespacedName{Name: KcpOperatorAdminKubeconfigSecretName, Namespace: opCfg.KCP.Namespace},
		mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			*obj.(*corev1.Secret) = corev1.Secret{Data: map[string][]byte{"kubeconfig": secretKubeconfigData}}
			return nil
		}).Times(len(DefaultProviderConnections) + 1)

	// Setup mock expectations for each provider connection
	for _, pc := range DefaultProviderConnections {
		s.clientMock.
			EXPECT().
			Get(
				mock.Anything,
				types.NamespacedName{Name: pc.Secret, Namespace: instance.Namespace},
				mock.Anything,
			).
			Return(apierrors.NewNotFound(
				schema.GroupResource{Group: "", Resource: "secrets"},
				pc.Secret,
			)).
			Once()

		s.clientMock.
			EXPECT().
			Create(
				mock.Anything,
				mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
					sec, ok := obj.(*corev1.Secret)
					if !ok {
						s.log.Error().Msgf("expected a *corev1.Secret, got %T", obj)
						return false
					}
					if sec.Name != pc.Secret || sec.Namespace != instance.Namespace {
						s.log.Error().Msgf("Secret %s/%s; want %s/%s",
							sec.Namespace, sec.Name,
							instance.Namespace, pc.Secret)
						return false
					}
					data, ok := sec.Data["kubeconfig"]
					if !ok {
						s.log.Error().Msg("missing kubeconfig key")
						return false
					}
					cfg, err := clientcmd.Load(data)
					if err != nil {
						s.log.Error().Msgf("invalid kubeconfig: %v", err)
						return false
					}
					ctx := cfg.Contexts[cfg.CurrentContext]
					cluster := cfg.Clusters[ctx.Cluster]
					want := wantProviderKubeconfigServer(s.T(), instance, opCfg, pc, "example.com")
					if cluster.Server != want {
						s.log.Error().Msgf("server URL = %q; want %q", cluster.Server, want)
						return false
					}
					return true
				}),
				mock.Anything,
			).
			RunAndReturn(func(ctx context.Context, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.CreateOption) error {
				providerSecret := obj.(*corev1.Secret)
				err := controllerutil.SetOwnerReference(instance, providerSecret, s.clientMock.Scheme())
				s.NoError(err)
				return nil
			}).
			Once()
	}

	// Run test
	s.testObj = NewProviderSecretSubroutine(s.clientMock, mockedKcpHelper, fakeHelm{ready: true}, "example.com")

	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, opCfg)
	res, opErr := s.testObj.Process(ctx, instance)
	s.Require().Nil(opErr)
	s.Assert().Equal(subroutines.OK(), res)
}
