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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

type DeploymentProcessTestSuite struct {
	suite.Suite
	scheme *runtime.Scheme
	log    *logger.Logger
	tmpDir string
}

func TestDeploymentProcessTestSuite(t *testing.T) {
	suite.Run(t, new(DeploymentProcessTestSuite))
}

func (s *DeploymentProcessTestSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	s.Require().NoError(clientgoscheme.AddToScheme(s.scheme))
	s.Require().NoError(pmcorev1alpha1.AddToScheme(s.scheme))
	logCfg := logger.DefaultConfig()
	logCfg.Level = "debug"
	logCfg.NoJSON = true
	logCfg.Name = "DeploymentProcessTest"
	s.log, _ = logger.New(logCfg)
}

func (s *DeploymentProcessTestSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	s.setupGotemplates()
}

// setupGotemplates creates a minimal gotemplates directory structure with simple
// templates that render valid YAML objects the fake client can handle.
func (s *DeploymentProcessTestSuite) setupGotemplates() {
	dirs := []string{
		"gotemplates/infra/infra/cert-manager",
		"gotemplates/infra/runtime/cert-manager",
		"gotemplates/components/infra",
		"gotemplates/components/runtime",
		"manifests/k8s/rebac-auth-webhook",
	}
	for _, d := range dirs {
		s.Require().NoError(os.MkdirAll(filepath.Join(s.tmpDir, d), 0o755))
	}

	// Infra template: renders a ConfigMap (FluxCD path — file starts with "helmrelease")
	infraHelmRelease := `{{- if .certManager.enabled }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: cert-manager-rendered
  namespace: {{ .helmReleaseNamespace }}
data:
  rendered: "true"
{{- end }}
`
	s.writeFile("gotemplates/infra/infra/cert-manager/helmrelease.yaml", infraHelmRelease)

	// ArgoCD template for infra (file starts with "application")
	infraArgoApp := `{{- if .certManager.enabled }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: cert-manager-argo-rendered
  namespace: {{ .helmReleaseNamespace }}
data:
  rendered: "true"
{{- end }}
`
	s.writeFile("gotemplates/infra/infra/cert-manager/application.yaml", infraArgoApp)

	// Runtime template: renders an OCM Resource-like ConfigMap
	runtimeTemplate := `{{- if .certManager.enabled }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: cert-manager-runtime-rendered
  namespace: {{ .releaseNamespace }}
data:
  rendered: "true"
{{- end }}
`
	s.writeFile("gotemplates/infra/runtime/cert-manager/resource.yaml", runtimeTemplate)

	// Components infra template (empty conditional to avoid errors)
	componentsInfra := `{{- if .values }}
{{- end }}
`
	s.writeFile("gotemplates/components/infra/helmreleases.yaml", componentsInfra)

	// Components runtime template (empty conditional)
	componentsRuntime := `{{- if .values }}
{{- end }}
`
	s.writeFile("gotemplates/components/runtime/ocm-chart-resources.yaml", componentsRuntime)

	// Webhook manifests required by manageAuthorizationWebhookSecrets
	selfSignedIssuer := `apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: rebac-authz-webhook-selfsigned-issuer
  namespace: {{ .namespace }}
spec:
  selfSigned: {}
`
	s.writeFile("manifests/k8s/rebac-auth-webhook/selfsigned-issuer.yaml", selfSignedIssuer)

	caCert := `apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: rebac-authz-webhook-ca
  namespace: {{ .namespace }}
spec:
  isCA: true
  secretName: rebac-authz-webhook-ca
  issuerRef:
    name: rebac-authz-webhook-selfsigned-issuer
`
	s.writeFile("manifests/k8s/rebac-auth-webhook/ca-cert.yaml", caCert)

	caIssuer := `apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: rebac-authz-webhook-ca-issuer
  namespace: {{ .namespace }}
spec:
  ca:
    secretName: rebac-authz-webhook-ca
`
	s.writeFile("manifests/k8s/rebac-auth-webhook/ca-issuer.yaml", caIssuer)

	webhookCert := `apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: rebac-authz-webhook-cert
  namespace: {{ .namespace }}
spec:
  secretName: rebac-authz-webhook-cert
  issuerRef:
    name: rebac-authz-webhook-ca-issuer
`
	s.writeFile("manifests/k8s/rebac-auth-webhook/webhook-cert.yaml", webhookCert)

	kcpWebhookSecret := `apiVersion: v1
kind: Secret
metadata:
  name: kcp-webhook-secret
  namespace: {{ .namespace }}
type: Opaque
stringData:
  kubeconfig: "placeholder"
`
	s.writeFile("manifests/k8s/rebac-auth-webhook/kcp-webhook-secret.yaml", kcpWebhookSecret)
}

