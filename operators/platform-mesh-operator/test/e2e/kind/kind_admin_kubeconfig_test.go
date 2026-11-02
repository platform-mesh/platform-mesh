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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kindE2EAdminProviderKubeconfigSecretName = "kind-e2e-admin-kubeconfig"
	kindE2EAdminProviderWorkspacePath        = "root:platform-mesh-system"
)

// Test03AdminKubeconfigSelfContained runs after Test01/Test02 (lexical order). Those tests and the Kind
// suite already bring up PlatformMesh and kcp-operator secrets; this test does not re-wait for Ready
// or for kubeconfig-kcp-admin. It registers an extra AdminAuth ProviderConnection so the operator
// materializes a provider kubeconfig from kubeconfig-kcp-admin (admin path), then verifies TLS + API access.
func (s *KindTestSuite) Test03AdminKubeconfigSelfContained() {
	s.logger.Info().Str("kind_e2e", "Test03AdminKubeconfigSelfContained").Msg("start")
	ctx := context.Background()
	s.runAdminKubeconfigSelfContainedE2E(ctx)
	s.logger.Info().Str("kind_e2e", "Test03AdminKubeconfigSelfContained").Msg("done")
}

func (s *KindTestSuite) runAdminKubeconfigSelfContainedE2E(ctx context.Context) {
	s.adminE2EPatchExtraProviderConnectionForAdminKubeconfig(ctx)
	s.adminE2EWaitAdminProviderKubeconfigSecret(ctx)

	sec := s.adminE2ERequireAdminProviderSecret(ctx)
	kcfg := sec.Data["kubeconfig"]
	s.assertGeneratedAdminProviderKubeconfigCABundleFormsChain(kcfg)

	var err error
	kcfg, err = normalizeAdminProviderKubeconfigForLocalRun(kcfg)
	s.Require().NoError(err, "normalize admin provider kubeconfig for host must succeed")

	dyn, err := dynamicClientForKubeconfig(kcfg)
	s.Require().NoError(err, "build dynamic client from admin provider kubeconfig must succeed")
	s.Require().NoError(
		func() error {
			gvr := schema.GroupVersionResource{Group: "core.platform-mesh.io", Version: "v1alpha1", Resource: "accounts"}
			_, err := dyn.Resource(gvr).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
			return err
		}(),
		"list accounts.core.platform-mesh.io with admin provider kubeconfig must succeed",
	)
}

func (s *KindTestSuite) adminE2EPatchExtraProviderConnectionForAdminKubeconfig(ctx context.Context) {
	pm := &pmcorev1alpha1.PlatformMesh{}
	err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Name:      e2ePlatformMeshName,
		Namespace: e2ePlatformMeshNamespace,
	}, pm)
	s.Require().NoError(err, "get PlatformMesh for admin e2e provider connection")

	// ExtraProviderConnections is the supported way to add a one-off AdminAuth provider secret without
	// changing the chart defaults; the operator reconciles it like other admin kubeconfigs.
	desired := pmcorev1alpha1.ProviderConnection{
		Path:              kindE2EAdminProviderWorkspacePath,
		Secret:            kindE2EAdminProviderKubeconfigSecretName,
		EndpointSliceName: ptr.To("core.platform-mesh.io"),
		AdminAuth:         ptr.To(true),
	}

	currentBySecret := make(map[string]int, len(pm.Spec.Kcp.ExtraProviderConnections))
	for i, pc := range pm.Spec.Kcp.ExtraProviderConnections {
		currentBySecret[pc.Secret] = i
	}

	if idx, ok := currentBySecret[kindE2EAdminProviderKubeconfigSecretName]; ok {
		if providerConnectionEquivalent(pm.Spec.Kcp.ExtraProviderConnections[idx], desired) {
			s.logger.Info().Msg("admin e2e: provider connection already present")
			return
		}
		pm.Spec.Kcp.ExtraProviderConnections[idx] = desired
	} else {
		pm.Spec.Kcp.ExtraProviderConnections = append(pm.Spec.Kcp.ExtraProviderConnections, desired)
	}

	s.Require().NoError(s.client.Update(ctx, pm), "update PlatformMesh admin e2e extraProviderConnections")
	s.logger.Info().Str("secret", kindE2EAdminProviderKubeconfigSecretName).Msg("admin e2e: provider connection updated")
}

