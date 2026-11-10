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

// Package manager builds multicluster managers over kcp APIExport providers.
package manager

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/providers/multi"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
)

const stopTimeout = 30 * time.Second

var setLogger sync.Once

// Providers builds one apiexport provider per export, watching the
// APIExportEndpointSlice of that name in the workspace at path.
func Providers(t testing.TB, instance *kcptc.Instance, path logicalcluster.Path, s *runtime.Scheme, exports ...string) map[string]*apiexport.Provider {
	t.Helper()
	providers := make(map[string]*apiexport.Provider, len(exports))
	for _, export := range exports {
		p, err := apiexport.New(instance.RESTConfig(path.String()), export, apiexport.Options{Scheme: s})
		require.NoError(t, err, "building provider for %s", export)
		providers[export] = p
	}
	return providers
}

// New builds a multicluster manager on the workspace at path over a multi
// provider of providers. The first call sets controller-runtime's logger
// (TEST_QUIET=1 keeps it silent).
func New(t testing.TB, instance *kcptc.Instance, path logicalcluster.Path, s *runtime.Scheme, providers map[string]*apiexport.Provider) (mcmanager.Manager, *multi.Provider) {
	t.Helper()
	setLogger.Do(func() {
		if os.Getenv("TEST_QUIET") != "1" {
			ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
		}
	})
	provider := multi.New(multi.Options{})
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		require.NoError(t, provider.AddProvider(name, providers[name]), "adding provider %s", name)
	}

	mgr, err := mcmanager.New(instance.RESTConfig(path.String()), provider, mcmanager.Options{
		Scheme:      s,
		Metrics:     metricsserver.Options{BindAddress: "0"},
		BaseContext: t.Context,
	})
	require.NoError(t, err, "building manager")
	return mgr, provider
}

// Start runs mgr until t ends, waits for its cache to sync and returns its
// context.
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