func (s *DeploymentProcessTestSuite) writeFile(relPath, content string) {
	fullPath := filepath.Join(s.tmpDir, relPath)
	s.Require().NoError(os.WriteFile(fullPath, []byte(content), 0o644))
}

func (s *DeploymentProcessTestSuite) newContext(operatorCfg config.OperatorConfig) context.Context {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg)
	return ctx
}

func (s *DeploymentProcessTestSuite) newOperatorConfig() config.OperatorConfig {
	return config.OperatorConfig{
		WorkspaceDir: s.tmpDir,
		KCP: config.KCPConfig{
			Namespace:      "platform-mesh-system",
			RootShardName:  "root",
			FrontProxyName: "frontproxy",
			FrontProxyPort: "8443",
		},
		Subroutines: config.SubroutinesConfig{
			Deployment: config.DeploymentSubroutineConfig{
				Enabled:     true,
				EnableIstio: false,
			},
		},
	}
}

const testProfileFluxCD = `
infra:
  deploymentTechnology: fluxcd
  certManager:
    enabled: true
    name: cert-manager
    targetNamespace: cert-manager
components:
  services: {}
`

const testProfileArgoCD = `
infra:
  deploymentTechnology: argocd
  certManager:
    enabled: true
    name: cert-manager
    targetNamespace: cert-manager
components:
  services: {}
`

const testProfileArgoCDWithDeploymentNamespace = `
infra:
  deploymentTechnology: argocd
  deploymentNamespace: custom-deploy-ns
  certManager:
    enabled: true
    name: cert-manager
    targetNamespace: cert-manager
components:
  services: {}
`

func (s *DeploymentProcessTestSuite) newFluxCDReadyCertManager(namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"})
	obj.SetName("cert-manager")
	obj.SetNamespace(namespace)
	_ = unstructured.SetNestedSlice(obj.Object, []any{
		map[string]any{"type": "Ready", "status": "True"},
	}, "status", "conditions")
	return obj
}

func (s *DeploymentProcessTestSuite) newArgoCDReadyCertManager(namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"})
	obj.SetName("cert-manager")
	obj.SetNamespace(namespace)
	_ = unstructured.SetNestedField(obj.Object, "Synced", "status", "sync", "status")
	_ = unstructured.SetNestedField(obj.Object, "Healthy", "status", "health", "status")
	return obj
}

func (s *DeploymentProcessTestSuite) newReadyRootShard(namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "RootShard"})
	obj.SetName("root")
	obj.SetNamespace(namespace)
	_ = unstructured.SetNestedSlice(obj.Object, []any{
		map[string]any{"type": "Available", "status": "True"},
	}, "status", "conditions")
	return obj
}

func (s *DeploymentProcessTestSuite) newReadyFrontProxy(namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "FrontProxy"})
	obj.SetName("frontproxy")
	obj.SetNamespace(namespace)
	_ = unstructured.SetNestedSlice(obj.Object, []any{
		map[string]any{"type": "Available", "status": "True"},
	}, "status", "conditions")
	return obj
}

func (s *DeploymentProcessTestSuite) newEstablishedCRD(name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
	obj.SetName(name)
	_ = unstructured.SetNestedSlice(obj.Object, []any{
		map[string]any{"type": "Established", "status": "True"},
	}, "status", "conditions")
	return obj
}

