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

// Package env resolves the deployed Platform Mesh the tests/e2e suites run
// against: the runtime (kube) cluster from KUBECONFIG and the kcp admin
// kubeconfig from ADMIN_KUBECONFIG. On a Tilt cluster:
//
//	. contrib/tilt/env.sh && KUBECONFIG=~/.kube/config task tests:e2e
//
// or simply `task tilt:e2e`.
package env

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	// Kubeconfig names the runtime cluster's kubeconfig (colon-separated list allowed).
	Kubeconfig = "KUBECONFIG"
	// KindContext optionally selects the context in KUBECONFIG (contrib/tilt/env.sh sets it).
	KindContext = "KIND_CONTEXT"
	// AdminKubeconfig names the kcp admin kubeconfig. Its "base" context (or the
	// current one) must point at the front-proxy.
	AdminKubeconfig = "ADMIN_KUBECONFIG"
)

const hint = "the e2e suites run against an already deployed Platform Mesh; " +
	"on a Tilt cluster run `task tilt:e2e`, or `. contrib/tilt/env.sh && KUBECONFIG=~/.kube/config task tests:e2e`"

// Check reports every missing or unreadable kubeconfig variable.
func Check() error {
	var errs []error
	if os.Getenv(Kubeconfig) == "" {
		errs = append(errs, fmt.Errorf("%s is not set", Kubeconfig))
	}
	if path := os.Getenv(AdminKubeconfig); path == "" {
		errs = append(errs, fmt.Errorf("%s is not set", AdminKubeconfig))
	} else if _, err := os.Stat(path); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", AdminKubeconfig, err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w (%s)", errors.Join(errs...), hint)
	}
	return nil
}

// Main is the TestMain of every e2e package: it exits with a clear message
// before any test runs when the environment is incomplete.
//
//	func TestMain(m *testing.M) { os.Exit(env.Main(m)) }
func Main(m *testing.M) int {
	if err := Check(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	return m.Run()
}

// Kube returns the rest config of the runtime cluster (KUBECONFIG, context
// KIND_CONTEXT when set).
func Kube(t testing.TB) *rest.Config {
	t.Helper()
	require.NotEmpty(t, os.Getenv(Kubeconfig), "%s is not set (%s)", Kubeconfig, hint)
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: os.Getenv(KindContext)}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	require.NoError(t, err, "loading %s", Kubeconfig)
	return cfg
}

// KCP returns the kcp admin rest config from ADMIN_KUBECONFIG, pointing at
// the front-proxy without a /clusters/ path.
func KCP(t testing.TB) *rest.Config {
	t.Helper()
	path := os.Getenv(AdminKubeconfig)
	require.NotEmpty(t, path, "%s is not set (%s)", AdminKubeconfig, hint)
	raw, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err, "loading %s", AdminKubeconfig)
	ctxName := raw.CurrentContext
	if _, ok := raw.Contexts["base"]; ok {
		ctxName = "base"
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, ctxName, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	require.NoError(t, err, "building rest config from %s context %q", AdminKubeconfig, ctxName)
	cfg.Host = trimClusterPath(cfg.Host)
	return cfg
}

// KCPWorkspace returns KCP(t) scoped to the logical cluster path (e.g. "root:orgs").
func KCPWorkspace(t testing.TB, path string) *rest.Config {
	t.Helper()
	cfg := KCP(t)
	cfg.Host += "/clusters/" + path
	return cfg
}

// KCPURL returns the front-proxy URL of ADMIN_KUBECONFIG (scheme and host).
func KCPURL(t testing.TB) *url.URL {
	t.Helper()
	u, err := url.Parse(KCP(t).Host)
	require.NoError(t, err, "parsing the kcp front-proxy URL")
	return &url.URL{Scheme: u.Scheme, Host: u.Host}
}

// HostReachable rewrites a kubeconfig written by a component for in-cluster
// use (front-proxy Service DNS, virtual workspace URLs) so it can be used from
// the machine running the tests: the server's scheme and host are replaced by
// the front-proxy of ADMIN_KUBECONFIG, and a /services/... virtual workspace
// path is replaced by /clusters/<fallback> when fallback is not empty.
// Credentials and CA data are kept.
func HostReachable(t testing.TB, kubeconfig []byte, fallback string) []byte {
	t.Helper()
	cfg, err := clientcmd.Load(kubeconfig)
	require.NoError(t, err, "parsing kubeconfig")
	cluster := currentCluster(t, cfg)
	server, err := url.Parse(cluster.Server)
	require.NoError(t, err, "parsing server %q", cluster.Server)
	base := KCPURL(t)
	server.Scheme, server.Host = base.Scheme, base.Host
	if fallback != "" && strings.HasPrefix(server.Path, "/services/") {
		server.Path = "/clusters/" + fallback
	}
	cluster.Server = server.String()
	out, err := clientcmd.Write(*cfg)
	require.NoError(t, err, "writing kubeconfig")
	return out
}

func currentCluster(t testing.TB, cfg *clientcmdapi.Config) *clientcmdapi.Cluster {
	t.Helper()
	ctx, ok := cfg.Contexts[cfg.CurrentContext]
	require.True(t, ok, "kubeconfig has no current context %q", cfg.CurrentContext)
	cluster, ok := cfg.Clusters[ctx.Cluster]
	require.True(t, ok, "kubeconfig has no cluster %q", ctx.Cluster)
	return cluster
}

func trimClusterPath(host string) string {
	if i := strings.Index(host, "/clusters/"); i >= 0 {
		return host[:i]
	}
	return strings.TrimSuffix(host, "/")
}