func (s *KindTestSuite) adminE2EWaitAdminProviderKubeconfigSecret(ctx context.Context) {
	name := kindE2EAdminProviderKubeconfigSecretName
	s.Eventually(func() bool {
		sec := &corev1.Secret{}
		if err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: e2ePlatformMeshNamespace}, sec); err != nil {
			s.logger.Info().Str("secret", name).Msg("admin e2e: operator provider secret not yet present")
			return false
		}
		if len(sec.Data["kubeconfig"]) == 0 {
			s.logger.Info().Str("secret", name).Msg("admin e2e: provider secret kubeconfig empty")
			return false
		}
		return true
	}, 6*time.Minute, 10*time.Second, "admin e2e provider secret %s/%s not ready", e2ePlatformMeshNamespace, name)
}

func (s *KindTestSuite) adminE2ERequireAdminProviderSecret(ctx context.Context) *corev1.Secret {
	sec := &corev1.Secret{}
	err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Name:      kindE2EAdminProviderKubeconfigSecretName,
		Namespace: e2ePlatformMeshNamespace,
	}, sec)
	s.Require().NoError(err, "provider kubeconfig secret %s/%s", e2ePlatformMeshNamespace, kindE2EAdminProviderKubeconfigSecretName)
	s.Require().NotEmpty(sec.Data["kubeconfig"], "secret %s must contain kubeconfig", kindE2EAdminProviderKubeconfigSecretName)
	return sec
}

func normalizeAdminProviderKubeconfigForLocalRun(kubeconfigBytes []byte) ([]byte, error) {
	cfg, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return nil, err
	}
	cur := cfg.Contexts[cfg.CurrentContext]
	cluster := cfg.Clusters[cur.Cluster]
	server := cluster.Server
	server = strings.Replace(server, "frontproxy-front-proxy.platform-mesh-system:6443", "kcp.api.portal.localhost:8443", 1)
	if strings.Contains(server, "/services/apiexport/") {
		server = fmt.Sprintf("https://kcp.api.portal.localhost:8443/clusters/%s", kindE2EAdminProviderWorkspacePath)
	}
	cluster.Server = server
	return clientcmd.Write(*cfg)
}

func parsePEMCertificates(pemBytes []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := pemBytes
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	return certs, nil
}

func isSelfSigned(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	if cert.Subject.String() != cert.Issuer.String() {
		return false
	}
	return cert.CheckSignatureFrom(cert) == nil
}

func (s *KindTestSuite) assertGeneratedAdminProviderKubeconfigCABundleFormsChain(providerKubeconfig []byte) {
	s.T().Helper()

	providerCfg, err := clientcmd.Load(providerKubeconfig)
	s.Require().NoError(err, "parse generated admin provider kubeconfig")
	s.Require().NotEmpty(providerCfg.CurrentContext, "generated kubeconfig must have current-context")
	providerCtx := providerCfg.Contexts[providerCfg.CurrentContext]
	s.Require().NotNil(providerCtx, "generated kubeconfig must contain current context")
	providerCluster := providerCfg.Clusters[providerCtx.Cluster]
	s.Require().NotNil(providerCluster, "generated kubeconfig must contain current cluster")
	s.Require().NotEmpty(providerCluster.CertificateAuthorityData, "generated kubeconfig must contain certificate-authority-data")

	providerCAs, err := parsePEMCertificates(providerCluster.CertificateAuthorityData)
	s.Require().NoError(err, "parse generated kubeconfig CA bundle PEM")
	s.Require().GreaterOrEqual(len(providerCAs), 1, "generated CA bundle must contain at least one cert")

	// Cryptographic consistency: every non-self-signed cert must have a signing issuer in the bundle.
	bySubject := make(map[string][]*x509.Certificate, len(providerCAs))
	for _, c := range providerCAs {
		if c != nil {
			bySubject[c.Subject.String()] = append(bySubject[c.Subject.String()], c)
		}
	}
	foundRoot := false
	for _, c := range providerCAs {
		if isSelfSigned(c) {
			foundRoot = true
			continue
		}
		issuers := bySubject[c.Issuer.String()]
		ok := false
		for _, iss := range issuers {
			if iss == nil {
				continue
			}
			if c.CheckSignatureFrom(iss) == nil {
				ok = true
				break
			}
		}
		s.Require().True(ok, "generated CA bundle contains cert without matching signing issuer in bundle (subject=%q issuer=%q)", c.Subject.String(), c.Issuer.String())
	}
	s.Require().True(foundRoot, "generated CA bundle must contain at least one self-signed root cert")
}
