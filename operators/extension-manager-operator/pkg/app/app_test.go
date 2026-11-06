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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/pkg/app"
	"go.platform-mesh.io/extension-manager-operator/pkg/config"
	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func TestNewScheme(t *testing.T) {
	s := app.NewScheme()
	for _, obj := range []runtime.Object{
		&pmuiv1alpha1.ContentConfiguration{},
		&kcpapisv1alpha1.APIExportEndpointSlice{},
		&kcpcorev1alpha1.LogicalCluster{},
		&kcptenancyv1alpha1.Workspace{},
	} {
		_, _, err := s.ObjectKinds(obj)
		assert.NoError(t, err, "%T not registered", obj)
	}
}

func TestSetupControllers(t *testing.T) {
	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)

	newManager := func(t *testing.T) mcmanager.Manager {
		t.Helper()
		mgr, err := mcmanager.New(&rest.Config{Host: "https://127.0.0.1:1"}, nil, manager.Options{
			Scheme:  app.NewScheme(),
			Metrics: metricsserver.Options{BindAddress: "0"},
			// Every subtest registers the same controller name.
			Controller: ctrlconfig.Controller{SkipNameValidation: ptr.To(true)},
		})
		require.NoError(t, err)
		return mgr
	}

	t.Run("requires a logger", func(t *testing.T) {
		err := app.SetupControllers(newManager(t), nil, app.Deps{})
		assert.Error(t, err)
	})

	t.Run("defaults", func(t *testing.T) {
		err := app.SetupControllers(newManager(t), nil, app.Deps{Log: log})
		assert.NoError(t, err)
	})

	t.Run("explicit config", func(t *testing.T) {
		operator := config.NewOperatorConfig()
		operator.EntityTypeValidationEnabled = true
		err := app.SetupControllers(newManager(t), &config.Config{
			Common:   &platformmeshconfig.CommonServiceConfig{MaxConcurrentReconciles: 2},
			Operator: operator,
		}, app.Deps{Log: log})
		assert.NoError(t, err)
	})
}
