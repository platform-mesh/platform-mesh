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
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/controllers/resource"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/options"

	"sigs.k8s.io/controller-runtime/pkg/controller"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

// ResourceControllerTestSuite runs the listener's resource (anchor) controller
// with the single provider against a kcp workspace.
type ResourceControllerTestSuite struct {
	suite.Suite

	listenerCfg *listener.Config
	cancel      context.CancelFunc
}

func TestResourceControllerTestSuite(t *testing.T) {
	suite.Run(t, new(ResourceControllerTestSuite))
}

func (suite *ResourceControllerTestSuite) SetupSuite() {
	tg := newTarget(suite.T(), "gql-resource")

	tmpDir := suite.T().TempDir()

	opts := options.NewOptions()
	opts.Common.Kubeconfig = tg.writeKubeconfig(suite.T(), tmpDir)
	opts.Common.Metrics.BindAddress = "0"
	opts.Common.HealthProbeBindAddress = "0"
	opts.SchemasDir = filepath.Join(tmpDir, "schemas")

	completedOpts, err := opts.Complete()
	suite.Require().NoError(err, "failed to complete options")

	listenerConfig, err := listener.NewConfig(completedOpts)
	suite.Require().NoError(err, "failed to create listener config")

	r, err := resource.New(
		suite.T().Context(),
		listenerConfig.Manager,
		controller.TypedOptions[mcreconcile.Request]{},
		listenerConfig.SchemaHandler,
		listenerConfig.Options.AnchorResource,
		listenerConfig.Options.ResourceGVR,
		listenerConfig.Options.AdditionalPathAnnotationKey,
		listenerConfig.Options.ClusterMetadataFunc,
		listenerConfig.Options.ClusterURLResolverFunc,
	)
	suite.Require().NoError(err, "failed to create resource reconciler")

	err = r.SetupWithManager(listenerConfig.Manager)
	suite.Require().NoError(err, "failed to setup resource reconciler with manager")

	suite.listenerCfg = listenerConfig

	ctx, cancel := context.WithCancel(suite.T().Context())
	suite.cancel = cancel

	t := suite.T()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := listenerConfig.Manager.Start(ctx); err != nil {
			t.Logf("multi-cluster manager stopped: %v", err)
		}
	}()
	// Join the manager before the suite's test completes, so its log line
	// cannot land after the test ended.
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Errorf("manager did not stop within 30s")
		}
	})
}

func (suite *ResourceControllerTestSuite) TearDownSuite() {
	suite.cancel()
}

func (suite *ResourceControllerTestSuite) TestSchemaGeneration() {
	schemaFilePath := filepath.Join(suite.listenerCfg.Options.SchemasDir, "single")
	var raw []byte
	suite.Eventually(func() bool {
		var err error
		raw, err = os.ReadFile(schemaFilePath)
		return err == nil && json.Valid(raw)
	}, 5*time.Second, 500*time.Millisecond, "expected schema file to be generated")

	var schema pmgatewayv1alpha1.Schema
	err := json.NewDecoder(bytes.NewReader(raw)).Decode(&schema)
	suite.Require().NoError(err, "failed to decode schema file")

	suite.NotEmpty(schema.ClusterMetadata, "schema has metadata")
	suite.NotEmpty(schema.Components, "schema has resources")
}