func (s *DeploymentProcessTestSuite) seedCertManagerCRDs(ctx context.Context, cl ctrlruntimeclient.Client) {
	s.Require().NoError(cl.Create(ctx, s.newEstablishedCRD("issuers.cert-manager.io")))
	s.Require().NoError(cl.Create(ctx, s.newEstablishedCRD("certificates.cert-manager.io")))
}

func (s *DeploymentProcessTestSuite) Test_Process_FluxCD_HappyPath() {
	ns := "platform-mesh-system"
	operatorCfg := s.newOperatorConfig()
	ctx := s.newContext(operatorCfg)

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Exposure: &pmcorev1alpha1.ExposureConfig{
				BaseDomain: "localhost",
				Port:       8443,
				Protocol:   "https",
			},
		},
	}

	profileCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh-profile", Namespace: ns},
		Data:       map[string]string{profileConfigMapKey: testProfileFluxCD},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(inst, profileCM).
		WithStatusSubresource(inst).
		Build()

	// Pre-create the unstructured resources the fake client needs to return on Get
	s.Require().NoError(cl.Create(ctx, s.newFluxCDReadyCertManager(ns)))
	s.Require().NoError(cl.Create(ctx, s.newReadyRootShard(ns)))
	s.Require().NoError(cl.Create(ctx, s.newReadyFrontProxy(ns)))
	s.seedCertManagerCRDs(ctx, cl)

	sub := &DeploymentSubroutine{
		clientRuntime:            cl,
		clientInfra:              cl,
		cfg:                      &pmconfig.CommonServiceConfig{IsLocal: true},
		cfgOperator:              &operatorCfg,
		gotemplatesInfraDir:      filepath.Join(s.tmpDir, "gotemplates/infra"),
		gotemplatesComponentsDir: filepath.Join(s.tmpDir, "gotemplates/components"),
		workspaceDirectory:       filepath.Join(s.tmpDir, "manifests/k8s"),
	}

	result, err := sub.Process(ctx, inst)

	s.NoError(err)
	s.True(result.IsContinue(), "expected OK/continue result, got stop")
}

func (s *DeploymentProcessTestSuite) Test_Process_ArgoCD_HappyPath() {
	ns := "platform-mesh-system"
	operatorCfg := s.newOperatorConfig()
	ctx := s.newContext(operatorCfg)

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Exposure: &pmcorev1alpha1.ExposureConfig{
				BaseDomain: "localhost",
				Port:       8443,
				Protocol:   "https",
			},
		},
	}

	profileCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh-profile", Namespace: ns},
		Data:       map[string]string{profileConfigMapKey: testProfileArgoCD},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(inst, profileCM).
		WithStatusSubresource(inst).
		Build()

	s.Require().NoError(cl.Create(ctx, s.newArgoCDReadyCertManager(ns)))
	s.Require().NoError(cl.Create(ctx, s.newReadyRootShard(ns)))
	s.Require().NoError(cl.Create(ctx, s.newReadyFrontProxy(ns)))
	s.seedCertManagerCRDs(ctx, cl)

	sub := &DeploymentSubroutine{
		clientRuntime:            cl,
		clientInfra:              cl,
		cfg:                      &pmconfig.CommonServiceConfig{IsLocal: true},
		cfgOperator:              &operatorCfg,
		gotemplatesInfraDir:      filepath.Join(s.tmpDir, "gotemplates/infra"),
		gotemplatesComponentsDir: filepath.Join(s.tmpDir, "gotemplates/components"),
		workspaceDirectory:       filepath.Join(s.tmpDir, "manifests/k8s"),
	}

	result, err := sub.Process(ctx, inst)

	s.NoError(err)
	s.True(result.IsContinue(), "expected OK/continue result, got stop")
}

