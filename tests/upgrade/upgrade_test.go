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

// Package upgrade tests the upgrade of Platform Mesh from the latest release
// to this checkout. It brings up its own kind cluster with the code behind
// "pmctl quickstart": first the release, then, over it, the checkout built
// into a local registry ("--dev").
//
// Run it with `task test-upgrade`. It needs docker, go and task, and takes a
// while: it builds every image of the checkout. A plain `go test ./...` skips
// it.
//
// What has to survive the upgrade is described by the scenarios in
// tests/upgrade/scenarios: each lists steps that seed data into the release,
// steps that validate it, and steps that go on working with it. The test
// calls scenarios.Seed and scenarios.Validate on the release, and on the
// checkout scenarios.Validate again and then scenarios.After. Around the
// upgrade it runs the migrations of "pmctl migrate" the release needs.
//
// Environment:
//
//	UPGRADE_TEST          true runs the test; `task test-upgrade` sets it
//	UPGRADE_FROM          release to upgrade from, e.g. v26.10.2 (default: the latest)
//	UPGRADE_CLUSTER       name of the kind cluster (default: pm-upgrade)
//	UPGRADE_KEEP_CLUSTER  true keeps the cluster after a passed run; after a
//	                      failed one it is always kept, for a look at it
//	UPGRADE_PARALLEL_BUILD  images built at a time (default: the quickstart default)
//	UPGRADE_VERBOSITY  klog level of quickstart, like its -v flag; 1 or more
//	                   prints the status of each step while it waits (default: 0)
//	UPGRADE_SKIP_COMPONENTS  optional components (ocm/components.yaml) the
//	                   build of the checkout leaves out, separated by commas
package upgrade

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/cmd/pmctl/app/migrate"
	"go.platform-mesh.io/platform-mesh/cmd/pmctl/app/quickstart"
	"go.platform-mesh.io/platform-mesh/tests/framework/keycloak"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/upgrade/scenarios"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const (
	// releaseTimeout bounds the installation of the release, buildTimeout
	// the build of the checkout and the upgrade to it.
	releaseTimeout = 45 * time.Minute
	buildTimeout   = 90 * time.Minute
)

var _ scenarios.Environment = &environment{}

// environment is the installation under test.
type environment struct {
	// cluster is the name of the kind cluster.
	cluster string
	// kubeconfig of the kind cluster. The CA certificate of the
	// installation is written next to it.
	kubeconfig string
	// url is the address of the portal, caFile the CA that signs its
	// certificate. Both are set by the first installation.
	url    string
	caFile string
}

// TestUpgrade runs the upgrade test from the specified release to the checkout.
// It installs the release specified by UPGRADE_FROM, seeds data, upgrades to the checkout, and verifies that the seeded data survives.
func TestUpgrade(t *testing.T) {
	if run, _ := strconv.ParseBool(os.Getenv("UPGRADE_TEST")); !run {
		t.Skip("creates a kind cluster and builds the checkout: run it with `task test-upgrade`")
	}
	setVerbosity(t)
	env := newEnvironment(t)

	// 1. The latest release. quickstart returns once the installation is
	//    ready: PlatformMesh is Ready, the charts and their workloads are
	//    rolled out, and the portal answers.
	from := os.Getenv("UPGRADE_FROM")
	phase(t, "install release", func(t *testing.T) {
		env.quickstart(t, from, false, releaseTimeout)
	})
	phase(t, "verify release", func(t *testing.T) {
		verify(t, env)
	})
	// ORDER MATTERS: seed and the first validation both come before the
	// upgrade. The data has to be created by the release, and the
	// validation has to hold on the release, or its failure after the
	// upgrade says nothing about the upgrade.
	phase(t, "seed", func(t *testing.T) { scenarios.Seed(t, env) })
	phase(t, "validate release", func(t *testing.T) { scenarios.Validate(t, env) })

	// 2. The migrations the release needs on its way to the checkout, as
	//    "pmctl migrate" runs them. ORDER MATTERS: their first part runs on
	//    the release and can stop a service until the upgrade, their second
	//    part runs on the checkout.
	phase(t, "migrate before upgrade", func(t *testing.T) {
		from = env.migrate(t, from, migrate.PhaseBefore)
	})

	// 3. The checkout, over the release.
	phase(t, "upgrade to checkout", func(t *testing.T) {
		env.quickstart(t, "", true, buildTimeout)
	})
	phase(t, "migrate after upgrade", func(t *testing.T) {
		env.migrate(t, from, migrate.PhaseAfter)
	})
	phase(t, "verify checkout", func(t *testing.T) {
		verify(t, env)
	})
	phase(t, "validate checkout", func(t *testing.T) { scenarios.Validate(t, env) })
	// ORDER MATTERS: after the validation, because it changes and deletes
	// what the validation looks for.
	phase(t, "after upgrade", func(t *testing.T) { scenarios.After(t, env) })
}

