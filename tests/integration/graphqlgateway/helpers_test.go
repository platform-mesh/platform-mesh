//go:build integration

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

package graphqlgateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	gatewaycrds "go.platform-mesh.io/kubernetes-graphql-gateway/config/crd"
	"go.platform-mesh.io/tests/framework/fixtures"
	"go.platform-mesh.io/tests/framework/kcp"
	"go.platform-mesh.io/tests/framework/scheme"
	"go.platform-mesh.io/tests/framework/wait"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/multicluster-provider/envtest"
	"github.com/kcp-dev/sdk/apis/core"
)

const clusterAccessCRD = "clusteraccesses.gateway.platform-mesh.io"

// target is a kcp workspace standing in for the kube-apiserver the envtest
// suites started: the gateway CRDs are established in it, and its admin
// credentials are inlined so they can be copied into secrets, kubeconfigs and
// schema metadata the way the suites used envtest's.
type target struct {
	config     *rest.Config
	client     ctrlruntimeclient.Client
	kubeconfig []byte
}

// newTarget creates a workspace under root, applies the gateway CRDs from the
// service's config/crd embed and waits for them to be established.
func newTarget(t testing.TB, prefix string) *target {
	t.Helper()
	env := kcp.Server(t)
	_, path := env.Workspace(t, core.RootCluster.Path(), envtest.WithNamePrefix(prefix))

	cfg := inlineTLS(t, env.RESTConfig(path))
	cl, err := ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: scheme.New(t, pmgatewayv1alpha1.AddToScheme)})
	require.NoError(t, err, "building client for %s", path)

	fixtures.ApplyFS(t, cl, gatewaycrds.Resources, "*.yaml")
	wait.CRDEstablished(t, cl, clusterAccessCRD)

	return &target{config: cfg, client: cl, kubeconfig: kubeconfigFor(t, cfg)}
}

// writeKubeconfig writes the target's kubeconfig into dir for components that
// take a kubeconfig path.
func (tg *target) writeKubeconfig(t testing.TB, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "kubeconfig")
	require.NoError(t, os.WriteFile(path, tg.kubeconfig, 0o600), "writing kubeconfig")
	return path
}

// inlineTLS returns a copy of cfg with CA, client certificate and key files
// read into the *Data fields (an attached kcp's kubeconfig may reference files).
func inlineTLS(t testing.TB, cfg *rest.Config) *rest.Config {
	t.Helper()
	cfg = rest.CopyConfig(cfg)
	read := func(data *[]byte, file *string) {
		if len(*data) == 0 && *file != "" {
			raw, err := os.ReadFile(*file)
			require.NoError(t, err, "reading %s", *file)
			*data = raw
		}
		*file = ""
	}
	read(&cfg.CAData, &cfg.CAFile)
	read(&cfg.CertData, &cfg.CertFile)
	read(&cfg.KeyData, &cfg.KeyFile)
	if cfg.BearerToken == "" && cfg.BearerTokenFile != "" {
		raw, err := os.ReadFile(cfg.BearerTokenFile)
		require.NoError(t, err, "reading %s", cfg.BearerTokenFile)
		cfg.BearerToken = string(raw)
	}
	cfg.BearerTokenFile = ""
	return cfg
}

// kubeconfigFor serializes cfg (inlined credentials) as a single-context kubeconfig.
func kubeconfigFor(t testing.TB, cfg *rest.Config) []byte {
	t.Helper()
	kc := clientcmdapi.NewConfig()
	kc.Clusters["workspace"] = &clientcmdapi.Cluster{
		Server:                   cfg.Host,
		CertificateAuthorityData: cfg.CAData,
		InsecureSkipTLSVerify:    cfg.Insecure,
		TLSServerName:            cfg.ServerName,
	}
	kc.AuthInfos["admin"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: cfg.CertData,
		ClientKeyData:         cfg.KeyData,
		Token:                 cfg.BearerToken,
	}
	kc.Contexts["workspace"] = &clientcmdapi.Context{Cluster: "workspace", AuthInfo: "admin"}
	kc.CurrentContext = "workspace"
	raw, err := clientcmd.Write(*kc)
	require.NoError(t, err, "serializing kubeconfig")
	return raw
}
