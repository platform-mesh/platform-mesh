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

package app_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/account-operator/pkg/app"
	"go.platform-mesh.io/account-operator/pkg/config"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	cfg := logger.DefaultConfig()
	cfg.NoJSON = true
	l, err := logger.New(cfg)
	require.NoError(t, err)
	return l
}

func newManager(t *testing.T) mcmanager.Manager {
	t.Helper()
	s, err := app.NewScheme()
	require.NoError(t, err)
	// The manager does not talk to the API server until it is started.
	mgr, err := mcmanager.New(&rest.Config{Host: "https://127.0.0.1:1"}, nil, mcmanager.Options{
		Scheme:  s,
		Metrics: metricsserver.Options{BindAddress: "0"},
		// Every subtest registers the same controller names.
		Controller: ctrlconfig.Controller{SkipNameValidation: ptr.To(true)},
	})
	require.NoError(t, err)
	return mgr
}

func TestNewScheme(t *testing.T) {
	s, err := app.NewScheme()
	require.NoError(t, err)
	require.True(t, s.Recognizes(pmcorev1alpha1.SchemeGroupVersion.WithKind("Account")))
	require.True(t, s.Recognizes(kcptenancyv1alpha1.SchemeGroupVersion.WithKind("Workspace")))
}

func TestSetupControllers(t *testing.T) {
	t.Run("requires a logger", func(t *testing.T) {
		require.Error(t, app.SetupControllers(newManager(t), config.NewOperatorConfig(), app.Deps{}))
	})

	t.Run("defaults", func(t *testing.T) {
		require.NoError(t, app.SetupControllers(newManager(t), config.NewOperatorConfig(), app.Deps{Log: testLogger(t)}))
	})

	t.Run("account info controller disabled", func(t *testing.T) {
		cfg := config.NewOperatorConfig()
		cfg.Controllers.AccountInfo.Enabled = false
		require.NoError(t, app.SetupControllers(newManager(t), cfg, app.Deps{Log: testLogger(t)}))
	})
}

func TestSetupWebhooks(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		require.NoError(t, app.SetupWebhooks(newManager(t), config.NewOperatorConfig(), app.Deps{}))
	})

	t.Run("enabled", func(t *testing.T) {
		cfg := config.NewOperatorConfig()
		cfg.Webhooks.Enabled = true
		cfg.Webhooks.DenyList = "admin, root"
		cfg.Webhooks.AdditionalAccountTypes = []string{"team"}
		require.NoError(t, app.SetupWebhooks(newManager(t), cfg, app.Deps{Log: testLogger(t)}))
	})
}
