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

package resource_test

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

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type ResourceControllerTestSuite struct {
	suite.Suite

	env         *envtest.Environment
	listenerCfg *listener.Config
	cancel      context.CancelFunc
}

func TestResourceControllerTestSuite(t *testing.T) {
	suite.Run(t, new(ResourceControllerTestSuite))
}

func (suite *ResourceControllerTestSuite) SetupSuite() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	log.SetLogger(klog.NewKlogr())

	suite.env = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd"),
		},
		ErrorIfCRDPathMissing: true,
	}

	_, err := suite.env.Start()
	suite.Require().NoError(err, "failed to start test environment")

	tmpDir := suite.T().TempDir()

	// Write the kubeconfig bytes to a temp file for the listener config
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err = os.WriteFile(kubeconfigPath, suite.env.KubeConfig, 0600)
	suite.Require().NoError(err, "failed to write kubeconfig")

	opts := options.NewOptions()
	opts.Common.Kubeconfig = kubeconfigPath
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
		listenerConfig.Options.AdditonalPathAnnotationKey,
		listenerConfig.Options.ClusterMetadataFunc,
		listenerConfig.Options.ClusterURLResolverFunc,
	)
	suite.Require().NoError(err, "failed to create resource reconciler")

	err = r.SetupWithManager(listenerConfig.Manager)
	suite.Require().NoError(err, "failed to setup resource reconciler with manager")

	suite.listenerCfg = listenerConfig

	ctx, cancel := context.WithCancel(suite.T().Context())
	suite.cancel = cancel

	go func() {
		err = listenerConfig.Manager.Start(ctx)
		suite.Require().NoError(err, "failed to start multi-cluster manager")
	}()
}

func (suite *ResourceControllerTestSuite) TestSchemaGeneration() {
	schemaFilePath := filepath.Join(suite.listenerCfg.Options.SchemasDir, "single")
	suite.Eventually(func() bool {
		_, err := os.Stat(schemaFilePath)
		return err == nil
	}, 5*time.Second, 500*time.Millisecond, "expected schema file to be generated")

	raw, err := os.ReadFile(schemaFilePath)
	suite.Require().NoError(err, "failed to read schema file")

	var schema pmgatewayv1alpha1.Schema
	err = json.NewDecoder(bytes.NewReader(raw)).Decode(&schema)
	suite.Require().NoError(err, "failed to decode schema file")

	suite.NotEmpty(schema.ClusterMetadata, "schema has metadata")
	suite.NotEmpty(schema.Components, "schema has resources")
}

func (suite *ResourceControllerTestSuite) TearDownSuite() {
	// Cancel the manager context first to allow graceful shutdown
	suite.cancel()

	err := suite.env.Stop()
	suite.Require().NoError(err, "failed to stop test environment")
}
