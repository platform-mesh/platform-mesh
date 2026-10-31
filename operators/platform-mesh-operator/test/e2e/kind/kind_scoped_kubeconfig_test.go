//go:build e2e

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

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Matches suite_kind_test runOperator KCP.Url and Kind object keys used elsewhere in this package.
const (
	kcpLocalFrontProxyURL = "https://kcp.api.portal.localhost:8443"

	e2ePlatformMeshNamespace = "platform-mesh-system"
	e2ePlatformMeshName      = "platform-mesh"

	e2eKcpClusterAdminClientCertSecretName = "kcp-cluster-admin-client-cert"

	// Operator-written Secrets for extraProviderConnections in platform-mesh.yaml (provider1 vs provider2 scenario).
	e2eScopedKubeconfigProvider1SecretName = "kind-e2e-provider1-scoped-kubeconfig"
	e2eScopedKubeconfigProvider2SecretName = "kind-e2e-provider2-scoped-kubeconfig"
	e2eAdminKubeconfigProvider3SecretName  = "kind-e2e-provider3-admin-kubeconfig"

	// APIExport / APIExportEndpointSlice name in yaml/kcp-provider-workspaces; kubectl resource for E2EProviderConfig.
	e2eKindScopedProviderExportName       = "kind-e2e-scoped-provider.platform-mesh.io"
	e2eScopedProviderConfigResource       = "e2eproviderconfigs.kind-e2e-scoped-provider.platform-mesh.io"
	e2eKindScopedProviderConfigAPIVersion = "kind-e2e-scoped-provider.platform-mesh.io/v1alpha1"

	// Match test/e2e/kind/yaml/platform-mesh-resource/platform-mesh.yaml extraProviderConnections[].path;
	// ProvidersecretSubroutine creates scoped SA + ClusterRole + ClusterRoleBinding in each provider workspace.
	e2eScopedKubeconfigProvider1Path = "root:providers:provider1"
	e2eScopedKubeconfigProvider2Path = "root:providers:provider2"

	e2eKcpProviderWorkspacesYAMLDir = "../../../test/e2e/kind/yaml/kcp-provider-workspaces"
)

// scopedE2EKcpAdminCertSecretNamespace is set by waitForKcpClusterAdminClientCert in this file only.
var scopedE2EKcpAdminCertSecretNamespace string

var scopedE2EKcpClusterAdminCertSearchNamespaces = []string{e2ePlatformMeshNamespace}

// TestScoped01KubeconfigKcpPrereq waits for kcp-cluster-admin-client-cert (TLS material for kcp clients) and seeds
// root:providers:provider1|provider2 with APIExports/slices before TestScoped02/03KubeconfigProvider1/2.
// Named TestScoped01… so go test lexicographic order runs it after kind_operator_test.go TestResourceReady / TestExtraWorkspaces
// (TestE < TestR < TestS); do not use Test01… here or it would run before TestResourceReady.
func (s *KindTestSuite) TestScoped01KubeconfigKcpPrereq() {
	s.logger.Info().Str("kind_e2e", "TestScoped01KubeconfigKcpPrereq").Msg("start")
	ctx := context.Background()
	s.waitForKcpClusterAdminClientCert(ctx)
	s.ensureScopedE2EKcpProviderWorkspaces(ctx)
	s.ensureScopedE2EProviderConnections(ctx)
	s.waitScopedProviderConnectionSecretsReady(ctx)
	s.logger.Info().Str("kind_e2e", "TestScoped01KubeconfigKcpPrereq").Msg("done")
}

