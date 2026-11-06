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

package suite

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// EtcdEndpoint is the TLS etcd URL shards reach through the config-plane gateway.
func (e *Env) EtcdEndpoint() string {
	return fmt.Sprintf("https://etcd.%s.sslip.io:31443", e.Config.NodeIP)
}

// EtcdClientSecret is the name of the mutual-TLS client secret shards mount.
const EtcdClientSecret = "etcd-client-tls"

// setupEtcdTLS issues the etcd server cert with the config-plane sslip.io SAN and routes it through the gateway, so shards on other clusters reach it.
func setupEtcdTLS(t *testing.T, c *Cluster) {
	t.Helper()
	host := fmt.Sprintf("etcd.%s.sslip.io", c.NodeIP)
	applyYAML(t, c, etcdServerCertYAML(host))
	applyYAML(t, c, etcdTLSRouteYAML(host))
	waitForSecret(t, c, ProviderNamespace, "etcd-server-tls")
	waitForSecret(t, c, ProviderNamespace, EtcdClientSecret)
	rolloutWait(t, c, ProviderNamespace, "deployment/etcd")
}

// CopyEtcdClientCert copies the mutual-TLS client secret to a cluster that runs root shards or shards, so its kcp pods can reach etcd.
func (e *Env) CopyEtcdClientCert(t *testing.T, target *Cluster) {
	t.Helper()
	createNamespace(t, target.Client, ProviderNamespace)
	copySecret(t, e.Config, target, ProviderNamespace, EtcdClientSecret)
}

func etcdServerCertYAML(host string) string {
	return fmt.Sprintf(`apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: etcd-server
  namespace: %s
spec:
  secretName: etcd-server-tls
  dnsNames:
    - etcd
    - etcd.%s.svc
    - %s
  usages:
    - server auth
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: etcd-ca
    kind: Issuer
    group: cert-manager.io
`, ProviderNamespace, ProviderNamespace, host)
}

func etcdTLSRouteYAML(host string) string {
	return fmt.Sprintf(`apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TLSRoute
metadata:
  name: etcd
  namespace: %s
spec:
  parentRefs:
    - name: eg
      namespace: envoy-gateway-system
      sectionName: passthrough
  hostnames:
    - %s
  rules:
    - backendRefs:
        - name: etcd
          port: 2379
`, ProviderNamespace, host)
}

func copySecret(t *testing.T, from, to *Cluster, namespace, name string) {
	t.Helper()
	src := &corev1.Secret{}
	require.NoError(t, from.Client.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: namespace, Name: name}, src))
	dst := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	dst.Type = src.Type
	dst.Data = src.Data
	if err := to.Client.Create(t.Context(), dst); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("copying secret %s/%s: %v", namespace, name, err)
	}
}