// verify checks that the installation works.
func verify(t *testing.T, _ *environment) {
	t.Helper()
	// TODO: the checks that an installation works beyond being ready, the
	// same before and after the upgrade (for example the Go suites of
	// tests/e2e against env.kubeconfig, a login to the portal at env.url).
	t.Skip("placeholder: no checks yet")
}

// setVerbosity sets the klog level of quickstart from UPGRADE_VERBOSITY. The
// test calls quickstart as a library, so its -v flag is not there to set it;
// without it, a step that waits prints nothing until it ends.
func setVerbosity(t *testing.T) {
	t.Helper()
	v := os.Getenv("UPGRADE_VERBOSITY")
	if v == "" {
		return
	}
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	require.NoError(t, fs.Set("v", v), "UPGRADE_VERBOSITY")
}

// newEnvironment prepares the environment and registers its cleanup. The
// files of the cluster go to .test/<cluster>, which git ignores.
func newEnvironment(t *testing.T) *environment {
	t.Helper()

	cluster := os.Getenv("UPGRADE_CLUSTER")
	if cluster == "" {
		cluster = "pm-upgrade"
	}
	dir, err := filepath.Abs(filepath.Join(".test", cluster))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o750))

	env := &environment{cluster: cluster, kubeconfig: filepath.Join(dir, "kubeconfig")}
	t.Cleanup(func() {
		keep, _ := strconv.ParseBool(os.Getenv("UPGRADE_KEEP_CLUSTER"))
		if t.Failed() || keep {
			t.Logf("keeping kind cluster %q: KUBECONFIG=%s; remove it with: pmctl quickstart delete --name %s --kubeconfig %s",
				env.cluster, env.kubeconfig, env.cluster, env.kubeconfig)
			return
		}
		env.delete(t)
	})
	return env
}

// streams are what quickstart writes its progress and summary to.
func streams() genericiooptions.IOStreams {
	return genericiooptions.IOStreams{In: os.Stdin, Out: os.Stderr, ErrOut: os.Stderr}
}

// quickstart installs Platform Mesh into the cluster, creating the cluster
// if needed and upgrading what is installed otherwise, and waits until the
// installation is ready. It installs the release version (empty: the latest),
// or with dev the build of this checkout.
func (e *environment) quickstart(t *testing.T, version string, dev bool, timeout time.Duration) {
	t.Helper()

	o := quickstart.NewOptions(streams())
	o.Name = e.cluster
	o.Kubeconfig = e.kubeconfig
	o.Version = version
	o.Dev = dev
	o.Timeout = timeout
	if n := os.Getenv("UPGRADE_PARALLEL_BUILD"); n != "" {
		parallel, err := strconv.Atoi(n)
		require.NoError(t, err, "UPGRADE_PARALLEL_BUILD")
		o.ParallelBuilds = parallel
	}
	if skip := os.Getenv("UPGRADE_SKIP_COMPONENTS"); dev && skip != "" {
		o.SkipComponents = strings.Split(skip, ",")
	}

	require.NoError(t, o.Complete(nil, nil))
	require.NoError(t, o.Validate())
	require.NoError(t, o.Run(t.Context()))

	e.url = o.URL()
	e.caFile = o.CAFile()
}

