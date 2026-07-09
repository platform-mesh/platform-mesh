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

package controller_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/yaml"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	mcc "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"

	_ "embed"
)

var (
	//go:embed test/setup/01-platform-mesh-system/apiexport-core.platform-mesh.io.yaml
	apiexportCorePlatformMeshIoYAML []byte
	//go:embed test/setup/01-platform-mesh-system/apibinding-core.platform-mesh.org.yaml
	apiexportendpointsliceCorePlatformMeshOrgYAML []byte
	//go:embed test/setup/01-platform-mesh-system/apiresourceschema-accountinfos.core.platform-mesh.io.yaml
	apiresourceschemaAccountinfosCorePlatformMeshIoYAML []byte
	//go:embed test/setup/01-platform-mesh-system/apiresourceschema-accounts.core.platform-mesh.io.yaml
	apiresourceschemaAccountsCorePlatformMeshIoYAML []byte

	//go:embed test/setup/02-orgs/account-root-org.yaml
	accountRootOrgYAML []byte

	// Workspace type YAML files (similar to security-operator)
	//go:embed test/setup/workspacetype-org.yaml
	workspaceTypeOrgYAML []byte
	//go:embed test/setup/workspace-type-orgs.yaml
	workspaceTypeOrgsYAML []byte
	//go:embed test/setup/workspace-type-account.yaml
	workspaceTypeAccountYAML []byte
)

const (
	defaultWorkspace              = "default"
	relativeBinaryAssetsDirectory = "../../bin"
)

// setupKCP starts kcp and sets up the basic platform-mesh workspace structure
// and configuration.
func (s *AccountTestSuite) setupKCP() {
	s.env = &envtest.Environment{AttachKcpOutput: false, KcpStopTimeout: time.Second * 30}
	s.env.BinaryAssetsDirectory = relativeBinaryAssetsDirectory
	s.env.KcpStartTimeout = 2 * time.Minute
	s.env.KcpStopTimeout = 30 * time.Second

	// Set the context in case using an existing kcp instance.
	if os.Getenv("USE_EXISTING_KCP") != "" && os.Getenv("EXISTING_KCP_CONTEXT") == "" {
		s.env.ExistingKcpContext = "base"
	}

	// Prevents kcp from cleaning up workspace fixtures before shutdown, the
	// instance controlled by envtest is ephemeral anyway.
	if os.Getenv("PRESERVE") == "" {
		s.Require().NoError(os.Setenv("PRESERVE", "true"))
	}

	var err error
	s.kcpConfig, err = s.env.Start()
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := s.env.Stop(); err != nil {
			s.T().Logf("Error stopping kcp environment: %v", err)
		}
	})

	s.kcpClient, err = mcc.New(s.kcpConfig, ctrlruntimeclient.Options{
		Scheme: s.scheme,
	})
	s.Require().NoError(err)

	rootClient := s.kcpClient.Cluster(core.RootCluster.Path())
	s.rootClient = rootClient

	// Create :root:platform-mesh-system
	_, s.platformMeshSysPath = envtest.NewWorkspaceFixture(s.T(), s.kcpClient, core.RootCluster.Path(), envtest.WithName("platform-mesh-system"))
	platformMeshSystemClient := s.kcpClient.Cluster(s.platformMeshSysPath)

	// register api-resource schemas
	for _, schemaYAML := range [][]byte{apiresourceschemaAccountinfosCorePlatformMeshIoYAML, apiresourceschemaAccountsCorePlatformMeshIoYAML} {
		var schema kcpapisv1alpha1.APIResourceSchema
		s.Require().NoError(yaml.Unmarshal(schemaYAML, &schema))
		err = platformMeshSystemClient.Create(s.ctx, &schema)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			s.Require().NoError(err)
		}
		s.logger.Info().Msgf("Created APIResourceSchema: %s", schema.Name)
	}

	// Fetch identity hash and create "core.platform-mesh.io" APIExport in
	// platform-mesh-system. This must happen before creating WorkspaceTypes
	// that reference this APIExport in their defaultAPIBindings, as kcp 0.32+
	// validates bind permissions at admission time.
	var aePlatformMesh, aeTenancy kcpapisv1alpha1.APIExport
	err = rootClient.Get(s.ctx, types.NamespacedName{Name: "tenancy.kcp.io"}, &aeTenancy)
	s.Require().NoError(err)
	err = yaml.Unmarshal(apiexportCorePlatformMeshIoYAML, &aePlatformMesh)
	s.Require().NoError(err)
	for i := range aePlatformMesh.Spec.PermissionClaims {
		pc := &aePlatformMesh.Spec.PermissionClaims[i]
		if pc.Group == "tenancy.kcp.io" {
			pc.IdentityHash = aeTenancy.Status.IdentityHash
		}
	}
	err = platformMeshSystemClient.Create(s.ctx, &aePlatformMesh)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		s.Require().NoError(err)
	}
	s.logger.Info().Msg("Created APIExport 'core.platform-mesh.io' in platform-mesh-system")

	// Create APIBinding in platform-mesh-system
	var platformMeshBinding kcpapisv1alpha2.APIBinding
	s.Require().NoError(yaml.Unmarshal(apiexportendpointsliceCorePlatformMeshOrgYAML, &platformMeshBinding))
	for i := range platformMeshBinding.Spec.PermissionClaims {
		pc := &platformMeshBinding.Spec.PermissionClaims[i]
		if pc.Group == "tenancy.kcp.io" {
			pc.IdentityHash = aeTenancy.Status.IdentityHash
		}
	}
	err = platformMeshSystemClient.Create(s.ctx, &platformMeshBinding)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		s.Require().NoError(err)
	}
	s.Assert().Eventually(func() bool {
		var binding kcpapisv1alpha2.APIBinding
		if err := platformMeshSystemClient.Get(s.ctx, ctrlruntimeclient.ObjectKey{Name: platformMeshBinding.Name}, &binding); err != nil {
			return false
		}
		return binding.Status.Phase == kcpapisv1alpha2.APIBindingPhaseBound
	}, 10*time.Second, 200*time.Millisecond, "APIBinding core.platform-mesh.io should be bound")

	// Create WorkspaceTypes in root workspace. These reference the
	// core.platform-mesh.io APIExport in their defaultAPIBindings, so the
	// APIExport must exist before this point.
	for _, workspaceTypeYAML := range [][]byte{
		workspaceTypeOrgYAML,
		workspaceTypeOrgsYAML,
		workspaceTypeAccountYAML,
	} {
		var workspaceType kcptenancyv1alpha1.WorkspaceType
		s.Require().NoError(yaml.Unmarshal(workspaceTypeYAML, &workspaceType))
		err = rootClient.Create(s.ctx, &workspaceType)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			s.Require().NoError(err)
		}
		s.logger.Info().Msgf("Created WorkspaceType '%s' in root workspace", workspaceType.Name)
	}

	// create :root:orgs
	orgsWs, orgsClusterPath := envtest.NewWorkspaceFixture(s.T(), s.kcpClient, core.RootCluster.Path(), envtest.WithName("orgs"), envtest.WithType(core.RootCluster.Path(), "orgs"))
	s.logger.Info().Msgf("orgs workspace path (%s), cluster id (%s)", orgsClusterPath, orgsWs.Spec.Cluster)
	s.orgsClusterPath = orgsClusterPath
	s.rootOrgsClient = s.kcpClient.Cluster(orgsClusterPath)

	var endpointSlice kcpapisv1alpha1.APIExportEndpointSlice
	s.Assert().Eventually(func() bool {
		err := platformMeshSystemClient.Get(s.ctx, ctrlruntimeclient.ObjectKey{Name: "core.platform-mesh.io"}, &endpointSlice)
		if err != nil {
			return false
		}
		return len(endpointSlice.Status.APIExportEndpoints) > 0 && endpointSlice.Status.APIExportEndpoints[0].URL != ""
	}, 10*time.Second, 200*time.Millisecond, "kcp should automatically create APIExportEndpointSlice with populated endpoints")

	s.Require().NotEmpty(endpointSlice.Status.APIExportEndpoints, "APIExportEndpointSlice should have at least one endpoint")
	s.Require().NotEqual("", endpointSlice.Status.APIExportEndpoints[0].URL, "APIExportEndpointSlice endpoint URL should not be empty")
}