func (s *KindTestSuite) ensureScopedE2EProviderConnections(ctx context.Context) {
	pm := &pmcorev1alpha1.PlatformMesh{}
	err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Name:      e2ePlatformMeshName,
		Namespace: e2ePlatformMeshNamespace,
	}, pm)
	s.Require().NoError(err, "get PlatformMesh for scoped e2e provider connections")

	desired := []pmcorev1alpha1.ProviderConnection{
		{
			Path:              e2eScopedKubeconfigProvider1Path,
			Secret:            e2eScopedKubeconfigProvider1SecretName,
			EndpointSliceName: ptr.To(e2eKindScopedProviderExportName),
			AdminAuth:         ptr.To(false),
		},
		{
			Path:           e2eScopedKubeconfigProvider2Path,
			Secret:         e2eScopedKubeconfigProvider2SecretName,
			APIExportNames: []string{e2eKindScopedProviderExportName},
			AdminAuth:      ptr.To(false),
		},
		{
			Path:              "root:platform-mesh-system",
			Secret:            e2eAdminKubeconfigProvider3SecretName,
			EndpointSliceName: ptr.To("core.platform-mesh.io"),
			AdminAuth:         ptr.To(true),
		},
	}

	currentBySecret := make(map[string]int, len(pm.Spec.Kcp.ExtraProviderConnections))
	for i, pc := range pm.Spec.Kcp.ExtraProviderConnections {
		currentBySecret[pc.Secret] = i
	}

	changed := false
	for _, d := range desired {
		if idx, ok := currentBySecret[d.Secret]; ok {
			if !providerConnectionEquivalent(pm.Spec.Kcp.ExtraProviderConnections[idx], d) {
				pm.Spec.Kcp.ExtraProviderConnections[idx] = d
				changed = true
			}
			continue
		}
		pm.Spec.Kcp.ExtraProviderConnections = append(pm.Spec.Kcp.ExtraProviderConnections, d)
		changed = true
	}

	if !changed {
		s.logger.Info().
			Str("kind_e2e", "TestScoped01KubeconfigKcpPrereq").
			Msg("scoped e2e provider connections already present")
		return
	}

	s.Require().NoError(
		s.client.Update(ctx, pm),
		"update PlatformMesh with scoped e2e provider connections",
	)
	s.logger.Info().
		Str("kind_e2e", "TestScoped01KubeconfigKcpPrereq").
		Msg("scoped e2e provider connections updated")
}

func providerConnectionEquivalent(a, b pmcorev1alpha1.ProviderConnection) bool {
	if a.Path != b.Path ||
		a.Secret != b.Secret ||
		a.External != b.External ||
		ptr.Deref(a.EndpointSliceName, "") != ptr.Deref(b.EndpointSliceName, "") ||
		ptr.Deref(a.RawPath, "") != ptr.Deref(b.RawPath, "") ||
		ptr.Deref(a.Namespace, "") != ptr.Deref(b.Namespace, "") ||
		ptr.Deref(a.AdminAuth, false) != ptr.Deref(b.AdminAuth, false) {
		return false
	}
	return slices.Equal(a.APIExportNames, b.APIExportNames)
}

func (s *KindTestSuite) waitScopedProviderConnectionSecretsReady(ctx context.Context) {
	secrets := []string{
		e2eScopedKubeconfigProvider1SecretName,
		e2eScopedKubeconfigProvider2SecretName,
		e2eAdminKubeconfigProvider3SecretName,
	}
	for _, secretName := range secrets {
		name := secretName
		s.Eventually(func() bool {
			sec := &corev1.Secret{}
			if err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: e2ePlatformMeshNamespace}, sec); err != nil {
				s.logger.Info().Str("secret", name).Msg("scoped prereq wait: provider secret not yet present")
				return false
			}
			if len(sec.Data["kubeconfig"]) == 0 {
				s.logger.Info().Str("secret", name).Msg("scoped prereq wait: provider secret kubeconfig empty")
				return false
			}
			return true
		}, 6*time.Minute, 10*time.Second, "provider secret %s not ready for scoped e2e", name)
	}
}

