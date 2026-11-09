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

package providers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/provider-operator/internal/config"
	"go.platform-mesh.io/provider-operator/pkg/subroutines/mocks"
	testfixtures "go.platform-mesh.io/provider-operator/pkg/subroutines/test"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

var secretKubeconfigData, _ = fs.ReadFile(testfixtures.Resources, "kubeconfig.yaml")

type KubeconfigCopyTestSuite struct {
	suite.Suite
	testObj       *KubeconfigCopySubroutine
	clientMock    *mocks.Client
	kcpHelperMock *mocks.KcpHelper
	kcpClientMock *mocks.Client
	scheme        *runtime.Scheme
	log           *logger.Logger
	operatorCfg   config.OperatorConfig
}

func TestKubeconfigCopyTestSuite(t *testing.T) {
	suite.Run(t, new(KubeconfigCopyTestSuite))
}

func (s *KubeconfigCopyTestSuite) SetupTest() {
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "KubeconfigCopyTestSuite"
	s.log, _ = logger.New(cfg)

	s.clientMock = new(mocks.Client)
	s.kcpHelperMock = new(mocks.KcpHelper)
	s.kcpClientMock = new(mocks.Client)

	s.scheme = runtime.NewScheme()
	s.clientMock.EXPECT().Scheme().Return(s.scheme).Maybe()

	s.operatorCfg = config.OperatorConfig{}
	s.operatorCfg.KCP.ClusterAdminSecretName = "kcp-admin"
	s.operatorCfg.KCP.Namespace = "platform-mesh-system"

	s.testObj = NewKubeconfigCopySubroutine(s.clientMock, s.kcpHelperMock, &s.operatorCfg, "https://kcp.api.example.com")
}

func (s *KubeconfigCopyTestSuite) TearDownTest() {
	s.clientMock = nil
	s.kcpHelperMock = nil
	s.kcpClientMock = nil
	s.testObj = nil
}

func (s *KubeconfigCopyTestSuite) newCtx() context.Context {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	return context.WithValue(ctx, keys.ConfigCtxKey, s.operatorCfg)
}

func (s *KubeconfigCopyTestSuite) newManagedProvider() *pmprovidersv1alpha1.ManagedProvider {
	return &pmprovidersv1alpha1.ManagedProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cowboys",
			Namespace: "providers-wildwest-ns",
		},
	}
}

func (s *KubeconfigCopyTestSuite) mockAdminSecret() {
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Name = "kcp-admin"
			secret.Namespace = "platform-mesh-system"
			secret.Data = map[string][]byte{
				"ca.crt":  []byte("fake-ca"),
				"tls.crt": []byte("fake-cert"),
				"tls.key": []byte("fake-key"),
			}
			return nil
		})
}

func (s *KubeconfigCopyTestSuite) mockProviderWithSecretRef() {
	// Default providerRefName = "cowboys" (inst.Name), providerRefPath = "root:providers:system"
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys"}, mock.AnythingOfType("*v1alpha1.Provider")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			provider := obj.(*pmprovidersv1alpha1.Provider)
			provider.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
				Name:      "cowboys-kubeconfig",
				Namespace: "kcp-side-ns",
			}
			// Must match providerKubeconfigSecretSpec("cowboys", "providers-wildwest-ns", nil)
			provider.Spec.ProviderKubeconfigSecret = &pmprovidersv1alpha1.KubeconfigSecretSpec{
				Name:      "cowboys-provider-kubeconfig",
				Namespace: "providers-wildwest-ns",
				Key:       "kubeconfig",
			}
			return nil
		})
}

func (s *KubeconfigCopyTestSuite) TestProcess_BuildKcpAdminConfigFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		Return(errors.New("connection refused"))

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to build kcp admin config")
}

func (s *KubeconfigCopyTestSuite) TestProcess_NewKcpClientFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	// default providerRefPath = "root:providers:system"
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(nil, errors.New("dial error"))

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to create kcp client")
}

func (s *KubeconfigCopyTestSuite) TestProcess_KubeconfigSecretRefNotSetYet() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys"}, mock.AnythingOfType("*v1alpha1.Provider")).
		Return(nil) // Provider found, Status.ProviderKubeconfigSecretRef is nil

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(pmprovidersv1alpha1.ManagedProviderPhaseCopyingKubeconfig, inst.Status.Phase)
}

