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

// Package manager builds multicluster managers over kcp APIExport providers,
// the way the operators wire themselves in cmd/.
package manager

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/tests/framework/kcp"

	"k8s.io/apimachinery/pkg/runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/providers/multi"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
)

const stopTimeout = 30 * time.Second

// Providers builds one apiexport provider per export, each watching the
// APIExportEndpointSlice of the same name in the workspace at path (usually
// :root:platform-mesh-system). The map is keyed by export name.
//
// The account-operator uses golang-commons' path-aware provider instead;
// build that from env.RESTConfig(path) and pass it to mcmanager.New.
func Providers(t testing.TB, env *kcp.Env, path logicalcluster.Path, s *runtime.Scheme, exports ...string) map[string]*apiexport.Provider {
	t.Helper()
	providers := make(map[string]*apiexport.Provider, len(exports))
	for _, export := range exports {
		p, err := apiexport.New(env.RESTConfig(path), export, apiexport.Options{Scheme: s})
		require.NoError(t, err, "building provider for %s", export)
		providers[export] = p
	}
	return providers
}

// New builds a multicluster manager on the workspace at path over a multi
// provider holding providers under their map keys. Metrics are disabled
// (bind address "0") and BaseContext is t's context.
func New(t testing.TB, env *kcp.Env, path logicalcluster.Path, s *runtime.Scheme, providers map[string]*apiexport.Provider) (mcmanager.Manager, *multi.Provider) {
	t.Helper()
	provider := multi.New(multi.Options{})
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		require.NoError(t, provider.AddProvider(name, providers[name]), "adding provider %s", name)
	}

	mgr, err := mcmanager.New(env.RESTConfig(path), provider, mcmanager.Options{
		Scheme:      s,
		Metrics:     metricsserver.Options{BindAddress: "0"},
		BaseContext: t.Context,
	})
	require.NoError(t, err, "building manager")
	return mgr, provider
}

// Start runs mgr until t ends and waits for its cache to sync. It returns
// the manager's context, which is cancelled in t.Cleanup; the cleanup waits
// for the manager to stop and fails t if it returned an error.
func Start(t testing.TB, mgr mcmanager.Manager) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("manager exited with error: %v", err)
			}
		case <-time.After(stopTimeout):
			t.Errorf("manager did not stop within %s", stopTimeout)
		}
	})

	synced := make(chan bool, 1)
	go func() { synced <- mgr.GetLocalManager().GetCache().WaitForCacheSync(ctx) }()
	select {
	case ok := <-synced:
		require.True(t, ok, "manager cache did not sync")
	case err := <-done:
		done <- err // for the cleanup
		require.NoError(t, err, "manager exited before its cache synced")
		require.Fail(t, "manager exited before its cache synced")
	}
	return ctx
}