// Provider1: APIExport + schema + endpoint slice come from TestScoped01KubeconfigKcpPrereq (yaml/kcp-provider-workspaces), like a
// pre-provisioned workspace. The test uses only the operator-written scoped kubeconfig to create an E2EProviderConfig
// instance for that export (virtual workspace server).
func (s *KindTestSuite) TestScoped02KubeconfigProvider1() {
	s.logger.Info().Str("kind_e2e", "TestScoped02KubeconfigProvider1").Msg("start")
	ctx := s.T().Context()
	s.scopedWaitPlatformMeshReady(ctx)

	sec := s.requireE2EProviderKubeconfigSecret(ctx, e2eScopedKubeconfigProvider1SecretName)
	kcfg := sec.Data["kubeconfig"]

	name := fmt.Sprintf("e2e-provider1-%d", time.Now().UnixNano())
	note := "scoped-kubeconfig-provider1-e2e"
	manifestPath := filepath.Join(s.T().TempDir(), "e2eproviderconfig-provider1.yaml")
	manifest := fmt.Sprintf(`apiVersion: %s
kind: E2EProviderConfig
metadata:
  name: %s
spec:
  note: %s
`, e2eKindScopedProviderConfigAPIVersion, name, note)
	s.Require().NoError(os.WriteFile(manifestPath, []byte(manifest), 0o600))

	_, err := s.runKubectlWithRawKubeconfig(kcfg, "apply", "--validate=false", "-f", manifestPath)
	s.Require().NoError(err, "kubectl apply E2EProviderConfig with operator-generated provider1 scoped kubeconfig")

	out, err := s.runKubectlWithRawKubeconfig(kcfg, "get", e2eScopedProviderConfigResource, name, "-o", "jsonpath={.spec.note}")
	s.Require().NoError(err)
	s.Require().Equal(note, strings.TrimSpace(string(out)))

	s.deleteE2EProviderConfigOrWarn(ctx, e2eScopedKubeconfigProvider1Path, name)
	s.logger.Info().Str("kind_e2e", "TestScoped02KubeconfigProvider1").Str("e2eproviderconfig", name).Msg("done")
}

// Provider2: same as Provider1 regarding pre-provisioned export YAML; scoped kubeconfig uses workspace cluster URL.
// Test creates an E2EProviderConfig resource with that kubeconfig only (no APIExport creation in the test).
func (s *KindTestSuite) TestScoped03KubeconfigProvider2() {
	s.logger.Info().Str("kind_e2e", "TestScoped03KubeconfigProvider2").Msg("start")
	ctx := s.T().Context()
	s.scopedWaitPlatformMeshReady(ctx)

	sec := s.requireE2EProviderKubeconfigSecret(ctx, e2eScopedKubeconfigProvider2SecretName)
	kcfg := sec.Data["kubeconfig"]

	name := fmt.Sprintf("e2e-provider2-%d", time.Now().UnixNano())
	note := "scoped-kubeconfig-provider2-e2e"
	manifestPath := filepath.Join(s.T().TempDir(), "e2eproviderconfig-provider2.yaml")
	manifest := fmt.Sprintf(`apiVersion: %s
kind: E2EProviderConfig
metadata:
  name: %s
spec:
  note: %s
`, e2eKindScopedProviderConfigAPIVersion, name, note)
	s.Require().NoError(os.WriteFile(manifestPath, []byte(manifest), 0o600))

	_, err := s.runKubectlWithRawKubeconfig(kcfg, "apply", "--validate=false", "-f", manifestPath)
	s.Require().NoError(err, "kubectl apply E2EProviderConfig with operator-generated provider2 scoped kubeconfig")

	out, err := s.runKubectlWithRawKubeconfig(kcfg, "get", e2eScopedProviderConfigResource, name, "-o", "jsonpath={.spec.note}")
	s.Require().NoError(err)
	s.Require().Equal(note, strings.TrimSpace(string(out)))

	s.deleteE2EProviderConfigOrWarn(ctx, e2eScopedKubeconfigProvider2Path, name)
	s.logger.Info().Str("kind_e2e", "TestScoped03KubeconfigProvider2").Str("e2eproviderconfig", name).Msg("done")
}

// Provider3: extraProviderConnections entry with adminAuth true — same slice-based virtual workspace wiring as default providers, admin cert material.
func (s *KindTestSuite) TestScoped04ExtraProviderAdminKubeconfigProvider3() {
	s.logger.Info().Str("kind_e2e", "TestScoped04ExtraProviderAdminKubeconfigProvider3").Msg("start")
	ctx := s.T().Context()
	s.scopedWaitPlatformMeshReady(ctx)

	sec := s.requireE2EProviderKubeconfigSecret(ctx, e2eAdminKubeconfigProvider3SecretName)
	cfg, err := clientcmd.Load(sec.Data["kubeconfig"])
	s.Require().NoError(err)
	clusterName := cfg.Contexts[cfg.CurrentContext].Cluster
	cluster := cfg.Clusters[clusterName]
	s.Require().NotNil(cluster)
	s.Require().Contains(cluster.Server, "front-proxy", "admin provider kubeconfig should use front-proxy host from operator rewrite")
	s.logger.Info().Str("kind_e2e", "TestScoped04ExtraProviderAdminKubeconfigProvider3").Str("secret", e2eAdminKubeconfigProvider3SecretName).Msg("done")
}

