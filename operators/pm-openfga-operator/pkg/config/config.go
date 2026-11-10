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

package config

import (
	"time"

	"github.com/spf13/pflag"

	"go.platform-mesh.io/pm-openfga-operator/internal/features"

	"k8s.io/component-base/featuregate"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

const (
	CoreProviderName        = "core"
	SystemProviderName      = "system"
	ProvidersProviderName   = "providers"
	FgaProviderName         = "fga"
	ProviderFgaProviderName = "provider-fga"
	providerSeparator       = "#"
	OrgsClusterPath         = "root:orgs"
)

type InitializerConfig struct {
	WorkspaceInitializerEnabled bool
}

type FGAConfig struct {
	Target          string
	ObjectType      string
	ParentRelation  string
	CreatorRelation string
	StoreIDCacheTTL time.Duration
}

type KCPConfig struct {
	Kubeconfig string
}

type APIExportEndpointSlices struct {
	CorePlatformMeshIO        string
	SystemPlatformMeshIO      string
	ProvidersPlatformMeshIO   string
	FgaPlatformMeshIO         string
	ProviderFgaPlatformMeshIO string
}

// Config struct to hold the app config
type Config struct {
	FeatureGates               featuregate.MutableFeatureGate
	FGA                        FGAConfig
	KCP                        KCPConfig
	APIExportEndpointSlices    APIExportEndpointSlices
	CoreModulePath             string
	WorkspacePath              string
	WorkspaceTypeName          string
	MigrateAuthorizationModels bool
	AllowMemberTuplesEnabled   bool
	RekeyOrphanedTuplesEnabled bool
	Initializer                InitializerConfig
}

func NewConfig() Config {
	return Config{
		FeatureGates: features.New(),
		FGA: FGAConfig{
			ObjectType:      "core_platform-mesh_io_account",
			ParentRelation:  "parent",
			CreatorRelation: "owner",
			StoreIDCacheTTL: 24 * time.Hour,
		},
		KCP: KCPConfig{
			Kubeconfig: "/api-kubeconfig/kubeconfig",
		},
		APIExportEndpointSlices: APIExportEndpointSlices{
			CorePlatformMeshIO:        "core.platform-mesh.io",
			SystemPlatformMeshIO:      "system.platform-mesh.io",
			ProvidersPlatformMeshIO:   "providers.platform-mesh.io",
			FgaPlatformMeshIO:         "fga.platform-mesh.io",
			ProviderFgaPlatformMeshIO: "provider-fga.platform-mesh.io",
		},
		WorkspacePath:     "root",
		WorkspaceTypeName: "security",
		Initializer: InitializerConfig{
			WorkspaceInitializerEnabled: true,
		},
	}
}

func (c *Config) AddFlags(fs *pflag.FlagSet) {
	c.FeatureGates.AddFlag(fs)
	fs.StringVar(&c.FGA.Target, "fga-target", c.FGA.Target, "Set the OpenFGA API target")
	fs.DurationVar(&c.FGA.StoreIDCacheTTL, "fga-store-id-cache-ttl", c.FGA.StoreIDCacheTTL, "TTL for the OpenFGA store ID cache (e.g. 5m, 1h)")
	fs.StringVar(&c.FGA.ObjectType, "fga-object-type", c.FGA.ObjectType, "Set the OpenFGA object type for account tuples")
	fs.StringVar(&c.FGA.ParentRelation, "fga-parent-relation", c.FGA.ParentRelation, "Set the OpenFGA parent relation name")
	fs.StringVar(&c.FGA.CreatorRelation, "fga-creator-relation", c.FGA.CreatorRelation, "Set the OpenFGA creator relation name")
	fs.StringVar(&c.KCP.Kubeconfig, "kcp-kubeconfig", c.KCP.Kubeconfig, "Set the kcp kubeconfig path")
	fs.StringVar(&c.APIExportEndpointSlices.CorePlatformMeshIO, "api-export-endpoint-slice-name", c.APIExportEndpointSlices.CorePlatformMeshIO, "Set the core.platform-mesh.io APIExportEndpointSlice name")
	fs.StringVar(&c.APIExportEndpointSlices.SystemPlatformMeshIO, "system-api-export-endpoint-slice-name", c.APIExportEndpointSlices.SystemPlatformMeshIO, "Set the system.platform-mesh.io APIExportEndpointSlice name")
	fs.StringVar(&c.APIExportEndpointSlices.ProvidersPlatformMeshIO, "providers-api-export-endpoint-slice-name", c.APIExportEndpointSlices.ProvidersPlatformMeshIO, "Set the providers.platform-mesh.io APIExportEndpointSlice name")
	fs.StringVar(&c.APIExportEndpointSlices.FgaPlatformMeshIO, "fga-api-export-endpoint-slice-name", c.APIExportEndpointSlices.FgaPlatformMeshIO, "Set the fga.platform-mesh.io APIExportEndpointSlice name")
	fs.StringVar(&c.APIExportEndpointSlices.ProviderFgaPlatformMeshIO, "provider-fga-api-export-endpoint-slice-name", c.APIExportEndpointSlices.ProviderFgaPlatformMeshIO, "Set the provider-fga.platform-mesh.io APIExportEndpointSlice name")
	fs.StringVar(&c.CoreModulePath, "core-module-path", c.CoreModulePath, "Set the path to the core module FGA model file")
	fs.StringVar(&c.WorkspacePath, "workspace-path", c.WorkspacePath, "Set the parent workspace path for created workspaces")
	fs.StringVar(&c.WorkspaceTypeName, "workspace-type-name", c.WorkspaceTypeName, "Set the workspace type name")
	fs.BoolVar(&c.MigrateAuthorizationModels, "migrate-authorization-models", c.MigrateAuthorizationModels, "Enable one-time authorization model migration")
	fs.BoolVar(&c.AllowMemberTuplesEnabled, "allow-member-tuples-enabled", c.AllowMemberTuplesEnabled, "Enable allow-member tuples management")
	fs.BoolVar(&c.RekeyOrphanedTuplesEnabled, "rekey-orphaned-tuples-enabled", c.RekeyOrphanedTuplesEnabled, "Enable re-keying of org tuples orphaned by a workspace re-creation (cluster-id change)")
	fs.BoolVar(&c.Initializer.WorkspaceInitializerEnabled, "initializer-workspace-enabled", c.Initializer.WorkspaceInitializerEnabled, "Enable workspace initialization")
}

func (c Config) InitializerName() string {
	return c.WorkspacePath + ":" + c.WorkspaceTypeName
}

// FeatureEnabled reports whether a feature gate is on.
// A Config built without NewConfig has every gate at its default.
func (c Config) FeatureEnabled(feature featuregate.Feature) bool {
	if c.FeatureGates == nil {
		return features.New().Enabled(feature)
	}
	return c.FeatureGates.Enabled(feature)
}

func (c Config) TerminatorName() string {
	return c.WorkspacePath + ":" + c.WorkspaceTypeName
}

// MultiProviderName returns a cluster name with provider prefix and separator for multi provider.
// The multi.Provider prefixes cluster names as "providerName#clusterName"
func MultiProviderName(providerName, clusterName string) multicluster.ClusterName {
	return multicluster.ClusterName(providerName + providerSeparator + clusterName)
}

// StoreClusterName returns the cluster name of a Store's logical cluster, as seen through the fga provider.
func StoreClusterName(logicalCluster string) multicluster.ClusterName {
	return MultiProviderName(FgaProviderName, logicalCluster)
}