func (s *KubeconfigCopyTestSuite) TestProcess_KubeconfigSecretGetFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.mockProviderWithSecretRef()
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys-kubeconfig", Namespace: "kcp-side-ns"}, mock.AnythingOfType("*v1.Secret")).
		Return(errors.New("secret fetch failed"))

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
}

func (s *KubeconfigCopyTestSuite) TestProcess_HappyPath() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.mockProviderWithSecretRef()
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys-kubeconfig", Namespace: "kcp-side-ns"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{"kubeconfig": secretKubeconfigData}
			return nil
		})
	// Ensure namespace "default" in runtime cluster (r.client, since RuntimeKubeconfigSecretName is empty)
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Namespace"), mock.Anything).
		Return(nil)
	// Default copy destination: Name = providerKubeconfigSecretName(inst.Name), Namespace = inst.Namespace
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys-provider-kubeconfig", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*v1.Secret")).
		Return(apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "cowboys-provider-kubeconfig"))
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
	s.Require().NotNil(inst.Status.ProviderKubeconfigSecretRef)
	s.Assert().Equal("cowboys-provider-kubeconfig", inst.Status.ProviderKubeconfigSecretRef.Name)
	s.Assert().Equal("providers-wildwest-ns", inst.Status.ProviderKubeconfigSecretRef.Namespace)
}

func (s *KubeconfigCopyTestSuite) TestProcess_CustomProviderReference() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.ProviderReference = &pmprovidersv1alpha1.ProviderReferenceSpec{
		Path: "root:custom:path",
		Name: "my-provider",
	}

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:custom:path").
		Return(nil, errors.New("stop here"))

	_, _ = s.testObj.Process(ctx, inst)

	s.kcpHelperMock.AssertExpectations(s.T())
}

func (s *KubeconfigCopyTestSuite) TestFinalize_NilKubeconfigRef() {
	// No ProviderKubeconfigSecretRef set (provider never reached Ready) → no-op.
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *KubeconfigCopyTestSuite) TestFinalize_DeletesSecret() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      "cowboys-provider-kubeconfig",
		Namespace: inst.Namespace,
	}

	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *KubeconfigCopyTestSuite) TestFinalize_SecretNotFound() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      "cowboys-provider-kubeconfig",
		Namespace: inst.Namespace,
	}

	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "cowboys-provider-kubeconfig"))

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *KubeconfigCopyTestSuite) TestFinalize_DeleteError() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      "cowboys-provider-kubeconfig",
		Namespace: inst.Namespace,
	}

	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(errors.New("delete failed"))

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
}

const (
	remoteRuntimeClusterHost    = "https://runtime.example.com"
	runtimeKubeconfigSecretName = "runtime-kubeconfig"
)

var (
	kcpKubeconfigKey    = types.NamespacedName{Name: "cowboys-kubeconfig", Namespace: "kcp-side-ns"}
	copiedKubeconfigKey = types.NamespacedName{Name: "cowboys-provider-kubeconfig", Namespace: "providers-wildwest-ns"}
)

type copyTarget int

const (
	toLocalCluster copyTarget = iota
	toRemoteCluster
)

type fakeKcpHelper struct {
	workspaces map[string]ctrlruntimeclient.Client
}

func (h fakeKcpHelper) NewKcpClient(_ *rest.Config, path string) (ctrlruntimeclient.Client, error) {
	cl, ok := h.workspaces[path]
	if !ok {
		return nil, fmt.Errorf("no kcp workspace %q", path)
	}
	return cl, nil
}

func fakeNewClient(
	clusters map[string]ctrlruntimeclient.Client,
) func(*rest.Config, ctrlruntimeclient.Options) (ctrlruntimeclient.Client, error) {
	return func(cfg *rest.Config, _ ctrlruntimeclient.Options) (ctrlruntimeclient.Client, error) {
		cl, ok := clusters[cfg.Host]
		if !ok {
			return nil, fmt.Errorf("no cluster at %s", cfg.Host)
		}
		return cl, nil
	}
}

