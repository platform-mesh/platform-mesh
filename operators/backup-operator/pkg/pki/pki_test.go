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

package pki_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/backup-operator/internal/testfixtures"
	"go.platform-mesh.io/backup-operator/pkg/pki"
)

func TestCollect(t *testing.T) {
	cl := testfixtures.Cluster(t)
	b, err := pki.Collect(context.Background(), cl, testfixtures.Namespace)
	require.NoError(t, err)

	certs := make([]string, 0, len(b.Certificates))
	issuers := make([]string, 0, len(b.Issuers))
	secrets := make([]string, 0, len(b.Secrets))
	for _, c := range b.Certificates {
		certs = append(certs, c.Name)
		assert.Empty(t, c.ResourceVersion)
	}
	for _, i := range b.Issuers {
		issuers = append(issuers, i.Name)
	}
	for _, s := range b.Secrets {
		secrets = append(secrets, s.Name)
		assert.NotEmpty(t, s.Data["tls.key"])
	}
	assert.ElementsMatch(t, []string{"root-ca", "root-server", "kcp-cluster-admin-client-cert"}, certs,
		"kcp-operator certificates and certificates from a kcp CA, not the webhook certificate")
	assert.ElementsMatch(t, []string{"root-ca", "selfsigned"}, issuers)
	assert.Equal(t, testfixtures.KcpPKISecrets, secrets)
	require.Len(t, b.Kubeconfigs, 1)
	assert.Equal(t, testfixtures.Kubeconfig, b.Kubeconfigs[0].GetName())
	assert.Empty(t, b.Kubeconfigs[0].GetUID())
	assert.NotContains(t, b.Kubeconfigs[0].Object, "status")
}

func TestCollectWithoutKcp(t *testing.T) {
	_, err := pki.Collect(context.Background(), testfixtures.Cluster(t), "other")
	assert.ErrorContains(t, err, "no kcp certificates found")
}
