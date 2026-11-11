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
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/cli/pkg/cmd/quickstart"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/klog/v2"
)

const (
	// releaseTimeout bounds the installation of the release, buildTimeout
	// the build of the checkout and the upgrade to it.
	releaseTimeout = 45 * time.Minute
	buildTimeout   = 90 * time.Minute
)

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

// seeded is what seed leaves in the installation for checkSeeded to find
// after the upgrade.
type seeded struct{}

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
	step(t, "install release", func(t *testing.T) {
		env.quickstart(t, from, false, releaseTimeout)
	})
	step(t, "verify release", func(t *testing.T) {
		verify(t, env)
	})
	var data seeded
	step(t, "seed", func(t *testing.T) {
		data = seed(t, env)
	})

	// 2. The checkout, over the release.
	step(t, "upgrade to checkout", func(t *testing.T) {
		env.quickstart(t, "", true, buildTimeout)
	})
	step(t, "verify checkout", func(t *testing.T) {
		verify(t, env)
	})
	step(t, "check seeded data", func(t *testing.T) {
		checkSeeded(t, env, data)
	})
}

// verify checks that the installation works.
func verify(t *testing.T, _ *environment) {
	t.Helper()
	// TODO: the checks that an installation works beyond being ready, the
	// same before and after the upgrade (for example the Go suites of
	// tests/e2e against env.kubeconfig, a login to the portal at env.url).
	t.Skip("placeholder: no checks yet")
}

// seed creates what must survive the upgrade.
func seed(t *testing.T, _ *environment) seeded {
	t.Helper()
	// TODO: create data in the release installation (an organization, an
	// account, a user, a provider binding) and describe it in seeded.
	t.Skip("placeholder: nothing is seeded yet")
	return seeded{}
}

// checkSeeded checks that what seed created is still there and usable.
func checkSeeded(t *testing.T, _ *environment, _ seeded) {
	t.Helper()
	// TODO: find what seed created, after the upgrade.
	t.Skip("placeholder: nothing is seeded yet")
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

// step runs fn as a subtest and stops the test if it fails: every step
// builds on the one before.
func step(t *testing.T, name string, fn func(t *testing.T)) {
	t.Helper()
	if !t.Run(name, fn) {
		t.FailNow()
	}
}