func (s *DeploymentProcessTestSuite) Test_Process_CertManagerCRDsNotEstablished_FluxCD() {
	ns := "platform-mesh-system"
	operatorCfg := s.newOperatorConfig()
	ctx := s.newContext(operatorCfg)

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Exposure: &pmcorev1alpha1.ExposureConfig{BaseDomain: "localhost", Port: 8443, Protocol: "https"},
		},
	}

	profileCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh-profile", Namespace: ns},
		Data:       map[string]string{profileConfigMapKey: testProfileFluxCD},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(inst, profileCM).
		WithStatusSubresource(inst).
		Build()
	s.Require().NoError(cl.Create(ctx, s.newFluxCDReadyCertManager(ns)))
	s.Require().NoError(cl.Create(ctx, s.newReadyRootShard(ns)))
	s.Require().NoError(cl.Create(ctx, s.newReadyFrontProxy(ns)))
	// cert-manager CRDs are NOT seeded — Process must stop and requeue.

	sub := &DeploymentSubroutine{
		clientRuntime:            cl,
		clientInfra:              cl,
		cfg:                      &pmconfig.CommonServiceConfig{IsLocal: true},
		cfgOperator:              &operatorCfg,
		gotemplatesInfraDir:      filepath.Join(s.tmpDir, "gotemplates/infra"),
		gotemplatesComponentsDir: filepath.Join(s.tmpDir, "gotemplates/components"),
		workspaceDirectory:       filepath.Join(s.tmpDir, "manifests/k8s"),
	}

	result, err := sub.Process(ctx, inst)

	s.NoError(err)
	s.False(result.IsContinue(), "expected StopWithRequeue when cert-manager CRDs are not established")
}

func (s *DeploymentProcessTestSuite) Test_Process_MissingProfile() {
	ns := "platform-mesh-system"
	operatorCfg := s.newOperatorConfig()
	ctx := s.newContext(operatorCfg)

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Exposure: &pmcorev1alpha1.ExposureConfig{BaseDomain: "localhost", Port: 8443, Protocol: "https"},
		},
	}

	// No profile ConfigMap — should fail on template rendering
	cl := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(inst).
		WithStatusSubresource(inst).
		Build()

	sub := &DeploymentSubroutine{
		clientRuntime:            cl,
		clientInfra:              cl,
		cfg:                      &pmconfig.CommonServiceConfig{IsLocal: true},
		cfgOperator:              &operatorCfg,
		gotemplatesInfraDir:      filepath.Join(s.tmpDir, "gotemplates/infra"),
		gotemplatesComponentsDir: filepath.Join(s.tmpDir, "gotemplates/components"),
		workspaceDirectory:       filepath.Join(s.tmpDir, "manifests/k8s"),
	}

	result, err := sub.Process(ctx, inst)

	s.Error(err)
	s.NotNil(result)
}

func (s *DeploymentProcessTestSuite) Test_Process_RootShardNotReady() {
	ns := "platform-mesh-system"
	operatorCfg := s.newOperatorConfig()
	ctx := s.newContext(operatorCfg)

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Exposure: &pmcorev1alpha1.ExposureConfig{BaseDomain: "localhost", Port: 8443, Protocol: "https"},
		},
	}

	profileCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh-profile", Namespace: ns},
		Data:       map[string]string{profileConfigMapKey: testProfileFluxCD},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(inst, profileCM).
		WithStatusSubresource(inst).
		Build()

	// cert-manager ready but NO RootShard
	s.Require().NoError(cl.Create(ctx, s.newFluxCDReadyCertManager(ns)))

	sub := &DeploymentSubroutine{
		clientRuntime:            cl,
		clientInfra:              cl,
		cfg:                      &pmconfig.CommonServiceConfig{IsLocal: true},
		cfgOperator:              &operatorCfg,
		gotemplatesInfraDir:      filepath.Join(s.tmpDir, "gotemplates/infra"),
		gotemplatesComponentsDir: filepath.Join(s.tmpDir, "gotemplates/components"),
		workspaceDirectory:       filepath.Join(s.tmpDir, "manifests/k8s"),
	}

	result, err := sub.Process(ctx, inst)

	s.NoError(err)
	s.False(result.IsContinue(), "expected StopWithRequeue when RootShard not found")
}

