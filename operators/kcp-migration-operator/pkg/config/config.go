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

import "github.com/spf13/pflag"

type ValidateSpecSubroutineConfig struct {
	Enabled bool
}

type CreateConfigMapSubroutineConfig struct {
	Enabled bool
}

type CreateChildOperatorSubroutineConfig struct {
	Enabled bool
}

type UpdateStatusSubroutineConfig struct {
	Enabled bool
}

type SubroutinesConfig struct {
	ValidateSpec        ValidateSpecSubroutineConfig
	CreateConfigMap     CreateConfigMapSubroutineConfig
	CreateChildOperator CreateChildOperatorSubroutineConfig
	UpdateStatus        UpdateStatusSubroutineConfig
}

type ChildOperatorResourcesConfig struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
}

type ChildOperatorConfig struct {
	Image     string
	Resources ChildOperatorResourcesConfig
}

type SecretsConfig struct {
	KCPKubeconfig    string
	SourceKubeconfig string
}

// OperatorConfig holds the configuration for the kcp Migration Operator (main mode)
type OperatorConfig struct {
	Subroutines   SubroutinesConfig
	ChildOperator ChildOperatorConfig
	Secrets       SecretsConfig
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		Subroutines: SubroutinesConfig{
			ValidateSpec: ValidateSpecSubroutineConfig{
				Enabled: true,
			},
			CreateConfigMap: CreateConfigMapSubroutineConfig{
				Enabled: true,
			},
			CreateChildOperator: CreateChildOperatorSubroutineConfig{
				Enabled: true,
			},
			UpdateStatus: UpdateStatusSubroutineConfig{
				Enabled: true,
			},
		},
		ChildOperator: ChildOperatorConfig{
			Image: "ghcr.io/platform-mesh/kcp-migration-operator:latest",
			Resources: ChildOperatorResourcesConfig{
				CPURequest:    "100m",
				CPULimit:      "500m",
				MemoryRequest: "128Mi",
				MemoryLimit:   "256Mi",
			},
		},
		Secrets: SecretsConfig{
			KCPKubeconfig:    "kcp-kubeconfig",
			SourceKubeconfig: "source-kubeconfig",
		},
	}
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&c.Subroutines.ValidateSpec.Enabled, "subroutines-validate-spec-enabled", c.Subroutines.ValidateSpec.Enabled, "Enable spec validation subroutine")
	fs.BoolVar(&c.Subroutines.CreateConfigMap.Enabled, "subroutines-create-configmap-enabled", c.Subroutines.CreateConfigMap.Enabled, "Enable configmap creation subroutine")
	fs.BoolVar(&c.Subroutines.CreateChildOperator.Enabled, "subroutines-create-child-operator-enabled", c.Subroutines.CreateChildOperator.Enabled, "Enable child operator creation subroutine")
	fs.BoolVar(&c.Subroutines.UpdateStatus.Enabled, "subroutines-update-status-enabled", c.Subroutines.UpdateStatus.Enabled, "Enable status update subroutine")

	fs.StringVar(&c.ChildOperator.Image, "child-operator-image", c.ChildOperator.Image, "Set child operator image")
	fs.StringVar(&c.ChildOperator.Resources.CPURequest, "child-operator-cpu-request", c.ChildOperator.Resources.CPURequest, "Set child operator CPU request")
	fs.StringVar(&c.ChildOperator.Resources.CPULimit, "child-operator-cpu-limit", c.ChildOperator.Resources.CPULimit, "Set child operator CPU limit")
	fs.StringVar(&c.ChildOperator.Resources.MemoryRequest, "child-operator-memory-request", c.ChildOperator.Resources.MemoryRequest, "Set child operator memory request")
	fs.StringVar(&c.ChildOperator.Resources.MemoryLimit, "child-operator-memory-limit", c.ChildOperator.Resources.MemoryLimit, "Set child operator memory limit")

	fs.StringVar(&c.Secrets.KCPKubeconfig, "secrets-kcp-kubeconfig", c.Secrets.KCPKubeconfig, "Set secret name containing kcp kubeconfig")
	fs.StringVar(&c.Secrets.SourceKubeconfig, "secrets-source-kubeconfig", c.Secrets.SourceKubeconfig, "Set secret name containing source kubeconfig")
}

// SourceConfig defines the source resource to watch
type SourceConfig struct {
	// APIVersion of the source resource (e.g., "fabric.foundation.sap.com/v1alpha1")
	APIVersion string `yaml:"apiVersion"`
	// Kind of the source resource (e.g., "Account")
	Kind string `yaml:"kind"`
	// Namespace to filter source resources (optional, empty = all namespaces)
	Namespace string `yaml:"namespace,omitempty"`
	// Resources must match ALL selectors (AND logic)
	LabelSelectors []string `yaml:"labelSelectors,omitempty"`
}

// TargetConfig defines the target workspace and namespace
type TargetConfig struct {
	// WorkspaceExpression is a Go template to derive the target workspace
	WorkspaceExpression string `yaml:"workspaceExpression"`
	// Namespace in the target workspace (optional)
	Namespace string `yaml:"namespace,omitempty"`
}

// TransformConfig defines the transformation template
type TransformConfig struct {
	// TemplatePath is the path to a template file (relative to templates directory)
	TemplatePath string `yaml:"templatePath,omitempty"`
	// Template is an inline template (used if templatePath is not set)
	Template string `yaml:"template,omitempty"`
	// ConfigMapName is the optional ConfigMap name containing the template
	ConfigMapName string `yaml:"configMapName,omitempty"`
	// ConfigMapKey is the key in the ConfigMap containing the template
	ConfigMapKey string `yaml:"configMapKey,omitempty"`
}

