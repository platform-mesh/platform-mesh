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

// Package pm gives the e2e tests the Platform Mesh they run against: the
// runtime cluster from KUBECONFIG (context KIND_CONTEXT when set) and kcp
// from ADMIN_KUBECONFIG. On a Tilt cluster, `task tilt:e2e` sets both.
//
//	cl, err := client.New(pm.Kube(t), client.Options{Scheme: scheme.New(t)})
//	orgs, err := client.New(pm.KCP(t, "root:orgs"), client.Options{Scheme: scheme.New(t)})
package pm

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const hint = "the e2e tests run against a deployed Platform Mesh; on a Tilt cluster run `task tilt:e2e`"

// Kube is the config of the runtime cluster, where the operators and the
// PlatformMesh resource live.
func Kube(t testing.TB) *rest.Config {
	t.Helper()
	require.NotEmpty(t, os.Getenv("KUBECONFIG"), "KUBECONFIG is not set (%s)", hint)
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: os.Getenv("KIND_CONTEXT")},
	).ClientConfig()
	require.NoError(t, err, "loading KUBECONFIG")
	return cfg
}

// KCP is the kcp admin config scoped to the workspace at path, or to no
// workspace when path is empty.
func KCP(t testing.TB, path string) *rest.Config {
	t.Helper()
	file := os.Getenv("ADMIN_KUBECONFIG")
	require.NotEmpty(t, file, "ADMIN_KUBECONFIG is not set (%s)", hint)
	// The "base" context points at the front-proxy without a workspace.
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: file},
		&clientcmd.ConfigOverrides{CurrentContext: "base"},
	).ClientConfig()
	require.NoError(t, err, "loading ADMIN_KUBECONFIG")
	cfg.Host = strings.TrimSuffix(cfg.Host, "/")
	if path != "" {
		cfg.Host += "/clusters/" + path
	}
	return cfg
}

// Kubeconfig is the config of a kubeconfig a component wrote for use inside
// the cluster. Its server (the front-proxy Service, or a virtual workspace
// URL) is replaced by the front-proxy of ADMIN_KUBECONFIG so the test host
// can reach it; a /services/ virtual workspace path becomes
// /clusters/<fallbackPath>. Credentials and CA data are kept.
func Kubeconfig(t testing.TB, kubeconfig []byte, fallbackPath string) *rest.Config {
	t.Helper()
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	require.NoError(t, err, "rest config from kubeconfig")
	server, err := url.Parse(cfg.Host)
	require.NoError(t, err, "parsing server %q", cfg.Host)
	front, err := url.Parse(KCP(t, "").Host)
	require.NoError(t, err, "parsing the front-proxy URL")
	server.Scheme, server.Host = front.Scheme, front.Host
	if strings.HasPrefix(server.Path, "/services/") {
		server.Path = "/clusters/" + fallbackPath
	}
	cfg.Host = server.String()
	return cfg
}