func scopedTryLoadKcpClusterAdminClientCert(ctx context.Context, s *KindTestSuite, sec *corev1.Secret) (namespace string, ok bool) {
	for _, ns := range scopedE2EKcpClusterAdminCertSearchNamespaces {
		err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
			Name:      e2eKcpClusterAdminClientCertSecretName,
			Namespace: ns,
		}, sec)
		if err != nil {
			continue
		}
		if sec.Data == nil {
			continue
		}
		if len(sec.Data["ca.crt"]) == 0 || len(sec.Data["tls.crt"]) == 0 || len(sec.Data["tls.key"]) == 0 {
			continue
		}
		return ns, true
	}
	return "", false
}

// requireE2EProviderKubeconfigSecret loads the operator-written provider secret (PlatformMesh extraProviderConnections[].secret).
// Call only after scopedWaitPlatformMeshReady: Ready implies ProvidersecretSubroutine has populated these secrets.
func (s *KindTestSuite) requireE2EProviderKubeconfigSecret(ctx context.Context, secretName string) *corev1.Secret {
	sec := &corev1.Secret{}
	err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Name:      secretName,
		Namespace: e2ePlatformMeshNamespace,
	}, sec)
	s.Require().NoError(err, "provider kubeconfig secret %s/%s must exist once PlatformMesh is Ready", e2ePlatformMeshNamespace, secretName)
	s.Require().NotEmpty(sec.Data["kubeconfig"], "secret %s/%s must contain non-empty kubeconfig data", e2ePlatformMeshNamespace, secretName)
	return sec
}

func (s *KindTestSuite) deleteE2EProviderConfigOrWarn(ctx context.Context, workspacePath, name string) {
	cl, err := s.kcpClientForWorkspace(ctx, workspacePath)
	if err != nil {
		s.logger.Warn().Err(err).Str("workspace", workspacePath).Msg("cleanup: no kcp client for E2EProviderConfig delete")
		return
	}
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(e2eKindScopedProviderConfigAPIVersion)
	obj.SetKind("E2EProviderConfig")
	obj.SetName(name)
	if err := cl.Delete(ctx, obj); err != nil {
		s.logger.Warn().Err(err).Str("name", name).Str("workspace", workspacePath).Msg("cleanup delete E2EProviderConfig")
	}
}

func (s *KindTestSuite) kcpAdminKubeconfigBytes(ctx context.Context) ([]byte, error) {
	sec := &corev1.Secret{}
	var ns string
	var ok bool
	if scopedE2EKcpAdminCertSecretNamespace != "" {
		ns = scopedE2EKcpAdminCertSecretNamespace
		err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
			Name:      e2eKcpClusterAdminClientCertSecretName,
			Namespace: ns,
		}, sec)
		if err != nil {
			return nil, err
		}
		ok = sec.Data != nil && len(sec.Data["ca.crt"]) > 0 && len(sec.Data["tls.crt"]) > 0 && len(sec.Data["tls.key"]) > 0
	}
	if !ok {
		ns, ok = scopedTryLoadKcpClusterAdminClientCert(ctx, s, sec)
		if !ok {
			return nil, fmt.Errorf("secret %s not found with TLS data in namespaces %v",
				e2eKcpClusterAdminClientCertSecretName, scopedE2EKcpClusterAdminCertSearchNamespaces)
		}
	}
	ca, crt, key := sec.Data["ca.crt"], sec.Data["tls.crt"], sec.Data["tls.key"]
	if len(ca) == 0 || len(crt) == 0 || len(key) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing ca.crt, tls.crt, or tls.key", ns, e2eKcpClusterAdminClientCertSecretName)
	}
	apiCfg := clientcmdapi.NewConfig()
	apiCfg.Clusters = map[string]*clientcmdapi.Cluster{
		"kcp": {
			Server:                   kcpLocalFrontProxyURL,
			CertificateAuthorityData: ca,
		},
	}
	apiCfg.Contexts = map[string]*clientcmdapi.Context{
		"admin": {Cluster: "kcp", AuthInfo: "admin"},
	}
	apiCfg.AuthInfos = map[string]*clientcmdapi.AuthInfo{
		"admin": {ClientCertificateData: crt, ClientKeyData: key},
	}
	apiCfg.CurrentContext = "admin"
	return clientcmd.Write(*apiCfg)
}