// PerformanceConfig defines performance tuning options
type PerformanceConfig struct {
	// MaxWorkers is the number of concurrent reconciliation workers
	MaxWorkers int `yaml:"maxWorkers,omitempty"`
	// RateLimitResourcesPerSecond limits sync operations per second
	RateLimitResourcesPerSecond int `yaml:"rateLimitResourcesPerSecond,omitempty"`
	// RateLimitBurst is the burst size for rate limiting
	RateLimitBurst int `yaml:"rateLimitBurst,omitempty"`
}

// SyncConfig holds the configuration for the sync mode (child operator)
type SyncConfig struct {
	// MigrationName is the name of the KCPMigration resource this sync is for
	MigrationName string

	// MigrationNamespace is the namespace of the KCPMigration resource
	MigrationNamespace string

	// Source configuration (embedded struct)
	Source SourceConfig `yaml:"source"`
	// Target configuration (embedded struct)
	Target TargetConfig `yaml:"target"`
	// Transform configuration (embedded struct)
	Transform TransformConfig `yaml:"transform"`
	// Performance configuration (embedded struct)
	Performance PerformanceConfig `yaml:"performance"`
	// KCPKubeconfigPath is the path to the kcp kubeconfig file
	KCPKubeconfigPath string

	// SourceKubeconfigPath is the path to the source cluster kubeconfig file
	SourceKubeconfigPath string
}

func NewSyncConfig() SyncConfig {
	return SyncConfig{
		Transform: TransformConfig{
			ConfigMapKey: "template.yaml",
		},
		Performance: PerformanceConfig{
			MaxWorkers:                  1,
			RateLimitResourcesPerSecond: 50,
			RateLimitBurst:              100,
		},
		KCPKubeconfigPath: "/etc/kcp/kubeconfig",
	}
}

func (c *SyncConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.MigrationName, "migration-name", c.MigrationName, "Name of the KCPMigration resource")
	fs.StringVar(&c.MigrationNamespace, "migration-namespace", c.MigrationNamespace, "Namespace of the KCPMigration resource")
	fs.StringVar(&c.Source.APIVersion, "source-api-version", c.Source.APIVersion, "API version of source resources to watch")
	fs.StringVar(&c.Source.Kind, "source-kind", c.Source.Kind, "Kind of source resources to watch")
	fs.StringVar(&c.Source.Namespace, "source-namespace", c.Source.Namespace, "Namespace to watch for source resources (empty = all namespaces)")
	fs.StringSliceVar(&c.Source.LabelSelectors, "source-label-selectors", c.Source.LabelSelectors, "Label selectors to filter source resources (e.g., 'app=myapp,env=prod')")
	fs.StringVar(&c.Target.WorkspaceExpression, "target-workspace-expression", c.Target.WorkspaceExpression, "Go template for target workspace")
	fs.StringVar(&c.Target.Namespace, "target-namespace", c.Target.Namespace, "Target namespace in kcp workspace")
	fs.StringVar(&c.Transform.Template, "template", c.Transform.Template, "Inline transformation template")
	fs.StringVar(&c.Transform.TemplatePath, "template-path", c.Transform.TemplatePath, "Path to template file (useful for local development)")
	fs.StringVar(&c.Transform.ConfigMapName, "template-configmap-name", c.Transform.ConfigMapName, "ConfigMap containing template")
	fs.StringVar(&c.Transform.ConfigMapKey, "template-configmap-key", c.Transform.ConfigMapKey, "Key in ConfigMap for template")
	fs.StringVar(&c.KCPKubeconfigPath, "kcp-kubeconfig-path", c.KCPKubeconfigPath, "Path to kcp kubeconfig")
	fs.StringVar(&c.SourceKubeconfigPath, "source-kubeconfig-path", c.SourceKubeconfigPath, "Path to source cluster kubeconfig")
	fs.IntVar(&c.Performance.RateLimitResourcesPerSecond, "rate-limit-resources-per-second", c.Performance.RateLimitResourcesPerSecond, "Max resources to sync per second")
	fs.IntVar(&c.Performance.RateLimitBurst, "rate-limit-burst", c.Performance.RateLimitBurst, "Rate limiter burst size")
	fs.IntVar(&c.Performance.MaxWorkers, "max-workers", c.Performance.MaxWorkers, "Max concurrent workers for reconciliation queue")
}

// ResourceSyncConfig defines a single resource type to sync
// This is used in the multi-resource sync file format
type ResourceSyncConfig struct {
	// Name is a unique identifier for this sync configuration
	Name string `yaml:"name"`
	// Source configuration (reuses SourceConfig struct)
	Source SourceConfig `yaml:"source"`
	// Target configuration (reuses TargetConfig struct)
	Target TargetConfig `yaml:"target"`
	// Transform configuration (reuses TransformConfig struct)
	Transform TransformConfig `yaml:"transform,omitempty"`
	// Performance configuration (reuses PerformanceConfig struct)
	Performance PerformanceConfig `yaml:"performance,omitempty"`
}

// MultiSyncConfig holds configuration for multi-resource sync mode
// This is loaded from a YAML configuration file
type MultiSyncConfig struct {
	// KCPKubeconfigPath is the path to the kcp kubeconfig file
	KCPKubeconfigPath string `yaml:"kcpKubeconfigPath"`
	// SourceKubeconfigPath is the path to the source cluster kubeconfig file
	SourceKubeconfigPath string `yaml:"sourceKubeconfigPath,omitempty"`
	// TemplatesDir is the directory containing template files
	// Template paths in resource configs are relative to this directory
	TemplatesDir string `yaml:"templatesDir,omitempty"`
	// Resources is the list of resource types to sync
	Resources []ResourceSyncConfig `yaml:"resources"`
}