type kubeconfigCopyEnv struct {
	local           ctrlruntimeclient.Client
	runtimeCluster  ctrlruntimeclient.Client
	managedProvider *pmprovidersv1alpha1.ManagedProvider
	reconcile       func() error
}

func (e kubeconfigCopyEnv) getManagedProvider(ctx context.Context) error {
	return e.local.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(e.managedProvider), &pmprovidersv1alpha1.ManagedProvider{})
}

func (e kubeconfigCopyEnv) getCopiedKubeconfig(ctx context.Context) error {
	return e.runtimeCluster.Get(ctx, copiedKubeconfigKey, &corev1.Secret{})
}

func (s *KubeconfigCopyTestSuite) reconcileUntilCopied(ctx context.Context, target copyTarget) kubeconfigCopyEnv {
	managedProvider := s.newManagedProvider()
	if target == toRemoteCluster {
		managedProvider.Spec.RuntimeKubeconfigSecretName = runtimeKubeconfigSecretName
	}

	local := s.newFakeClient(managedProvider, kcpAdminSecret(), s.runtimeKubeconfigSecret(managedProvider.Namespace))
	kcpWorkspace := s.newFakeClient(providerWithKubeconfigRef(), kcpKubeconfigSecret())
	remoteClient := s.newFakeClient()

	sub := NewKubeconfigCopySubroutine(local, fakeKcpHelper{workspaces: map[string]ctrlruntimeclient.Client{
		"root:providers:system": kcpWorkspace,
	}}, &s.operatorCfg, "https://kcp.api.example.com")
	sub.newClient = fakeNewClient(map[string]ctrlruntimeclient.Client{
		remoteRuntimeClusterHost: remoteClient,
	})

	env := kubeconfigCopyEnv{
		local:           local,
		runtimeCluster:  local,
		managedProvider: managedProvider,
		reconcile:       doReconcileFn(ctx, local, managedProvider, sub),
	}
	if target == toRemoteCluster {
		env.runtimeCluster = remoteClient
	}

	s.Require().NoError(env.reconcile(), "reconcile adding the finalizer")
	s.Require().NoError(env.reconcile(), "reconcile copying the kubeconfig")
	s.Require().NoError(env.getCopiedKubeconfig(ctx), "kubeconfig not copied to the runtime cluster")
	return env
}

// doReconcileFn returns one reconcile run over the ManagedProvider through the lifecycle manager.
func doReconcileFn(ctx context.Context, cl ctrlruntimeclient.Client, managedProvider *pmprovidersv1alpha1.ManagedProvider, sub *KubeconfigCopySubroutine) func() error {
	lifecycleManager := lifecycle.New(&fakeManager{client: cl}, "ManagedProviderReconciler", func() ctrlruntimeclient.Object {
		return &pmprovidersv1alpha1.ManagedProvider{}
	}, sub).WithConditions(conditions.NewManager())
	req := mcreconcile.Request{Request: reconcile.Request{NamespacedName: ctrlruntimeclient.ObjectKeyFromObject(managedProvider)}}
	return func() error {
		_, err := lifecycleManager.Reconcile(ctx, req)
		return err
	}
}

func (s *KubeconfigCopyTestSuite) newFakeClient(objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	scheme := runtime.NewScheme()
	s.Require().NoError(corev1.AddToScheme(scheme))
	s.Require().NoError(pmprovidersv1alpha1.AddToScheme(scheme))
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&pmprovidersv1alpha1.ManagedProvider{}).
		WithObjects(objs...).
		Build()
}

func kcpAdminSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "kcp-admin", Namespace: "platform-mesh-system"},
		Data:       map[string][]byte{"ca.crt": []byte("ca"), "tls.crt": []byte("cert"), "tls.key": []byte("key")},
	}
}

func (s *KubeconfigCopyTestSuite) runtimeKubeconfigSecret(namespace string) *corev1.Secret {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["runtime"] = &clientcmdapi.Cluster{Server: remoteRuntimeClusterHost}
	cfg.AuthInfos["runtime"] = &clientcmdapi.AuthInfo{Token: "runtime-token"}
	cfg.Contexts["runtime"] = &clientcmdapi.Context{Cluster: "runtime", AuthInfo: "runtime"}
	cfg.CurrentContext = "runtime"
	kubeconfig, err := clientcmd.Write(*cfg)
	s.Require().NoError(err)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: runtimeKubeconfigSecretName, Namespace: namespace},
		Data:       map[string][]byte{"kubeconfig": kubeconfig},
	}
}