func (s *KindTestSuite) restConfigForLocalKCPFrontProxy(kubeconfigBytes []byte) (*rest.Config, error) {
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, err
	}
	out := rest.CopyConfig(cfg)
	out.Host = kcpLocalFrontProxyURL
	return out, nil
}

func (s *KindTestSuite) kcpClientForWorkspace(ctx context.Context, workspacePath string) (ctrlruntimeclient.Client, error) {
	raw, err := s.kcpAdminKubeconfigBytes(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := s.restConfigForLocalKCPFrontProxy(raw)
	if err != nil {
		return nil, err
	}
	return (&subroutines.Helper{}).NewKcpClient(cfg, workspacePath)
}

func (s *KindTestSuite) kcpClientForWorkspaceWithScheme(ctx context.Context, scheme *runtime.Scheme, workspacePath string) ctrlruntimeclient.Client {
	kcpAdminCfg, err := subroutines.BuildKubeconfigFromConfig(s.client, &defaultKcpOperatorConfig, defaultKcpOperatorConfig.Url)
	s.Require().NoError(err, "getting kcp admin rest config should succeed")
	kcpAdminCfg.Host += "/clusters/" + workspacePath

	scopedKcpAdminClient, err := ctrlruntimeclient.New(kcpAdminCfg, ctrlruntimeclient.Options{
		Scheme: scheme,
	})
	s.Require().NoError(err, "creating scoped kcp admin client should succeed")

	return scopedKcpAdminClient
}

func (s *KindTestSuite) waitForKcpClusterAdminClientCert(ctx context.Context) {
	sec := &corev1.Secret{}
	s.Eventually(func() bool {
		ns, ok := scopedTryLoadKcpClusterAdminClientCert(ctx, s, sec)
		if !ok {
			s.logger.Warn().
				Str("secret", e2eKcpClusterAdminClientCertSecretName).
				Strs("searchedNamespaces", scopedE2EKcpClusterAdminCertSearchNamespaces).
				Msg("kcp cluster-admin client cert secret not available yet in any candidate namespace")
			return false
		}
		scopedE2EKcpAdminCertSecretNamespace = ns
		s.logger.Info().
			Str("kind_e2e", "TestScoped01KubeconfigKcpPrereq").
			Str("secret", e2eKcpClusterAdminClientCertSecretName).
			Str("namespace", ns).
			Msg("cluster-admin TLS secret ready")
		return true
	}, 20*time.Minute, 10*time.Second,
		"Secret "+e2eKcpClusterAdminClientCertSecretName+" was not populated in namespaces "+
			strings.Join(scopedE2EKcpClusterAdminCertSearchNamespaces, ", ")+
			" (needed for TestScoped01KubeconfigKcpPrereq / kcp workspace setup)")
}

// ensureScopedE2EKcpProviderWorkspaces creates root:providers:provider1|provider2 and applies static YAML that models a
// real deployment: APIExport (+ APIResourceSchema + APIExportEndpointSlice) already exists before tests run; tests only
// exercise creating resources from that export via scoped kubeconfigs.
func (s *KindTestSuite) ensureScopedE2EKcpProviderWorkspaces(ctx context.Context) {
	s.logger.Info().Str("kind_e2e", "TestScoped01KubeconfigKcpPrereq").Msg("seeding root:providers:provider1|provider2 and export YAML")
	emptyTmpl := make(map[string]string)
	rootClient, err := s.kcpClientForWorkspace(ctx, "root")
	s.Require().NoError(err, "kcp client for root")
	s.Require().NoError(
		ApplyManifestFromFile(ctx, filepath.Join(e2eKcpProviderWorkspacesYAMLDir, "workspace-providers.yaml"), rootClient, emptyTmpl),
		"apply workspace-providers.yaml",
	)
	s.logWorkspaceObservedAfterApply(ctx, rootClient, "providers")
	s.waitWorkspaceReady(ctx, rootClient, "providers")
	providersClient, err := s.kcpClientForWorkspace(ctx, "root:providers")
	s.Require().NoError(err, "kcp client for root:providers")
	s.Require().NoError(
		ApplyManifestFromFile(ctx, filepath.Join(e2eKcpProviderWorkspacesYAMLDir, "workspace-provider1-provider2.yaml"), providersClient, emptyTmpl),
		"apply workspace-provider1-provider2.yaml",
	)
	s.logWorkspaceObservedAfterApply(ctx, providersClient, "provider1")
	s.logWorkspaceObservedAfterApply(ctx, providersClient, "provider2")
	s.waitWorkspaceReady(ctx, providersClient, "provider1")
	s.waitWorkspaceReady(ctx, providersClient, "provider2")

	provider1Client, err := s.kcpClientForWorkspace(ctx, e2eScopedKubeconfigProvider1Path)
	s.Require().NoError(err, "kcp client for provider1")
	s.Require().NoError(
		ApplyManifestFromFile(ctx, filepath.Join(e2eKcpProviderWorkspacesYAMLDir, "provider1-kind-e2e-scoped-provider-export.yaml"), provider1Client, emptyTmpl),
		"apply root:providers:provider1 "+e2eKindScopedProviderExportName+" export manifests",
	)
	s.ensureProvider1EndpointSliceBootstrapAPIBinding(ctx, provider1Client)

	provider2Client, err := s.kcpClientForWorkspace(ctx, e2eScopedKubeconfigProvider2Path)
	s.Require().NoError(err, "kcp client for provider2")
	s.Require().NoError(
		ApplyManifestFromFile(ctx, filepath.Join(e2eKcpProviderWorkspacesYAMLDir, "provider2-kind-e2e-scoped-provider-export.yaml"), provider2Client, emptyTmpl),
		"apply root:providers:provider2 "+e2eKindScopedProviderExportName+" export manifests",
	)
	s.ensureProvider2ExportBootstrapAPIBindingReady(ctx, provider2Client)

	// Provider1 scoped kubeconfig uses endpointSliceName, so wait for slice status endpoints before reconciling provider secrets.
	s.waitAPIExportEndpointSliceEndpointsReady(ctx, e2eScopedKubeconfigProvider1Path, e2eKindScopedProviderExportName)
	s.logger.Info().
		Str("kind_e2e", "TestScoped01KubeconfigKcpPrereq").
		Str("export", e2eKindScopedProviderExportName).
		Msg("provider workspaces and APIExports applied")
}

func (s *KindTestSuite) waitAPIExportEndpointSliceEndpointsReady(ctx context.Context, workspacePath, sliceName string) {
	cl, err := s.kcpClientForWorkspace(ctx, workspacePath)
	s.Require().NoError(err, "kcp client for endpoint slice workspace %s", workspacePath)

	s.Eventually(func() bool {
		slice := &unstructured.Unstructured{}
		slice.SetAPIVersion("apis.kcp.io/v1alpha1")
		slice.SetKind("APIExportEndpointSlice")
		if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: sliceName}, slice); err != nil {
			return false
		}
		endpoints, foundEndpoints, _ := unstructured.NestedSlice(slice.Object, "status", "endpoints")
		apiExportEndpoints, foundAPIExportEndpoints, _ := unstructured.NestedSlice(slice.Object, "status", "apiExportEndpoints")
		activeEndpoints := endpoints
		if len(activeEndpoints) == 0 && len(apiExportEndpoints) > 0 {
			activeEndpoints = apiExportEndpoints
		}
		if (!foundEndpoints && !foundAPIExportEndpoints) || len(activeEndpoints) == 0 {
			return false
		}
		return true
	}, 6*time.Minute, 10*time.Second, "APIExportEndpointSlice %s in %s has no endpoints", sliceName, workspacePath)
}