func (s *DeploymentProcessTestSuite) Test_Process_DeploymentNamespace() {
	ns := "platform-mesh-system"
	operatorCfg := s.newOperatorConfig()
	ctx := s.newContext(operatorCfg)

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			Exposure: &pmcorev1alpha1.ExposureConfig{
				BaseDomain: "localhost",
				Port:       8443,
				Protocol:   "https",
			},
		},
	}

	profileCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh-profile", Namespace: ns},
		Data:       map[string]string{profileConfigMapKey: testProfileArgoCDWithDeploymentNamespace},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(inst, profileCM).
		WithStatusSubresource(inst).
		Build()

	s.Require().NoError(cl.Create(ctx, s.newArgoCDReadyCertManager("custom-deploy-ns")))
	s.Require().NoError(cl.Create(ctx, s.newReadyRootShard(ns)))
	s.Require().NoError(cl.Create(ctx, s.newReadyFrontProxy(ns)))
	s.seedCertManagerCRDs(ctx, cl)

	sub := &DeploymentSubroutine{
		clientRuntime:            cl,
		clientInfra:              cl,
		cfg:                      &pmconfig.CommonServiceConfig{IsLocal: true},
		cfgOperator:              &operatorCfg,
		gotemplatesInfraDir:      filepath.Join(s.tmpDir, "gotemplates/infra"),
		gotemplatesComponentsDir: filepath.Join(s.tmpDir, "gotemplates/components"),
		workspaceDirectory:       filepath.Join(s.tmpDir, "manifests/k8s"),
	}

	result, err := sub.Process(ctx, inst)

	s.NoError(err)
	s.True(result.IsContinue(), "expected OK/continue result, got stop")

	// Verify the infra template rendered with helmReleaseNamespace = custom-deploy-ns
	var cm corev1.ConfigMap
	err = cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "cert-manager-argo-rendered", Namespace: "custom-deploy-ns"}, &cm)
	s.NoError(err, "ConfigMap should exist in custom-deploy-ns (the deploymentNamespace)")
	s.Equal("custom-deploy-ns", cm.Namespace)
}

func (s *DeploymentProcessTestSuite) newWebhookSecretFixture(ns string, tlsCrt, caCrt, trustedCA []byte) (ctrlruntimeclient.Client, *DeploymentSubroutine, context.Context) {
	operatorCfg := s.newOperatorConfig()
	operatorCfg.Subroutines.Deployment.AuthorizationWebhookSecretName = "kcp-webhook-secret"
	operatorCfg.Subroutines.Deployment.AuthorizationWebhookSecretCAName = "rebac-authz-webhook-cert"
	ctx := s.newContext(operatorCfg)

	kubeconfig, err := clientcmd.Write(clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"webhook": {Server: "https://rebac-authz-webhook:9443/authz", CertificateAuthorityData: trustedCA},
		},
		Contexts:       map[string]*clientcmdapi.Context{"webhook": {Cluster: "webhook"}},
		CurrentContext: "webhook",
	})
	s.Require().NoError(err)

	kcpPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "root-kcp-0", Namespace: ns, Labels: map[string]string{"app.kubernetes.io/name": "kcp"},
	}}
	cl := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "rebac-authz-webhook-cert", Namespace: ns},
				Data:       map[string][]byte{"tls.crt": tlsCrt, "ca.crt": caCrt},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "kcp-webhook-secret", Namespace: ns},
				Data:       map[string][]byte{"kubeconfig": kubeconfig},
			},
			kcpPod,
		).
		WithInterceptorFuncs(interceptor.Funcs{
			// The real client sets the kind of typed objects for apply patches; the fake one does not.
			Patch: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
				if secret, ok := obj.(*corev1.Secret); ok {
					secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	sub := &DeploymentSubroutine{clientRuntime: cl, clientInfra: cl, cfgOperator: &operatorCfg}
	return cl, sub, ctx
}

func (s *DeploymentProcessTestSuite) trustedWebhookCA(ctx context.Context, cl ctrlruntimeclient.Client, ns string) []byte {
	secret := &corev1.Secret{}
	s.Require().NoError(cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "kcp-webhook-secret", Namespace: ns}, secret))
	kubeconfig, err := clientcmd.Load(secret.Data["kubeconfig"])
	s.Require().NoError(err)
	return kubeconfig.Clusters["webhook"].CertificateAuthorityData
}

