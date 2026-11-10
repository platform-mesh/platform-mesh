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

package platformmeshoperator

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// TestAdminKubeconfigSelfContained: an AdminAuth extra provider connection
// makes the operator materialise a provider kubeconfig from
// kubeconfig-kcp-admin whose CA bundle forms a chain and which can list
// core.platform-mesh.io Accounts.
func TestAdminKubeconfigSelfContained(t *testing.T) {
	const (
		secretName    = "kind-e2e-admin-kubeconfig"
		workspacePath = "root:platform-mesh-system"
	)
	cl := kubeClient(t)
	waitPlatformMeshReady(t, cl)

	addProviderConnections(t, cl, pmcorev1alpha1.ProviderConnection{
		Path:              workspacePath,
		Secret:            secretName,
		EndpointSliceName: ptr.To("core.platform-mesh.io"),
		AdminAuth:         ptr.To(true),
	})
	kubeconfig := waitKubeconfigSecret(t, cl, ctrlruntimeclient.ObjectKey{Name: secretName, Namespace: platformMeshNamespace}, 6*time.Minute)

	requireCABundleFormsChain(t, kubeconfig)

	accounts := &unstructured.UnstructuredList{}
	accounts.SetAPIVersion("core.platform-mesh.io/v1alpha1")
	accounts.SetKind("AccountList")
	require.NoError(t, kubeconfigClient(t, kubeconfig, workspacePath).List(t.Context(), accounts),
		"listing accounts.core.platform-mesh.io with the admin provider kubeconfig")
}

// requireCABundleFormsChain checks that every non-self-signed certificate in
// the kubeconfig's certificate-authority-data has its signing issuer in the
// bundle, and that the bundle contains a self-signed root.
func requireCABundleFormsChain(t *testing.T, kubeconfig []byte) {
	t.Helper()
	cfg, err := clientcmd.Load(kubeconfig)
	require.NoError(t, err, "parsing provider kubeconfig")
	kctx := cfg.Contexts[cfg.CurrentContext]
	require.NotNil(t, kctx, "kubeconfig must have a current context")
	cluster := cfg.Clusters[kctx.Cluster]
	require.NotNil(t, cluster, "kubeconfig must contain the current cluster")
	require.NotEmpty(t, cluster.CertificateAuthorityData, "kubeconfig must contain certificate-authority-data")

	certs := parsePEMCertificates(t, cluster.CertificateAuthorityData)
	require.NotEmpty(t, certs, "CA bundle must contain at least one certificate")

	bySubject := map[string][]*x509.Certificate{}
	for _, c := range certs {
		bySubject[c.Subject.String()] = append(bySubject[c.Subject.String()], c)
	}
	foundRoot := false
	for _, c := range certs {
		if c.Subject.String() == c.Issuer.String() && c.CheckSignatureFrom(c) == nil {
			foundRoot = true
			continue
		}
		signed := false
		for _, issuer := range bySubject[c.Issuer.String()] {
			if c.CheckSignatureFrom(issuer) == nil {
				signed = true
				break
			}
		}
		require.True(t, signed, "CA bundle contains a certificate without its signing issuer (subject=%q issuer=%q)", c.Subject, c.Issuer)
	}
	require.True(t, foundRoot, "CA bundle must contain a self-signed root certificate")
}

func parsePEMCertificates(t *testing.T, data []byte) []*x509.Certificate {
	t.Helper()
	var certs []*x509.Certificate
	for rest := data; len(rest) > 0; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err, "parsing CA bundle certificate")
		certs = append(certs, c)
	}
	return certs
}