func (s *KindTestSuite) ensureProvider1EndpointSliceBootstrapAPIBinding(ctx context.Context, provider1Client ctrlruntimeclient.Client) {
	binding := &unstructured.Unstructured{}
	binding.SetAPIVersion("apis.kcp.io/v1alpha2")
	binding.SetKind("APIBinding")
	binding.SetName("e2e-provider1-endpoint-bootstrap")
	binding.Object["spec"] = map[string]any{
		"reference": map[string]any{
			"export": map[string]any{
				"path": e2eScopedKubeconfigProvider1Path,
				"name": e2eKindScopedProviderExportName,
			},
		},
	}
	err := provider1Client.Create(ctx, binding)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		s.Require().NoError(err, "create provider1 bootstrap APIBinding")
	}
}

func (s *KindTestSuite) ensureProvider2ExportBootstrapAPIBindingReady(ctx context.Context, provider2Client ctrlruntimeclient.Client) {
	const bindingName = "e2e-provider2-export-bootstrap"
	binding := &unstructured.Unstructured{}
	binding.SetAPIVersion("apis.kcp.io/v1alpha2")
	binding.SetKind("APIBinding")
	binding.SetName(bindingName)
	binding.Object["spec"] = map[string]any{
		"reference": map[string]any{
			"export": map[string]any{
				"path": e2eScopedKubeconfigProvider2Path,
				"name": e2eKindScopedProviderExportName,
			},
		},
	}
	err := provider2Client.Create(ctx, binding)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		s.Require().NoError(err, "create provider2 bootstrap APIBinding")
	}
	s.Eventually(func() bool {
		current := &unstructured.Unstructured{}
		current.SetAPIVersion("apis.kcp.io/v1alpha2")
		current.SetKind("APIBinding")
		if err := provider2Client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: bindingName}, current); err != nil {
			return false
		}
		phase, _, _ := unstructured.NestedString(current.Object, "status", "phase")
		return phase == "Bound"
	}, 3*time.Minute, 5*time.Second, "provider2 bootstrap APIBinding %s not bound", bindingName)
}