func providerWithKubeconfigRef() *pmprovidersv1alpha1.Provider {
	return &pmprovidersv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{Name: "cowboys"},
		Spec: pmprovidersv1alpha1.ProviderSpec{
			ProviderKubeconfigSecret: &pmprovidersv1alpha1.KubeconfigSecretSpec{
				Name: copiedKubeconfigKey.Name, Namespace: copiedKubeconfigKey.Namespace, Key: "kubeconfig",
			},
		},
		Status: pmprovidersv1alpha1.ProviderStatus{
			ProviderKubeconfigSecretRef: &corev1.SecretReference{Name: kcpKubeconfigKey.Name, Namespace: kcpKubeconfigKey.Namespace},
		},
	}
}

func kcpKubeconfigSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: kcpKubeconfigKey.Name, Namespace: kcpKubeconfigKey.Namespace},
		Data:       map[string][]byte{"kubeconfig": []byte("provider workspace kubeconfig")},
	}
}

func (s *KubeconfigCopyTestSuite) TestDelete_RemovesCopiedKubeconfig() {
	s.Run("local runtime cluster", func() {
		s.Run("copy removed", func() {
			ctx := s.newCtx()
			env := s.reconcileUntilCopied(ctx, toLocalCluster)

			s.Require().NoError(env.local.Delete(ctx, env.managedProvider))
			s.Require().NoError(env.reconcile())

			s.Assert().True(apierrors.IsNotFound(env.getManagedProvider(ctx)), "ManagedProvider not deleted")
			s.Assert().True(apierrors.IsNotFound(env.getCopiedKubeconfig(ctx)), "copied kubeconfig left on the runtime cluster")
		})
	})

	s.Run("remote runtime cluster", func() {
		s.Run("copy removed", func() {
			ctx := s.newCtx()
			env := s.reconcileUntilCopied(ctx, toRemoteCluster)

			s.Require().NoError(env.local.Delete(ctx, env.managedProvider))
			s.Require().NoError(env.reconcile())

			s.Assert().True(apierrors.IsNotFound(env.getManagedProvider(ctx)), "ManagedProvider not deleted")
			s.Assert().True(apierrors.IsNotFound(env.getCopiedKubeconfig(ctx)), "copied kubeconfig left on the runtime cluster")
		})

		s.Run("waits while the runtime kubeconfig is missing", func() {
			ctx := s.newCtx()
			env := s.reconcileUntilCopied(ctx, toRemoteCluster)
			s.Require().NoError(env.local.Delete(ctx, s.runtimeKubeconfigSecret(env.managedProvider.Namespace)))

			s.Require().NoError(env.local.Delete(ctx, env.managedProvider))
			s.Require().Error(env.reconcile(), "deletion must wait until the runtime kubeconfig is restored")

			s.Assert().NoError(env.getManagedProvider(ctx), "ManagedProvider deleted without kubeconfig cleanup")
			s.Assert().NoError(env.getCopiedKubeconfig(ctx), "copied kubeconfig gone from the runtime cluster")
		})

		s.Run("waits while the runtime kubeconfig is unusable", func() {
			ctx := s.newCtx()
			env := s.reconcileUntilCopied(ctx, toRemoteCluster)
			unusable := s.runtimeKubeconfigSecret(env.managedProvider.Namespace)
			unusable.Data = nil
			s.Require().NoError(env.local.Update(ctx, unusable))

			s.Require().NoError(env.local.Delete(ctx, env.managedProvider))
			s.Require().Error(env.reconcile(), "deletion must wait until the runtime kubeconfig is fixed")

			s.Assert().NoError(env.getManagedProvider(ctx), "ManagedProvider deleted without kubeconfig cleanup")
			s.Assert().NoError(env.getCopiedKubeconfig(ctx), "copied kubeconfig gone from the runtime cluster")
		})
	})
}