// setupManager configures but does not start the manager
func (s *AccountTestSuite) setupManager() {
	// Setup root workspace
	providerConfig := rest.CopyConfig(s.kcpConfig)
	providerConfig.Host += fmt.Sprintf("/clusters/%s", s.platformMeshSysPath)

	axplogr := s.logger.ComponentLogger("apiexport_provider").Logr()
	provider, err := pathaware.New(providerConfig, "core.platform-mesh.io", apiexport.Options{
		Scheme: s.scheme,
		Log:    &axplogr,
	})
	s.Require().NoError(err)

	mcOpts := mcmanager.Options{
		Scheme: s.scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		BaseContext: func() context.Context { return s.ctx },
	}

	s.mgr, err = mcmanager.New(providerConfig, provider, mcOpts)
	s.Require().NoError(err)
}

// startManager starts the manager configured by setupManager
func (s *AccountTestSuite) startManager() {
	go func() {
		if err := s.mgr.Start(s.ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Error().Msgf("Manager exited with error: %v", err)
		}
	}()
}

// setupDefaultOrg creates the "default" Account with type org and waits for its
// Workspace to be ready
func (s *AccountTestSuite) setupDefaultOrg() {
	// Setup orgs workspace with test "default" organisation
	var acc pmcorev1alpha1.Account
	err := yaml.Unmarshal(accountRootOrgYAML, &acc)
	s.Require().NoError(err, "Unmarshalling embedded data")
	err = s.rootOrgsClient.Create(s.ctx, &acc)
	s.Require().NoError(err, "Creating unmarshalled object")
	// Wait for the default workspace to be created by the account controller
	var defaultWs kcptenancyv1alpha1.Workspace
	s.Require().Eventually(func() bool {
		err := s.rootOrgsClient.Get(s.ctx, types.NamespacedName{Name: defaultWorkspace}, &defaultWs)
		if err != nil {
			return false
		}
		return defaultWs.Status.Phase == "Ready"
	}, 15*time.Second, 500*time.Millisecond, "default workspace should be ready")
	s.rootOrgsDefaultClient = s.kcpClient.Cluster(s.orgsClusterPath.Join(defaultWorkspace))
}