// kubectl with kubeconfig bytes unchanged (virtual workspace or workspace cluster URL as written by the operator).
func (s *KindTestSuite) runKubectlWithRawKubeconfig(kubeconfigBytes []byte, kubectlArgs ...string) ([]byte, error) {
	normalizedKubeconfigBytes, err := normalizeScopedKubeconfigServerForLocalRun(kubeconfigBytes)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "scoped-kubeconfig-raw-*.yaml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.Write(normalizedKubeconfigBytes); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	args := append([]string{"--kubeconfig", tmp.Name()}, kubectlArgs...)
	return runCommand("kubectl", args...)
}

// normalizeScopedKubeconfigServerForLocalRun handles scoped e2e cases.
// This is test-only behavior for host-run kubectl in local/CI e2e, not generic production kubeconfig rewriting.
func normalizeScopedKubeconfigServerForLocalRun(kubeconfigBytes []byte) ([]byte, error) {
	cfg, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return nil, err
	}

	currentContext := cfg.Contexts[cfg.CurrentContext]
	cluster := cfg.Clusters[currentContext.Cluster]

	server := cluster.Server

	// provider2: in-cluster front-proxy DNS is not resolvable from host-run kubectl.
	server = strings.Replace(server, "frontproxy-front-proxy.platform-mesh-system:8443", "kcp.api.portal.localhost:8443", 1)

	// provider1: virtual workspace URL from endpoint slice is flaky for create/get in host-run kubectl.
	// For this fixed fixture, use the concrete provider1 workspace cluster URL.
	if strings.Contains(server, "/services/apiexport/") {
		server = "https://kcp.api.portal.localhost:8443/clusters/" + e2eScopedKubeconfigProvider1Path
	}

	cluster.Server = server
	out, err := clientcmd.Write(*cfg)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// logWorkspaceObservedAfterApply helps CI debug: confirms whether the Workspace object is visible right after SSA apply.
func (s *KindTestSuite) logWorkspaceObservedAfterApply(ctx context.Context, cl ctrlruntimeclient.Client, workspaceName string) {
	ws := &unstructured.Unstructured{}
	ws.SetAPIVersion("tenancy.kcp.io/v1alpha1")
	ws.SetKind("Workspace")
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: workspaceName}, ws); err != nil {
		if apierrors.IsNotFound(err) {
			s.logger.Warn().Str("workspace", workspaceName).
				Msg("workspace not visible immediately after apply (apiserver may lag); wait loop will retry")
			return
		}
		s.logger.Warn().Err(err).Str("workspace", workspaceName).
			Msg("could not get workspace right after apply")
		return
	}
	s.logWorkspacePhaseAndConditionMessages(ws, workspaceName, false, "workspace observed after apply")
}