func (s *DeploymentProcessTestSuite) Test_UpdateKcpWebhookSecret_SyncsNewCAAndRestartsKcp() {
	ns := "platform-mesh-system"
	cl, sub, ctx := s.newWebhookSecretFixture(ns, []byte("leaf"), []byte("new-ca"), []byte("old-ca"))
	inst := &pmcorev1alpha1.PlatformMesh{ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns}}

	result, err := sub.updateKcpWebhookSecret(ctx, inst)

	s.NoError(err)
	s.True(result.IsContinue())
	s.Zero(result.Requeue())
	s.Equal([]byte("new-ca"), s.trustedWebhookCA(ctx, cl, ns))
	err = cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "root-kcp-0", Namespace: ns}, &corev1.Pod{})
	s.True(apierrors.IsNotFound(err), "kcp pods must restart to load the new CA")
}

func (s *DeploymentProcessTestSuite) Test_UpdateKcpWebhookSecret_RequeuesWhileCertificateIsSelfSigned() {
	ns := "platform-mesh-system"
	cl, sub, ctx := s.newWebhookSecretFixture(ns, []byte("self-signed"), []byte("self-signed"), []byte("self-signed"))
	inst := &pmcorev1alpha1.PlatformMesh{ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns}}

	result, err := sub.updateKcpWebhookSecret(ctx, inst)

	s.NoError(err)
	s.True(result.IsContinue())
	s.Equal(DefaultRequeueInterval, result.Requeue())
	s.Equal([]byte("self-signed"), s.trustedWebhookCA(ctx, cl, ns))
	s.NoError(cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "root-kcp-0", Namespace: ns}, &corev1.Pod{}), "kcp must not restart when the CA is unchanged")
}

func (s *DeploymentProcessTestSuite) Test_UpdateKcpWebhookSecret_RequeuesWhenCertificateSecretIsMissing() {
	ns := "platform-mesh-system"
	cl, sub, ctx := s.newWebhookSecretFixture(ns, nil, nil, []byte("old-ca"))
	s.Require().NoError(cl.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rebac-authz-webhook-cert", Namespace: ns}}))
	inst := &pmcorev1alpha1.PlatformMesh{ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns}}

	result, err := sub.updateKcpWebhookSecret(ctx, inst)

	s.NoError(err)
	s.True(result.IsContinue())
	s.Equal(DefaultRequeueInterval, result.Requeue())
}

func (s *DeploymentProcessTestSuite) Test_ManageAuthorizationWebhookSecrets_IssuesServingCertificateFromCA() {
	ns := "platform-mesh-system"
	cl, sub, ctx := s.newWebhookSecretFixture(ns, []byte("leaf"), []byte("ca"), []byte("ca"))
	sub.workspaceDirectory = "../../manifests/k8s"
	inst := &pmcorev1alpha1.PlatformMesh{ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: ns}}

	_, err := sub.manageAuthorizationWebhookSecrets(ctx, inst)
	s.Require().NoError(err)

	get := func(kind, name string) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: kind})
		s.Require().NoError(cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: ns}, obj))
		return obj
	}
	field := func(obj *unstructured.Unstructured, path ...string) any {
		value, _, _ := unstructured.NestedFieldNoCopy(obj.Object, path...)
		return value
	}

	servingCert := get("Certificate", "rebac-authz-webhook-cert")
	caIssuer := get("Issuer", field(servingCert, "spec", "issuerRef", "name").(string))
	caCert := get("Certificate", "rebac-authz-webhook-ca")
	selfSignedIssuer := get("Issuer", field(caCert, "spec", "issuerRef", "name").(string))

	s.Equal(field(caCert, "spec", "secretName"), field(caIssuer, "spec", "ca", "secretName"))
	s.Equal(true, field(caCert, "spec", "isCA"))
	s.NotNil(field(selfSignedIssuer, "spec", "selfSigned"))
}