// migrate runs a phase of the migrations an installation at the version
// from needs, and returns that version: with from empty it is the installed
// one, which only the installation knows before the upgrade.
func (e *environment) migrate(t *testing.T, from, phase string) string {
	t.Helper()

	o := migrate.NewOptions(streams())
	o.Kubeconfig = e.kubeconfig
	o.From = from
	o.Phase = phase
	// Next to the kubeconfig: kept with the cluster after a failed run.
	o.BackupDir = filepath.Join(filepath.Dir(e.kubeconfig), "migrate")

	require.NoError(t, o.Complete(nil, nil))
	require.NoError(t, o.Validate())
	require.NoError(t, o.Run(t.Context()))
	return o.From
}

// Kube implements scenarios.Environment: the admin config of the kind
// cluster.
func (e *environment) Kube(context.Context) (*rest.Config, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", e.kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", e.kubeconfig, err)
	}
	return cfg, nil
}

// secret reads a secret of the installation.
func (e *environment) secret(ctx context.Context, name string) (map[string][]byte, error) {
	cfg, err := e.Kube(ctx)
	if err != nil {
		return nil, err
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	secret, err := kube.CoreV1().Secrets(pm.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("secret %s/%s: %w", pm.Namespace, name, err)
	}
	return secret.Data, nil
}

// KCP implements scenarios.Environment: the kcp admin config of the
// installation, without a workspace path. The kubeconfig the operator keeps
// in the cluster points at the front-proxy as the host reaches it. It is
// read anew on every call: the upgrade may replace it.
func (e *environment) KCP(ctx context.Context) (*rest.Config, error) {
	data, err := e.secret(ctx, pm.AdminKubeconfigSecret)
	if err != nil {
		return nil, err
	}
	return pm.AdminConfig(data["kubeconfig"])
}

// Keycloak implements scenarios.Environment: the admin client of the
// Keycloak of the installation, which is served under the address of the
// portal.
func (e *environment) Keycloak(ctx context.Context) (keycloak.Client, error) {
	admin, err := e.secret(ctx, "keycloak-admin")
	if err != nil {
		return nil, err
	}
	if len(admin["username"]) == 0 || len(admin["password"]) == 0 {
		return nil, errors.New("secret keycloak-admin has no username or password")
	}
	roots, err := e.roots()
	if err != nil {
		return nil, err
	}
	return keycloak.New(strings.TrimSuffix(e.url, "/")+"/keycloak", string(admin["username"]), string(admin["password"]), roots), nil
}

// Portal implements scenarios.Environment: the address of the portal and a
// client that trusts the CA of the installation.
func (e *environment) Portal(context.Context) (string, *http.Client, error) {
	roots, err := e.roots()
	if err != nil {
		return "", nil, err
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
			// A transport with a TLS config of its own speaks HTTP/1.1
			// and offers no protocol in the handshake, and the gateway
			// closes such a connection to the subdomain of an
			// organization.
			ForceAttemptHTTP2: true,
		},
	}
	return strings.TrimSuffix(e.url, "/"), client, nil
}

// roots are the CAs the certificate of the portal is checked against: the
// CA of the installation, which quickstart wrote to caFile.
func (e *environment) roots() (*x509.CertPool, error) {
	return pm.Roots(e.caFile)
}

// delete removes the cluster and what quickstart left next to it.
func (e *environment) delete(t *testing.T) {
	t.Helper()

	o := quickstart.NewDeleteOptions(streams())
	o.Name = e.cluster
	o.Kubeconfig = e.kubeconfig
	require.NoError(t, o.Complete(nil, nil))
	require.NoError(t, o.Validate())
	// The context of the test is done by the time its cleanup runs.
	if err := o.Run(context.Background()); err != nil {
		t.Errorf("deleting kind cluster %q: %v", e.cluster, err)
	}
}

// phase runs fn as a subtest and stops the test if it fails: every phase
// builds on the one before.
func phase(t *testing.T, name string, fn func(t *testing.T)) {
	t.Helper()
	if !t.Run(name, fn) {
		t.FailNow()
	}
}