// logWorkspacePhaseAndConditionMessages logs status.phase and each condition's message field in full (no truncation).
func (s *KindTestSuite) logWorkspacePhaseAndConditionMessages(ws *unstructured.Unstructured, workspaceName string, warn bool, msg string) {
	phase, _, _ := unstructured.NestedString(ws.Object, "status", "phase")
	var messages []string
	conditions, ok, _ := unstructured.NestedSlice(ws.Object, "status", "conditions")
	if ok {
		for _, raw := range conditions {
			cm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			condType, _, _ := unstructured.NestedString(cm, "type")
			m, _, _ := unstructured.NestedString(cm, "message")
			reason, _, _ := unstructured.NestedString(cm, "reason")
			switch {
			case m != "":
				messages = append(messages, m)
			case reason != "":
				// kcp often omits message on True conditions; False frequently sets reason only.
				if condType != "" {
					messages = append(messages, condType+": "+reason)
				} else {
					messages = append(messages, reason)
				}
			}
		}
	}
	evt := s.logger.Info()
	if warn {
		evt = s.logger.Warn()
	}
	evt = evt.Str("workspace", workspaceName)
	if phase != "" {
		evt = evt.Str("phase", phase)
	}
	if len(messages) > 0 {
		evt = evt.Str("condition_messages", strings.Join(messages, "\n---\n"))
	}
	evt.Msg(msg)
}

func (s *KindTestSuite) waitWorkspaceReady(ctx context.Context, cl ctrlruntimeclient.Client, workspaceName string) {
	s.Eventually(func() bool {
		ws := &unstructured.Unstructured{}
		ws.SetAPIVersion("tenancy.kcp.io/v1alpha1")
		ws.SetKind("Workspace")
		if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: workspaceName}, ws); err != nil {
			s.logger.Warn().Err(err).Str("workspace", workspaceName).Msg("workspace not ready yet")
			return false
		}
		phase, found, err := unstructured.NestedString(ws.Object, "status", "phase")
		if err != nil || !found || phase != "Ready" {
			s.logWorkspacePhaseAndConditionMessages(ws, workspaceName, true, "workspace exists but not Ready yet")
			return false
		}
		return true
	}, 3*time.Minute, 10*time.Second, "workspace "+workspaceName+" did not become ready")
}

// scopedWaitPlatformMeshReady mirrors kind_operator_test.go TestResourceReady (same Get key, Ready + Status "True", 25m / 10s).
// On each unsuccessful poll it also logs status.conditions at Debug (turn on debug log level to see in CI).
func (s *KindTestSuite) scopedWaitPlatformMeshReady(ctx context.Context) {
	s.Eventually(func() bool {
		pm := pmcorev1alpha1.PlatformMesh{}
		err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
			Name:      e2ePlatformMeshName,
			Namespace: e2ePlatformMeshNamespace,
		}, &pm)
		if err != nil {
			s.logger.Warn().Err(err).Msg("Failed to get Platform Mesh resource")
			return false
		}

		for _, condition := range pm.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				s.logger.Info().Msg("PlatformMesh resource is ready")
				return true
			}
		}
		s.logPlatformMeshNotReadyDebug(&pm)
		return false
	}, 25*time.Minute, 10*time.Second)
}

func (s *KindTestSuite) logPlatformMeshNotReadyDebug(pm *pmcorev1alpha1.PlatformMesh) {
	if len(pm.Status.Conditions) == 0 {
		s.logger.Debug().
			Msg("PlatformMesh not Ready: status.conditions is empty")
		return
	}
	var b strings.Builder
	for i, c := range pm.Status.Conditions {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		_, _ = fmt.Fprintf(&b, "type=%s status=%s reason=%s message=%s", c.Type, c.Status, c.Reason, c.Message)
	}
	s.logger.Debug().
		Str("conditions", b.String()).
		Msg("PlatformMesh not Ready: current status.conditions")
}
